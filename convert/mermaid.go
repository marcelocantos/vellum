// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Mermaid image output formats for mmdc.
//
// HTML/view paths use SVG (vector, legible labels in browsers).
// PDF paths keep PNG: Mermaid SVGs rely on foreignObject for node labels,
// which Prince (and some print engines) do not paint — so PDF stays
// raster PNG at 2× scale for label fidelity (🎯T17 dual path).
const (
	MermaidSVG = "svg"
	MermaidPNG = "png"
)

var mermaidBlockRe = regexp.MustCompile("(?m)(?:^<!--\\s*vellum:scale\\s+([0-9.]+)\\s*-->\\s*\n)?^```mermaid\\s*\n([\\s\\S]+?)^```\\s*$")

type mermaidDiagram struct {
	source string
	scale  float64 // CSS scale factor (1.0 = default)
}

// mermaidPreprocessor extracts ```mermaid code blocks from markdown source,
// renders each via mmdc, and replaces them with HTML placeholders.
type mermaidPreprocessor struct {
	diagrams     []mermaidDiagram
	placeholders []string
	format       string // MermaidSVG or MermaidPNG
}

func newMermaidPreprocessor(format string) *mermaidPreprocessor {
	return &mermaidPreprocessor{format: resolveMermaidFormat(format)}
}

func resolveMermaidFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case MermaidPNG:
		return MermaidPNG
	default:
		// Default SVG: HTML, view, clipboard, and public Render.
		return MermaidSVG
	}
}

// Extract finds all mermaid code blocks (with optional <!-- vellum:scale N -->
// hints) and replaces them with placeholders.
func (m *mermaidPreprocessor) Extract(src string) string {
	return mermaidBlockRe.ReplaceAllStringFunc(src, func(match string) string {
		inner := mermaidBlockRe.FindStringSubmatch(match)
		if len(inner) < 3 {
			return match
		}
		scale := 1.0
		if inner[1] != "" {
			if v, err := strconv.ParseFloat(inner[1], 64); err == nil && v > 0 {
				scale = v
			}
		}
		idx := len(m.diagrams)
		m.diagrams = append(m.diagrams, mermaidDiagram{
			source: strings.TrimSpace(inner[2]),
			scale:  scale,
		})
		p := fmt.Sprintf("<!--MERMAID:%d-->", idx)
		m.placeholders = append(m.placeholders, p)
		return p
	})
}

// ReplaceAll renders all collected mermaid diagrams and replaces
// placeholders in the rendered HTML.
//
// When mmdc fails for a diagram, the source is kept as a
// <pre class="mermaid-error"> fallback so the document still renders,
// the failure is logged to stderr, and a diagnostic string is appended
// to the returned soft-error list (1-based diagram index). Callers must
// surface those soft errors (MCP errors array / CLI non-zero exit).
func (m *mermaidPreprocessor) ReplaceAll(ctx context.Context, html string) (string, []string) {
	if len(m.diagrams) == 0 {
		return html, nil
	}

	// Each diagram costs a full mmdc process, and mmdc drives Puppeteer,
	// which launches a headless Chromium — so the cost is browser
	// startup per diagram, not markup generation. Rendering them
	// serially made document time linear in diagram count (measured
	// 2026-08-07: 8 diagrams took 3536ms serially against 854ms
	// concurrently, ~440ms apiece). Render concurrently, bounded by CPU
	// count so a diagram-heavy document cannot spawn an unbounded pile
	// of browsers.
	type result struct {
		img string
		err error
	}
	results := make([]result, len(m.diagrams))

	limit := runtime.NumCPU()
	if limit > len(m.diagrams) {
		limit = len(m.diagrams)
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, d := range m.diagrams {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			img, err := renderMermaidFn(ctx, d.source, m.format)
			results[i] = result{img: img, err: err}
		}()
	}
	wg.Wait()

	// Substitution and diagnostics stay serial and in document order, so
	// placeholder replacement and the 1-based indices in soft-error
	// messages are deterministic regardless of completion order.
	var soft []string
	for i, d := range m.diagrams {
		img := results[i].img
		if err := results[i].err; err != nil {
			// 1-based index for human-facing messages.
			msg := fmt.Sprintf("mermaid diagram %d: %v", i+1, err)
			fmt.Fprintln(os.Stderr, "Error:", msg)
			soft = append(soft, msg)
			img = `<pre class="mermaid-error">` + htmlEscapeText(d.source) + `</pre>`
		}
		// Apply scale via CSS width percentage if not default.
		style := ""
		if d.scale != 1.0 {
			style = fmt.Sprintf(` style="max-width: %.0f%%"`, d.scale*100)
		}
		wrapped := fmt.Sprintf(`<div class="mermaid-svg"%s>%s</div>`, style, img)
		html = strings.Replace(html, m.placeholders[i], wrapped, 1)
	}

	return html, soft
}

// renderMermaidFn is the diagram renderer. Tests override it to inject
// failures or assert format without shelling out to mmdc.
var renderMermaidFn = renderMermaid

func htmlEscapeText(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	return replacer.Replace(s)
}

func renderMermaid(ctx context.Context, src string, format string) (string, error) {
	format = resolveMermaidFormat(format)

	// Write mermaid source to a temp file.
	inFile, err := os.CreateTemp("", "vellum-mmd-*.mmd")
	if err != nil {
		return "", err
	}
	defer os.Remove(inFile.Name())

	if _, err := inFile.WriteString(src); err != nil {
		inFile.Close()
		return "", err
	}
	inFile.Close()

	ext := format
	outFile, err := os.CreateTemp("", "vellum-mmd-*."+ext)
	if err != nil {
		return "", err
	}
	outFile.Close()
	defer os.Remove(outFile.Name())

	args := []string{
		"-i", inFile.Name(),
		"-o", outFile.Name(),
		"-e", format,
		"--quiet",
	}
	// PNG at 2× for print sharpness; SVG is vector (no raster scale).
	if format == MermaidPNG {
		args = append(args, "-s", "2")
	}

	cmd := exec.CommandContext(ctx, "mmdc", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mmdc: %w: %s", err, string(out))
	}

	data, err := os.ReadFile(outFile.Name())
	if err != nil {
		return "", fmt.Errorf("reading mmdc output: %w", err)
	}

	if format == MermaidSVG {
		return embedMermaidSVG(data), nil
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf(`<img src="data:image/png;base64,%s" alt="Mermaid diagram">`, b64), nil
}

// embedMermaidSVG returns inline SVG markup suitable for HTML/view paths.
func embedMermaidSVG(data []byte) string {
	s := string(data)
	// Drop XML prolog / doctype so the fragment embeds cleanly in HTML.
	if i := strings.Index(s, "<svg"); i >= 0 {
		s = s[i:]
	}
	return strings.TrimSpace(s)
}
