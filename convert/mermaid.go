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
	"strconv"
	"strings"
)

var mermaidBlockRe = regexp.MustCompile("(?m)(?:^<!--\\s*vellum:scale\\s+([0-9.]+)\\s*-->\\s*\n)?^```mermaid\\s*\n([\\s\\S]+?)^```\\s*$")

type mermaidDiagram struct {
	source string
	scale  float64 // CSS scale factor (1.0 = default)
}

// mermaidPreprocessor extracts ```mermaid code blocks from markdown source,
// renders each to PNG via mmdc, and replaces them with HTML placeholders.
type mermaidPreprocessor struct {
	diagrams     []mermaidDiagram
	placeholders []string
}

func newMermaidPreprocessor() *mermaidPreprocessor {
	return &mermaidPreprocessor{}
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

// ReplaceAll renders all collected mermaid diagrams to SVG and replaces
// placeholders in the rendered HTML.
func (m *mermaidPreprocessor) ReplaceAll(ctx context.Context, html string) (string, error) {
	if len(m.diagrams) == 0 {
		return html, nil
	}

	for i, d := range m.diagrams {
		img, err := renderMermaid(ctx, d.source)
		if err != nil {
			img = `<pre class="mermaid-error">` + d.source + `</pre>`
		}
		// Apply scale via CSS width percentage if not default.
		style := ""
		if d.scale != 1.0 {
			style = fmt.Sprintf(` style="max-width: %.0f%%"`, d.scale*100)
		}
		wrapped := fmt.Sprintf(`<div class="mermaid-svg"%s>%s</div>`, style, img)
		html = strings.Replace(html, m.placeholders[i], wrapped, 1)
	}

	return html, nil
}

func renderMermaid(ctx context.Context, src string) (string, error) {
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

	// Render as PNG at 2x scale — SVG foreignObject labels don't
	// render in Prince, so PNG is more reliable.
	outFile, err := os.CreateTemp("", "vellum-mmd-*.png")
	if err != nil {
		return "", err
	}
	outFile.Close()
	defer os.Remove(outFile.Name())

	cmd := exec.CommandContext(ctx, "mmdc",
		"-i", inFile.Name(),
		"-o", outFile.Name(),
		"-e", "png",
		"-s", "2",
		"--quiet",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mmdc: %w: %s", err, string(out))
	}

	pngData, err := os.ReadFile(outFile.Name())
	if err != nil {
		return "", fmt.Errorf("reading mmdc output: %w", err)
	}

	// Embed as base64 data URI so the HTML is self-contained.
	b64 := base64.StdEncoding.EncodeToString(pngData)
	return fmt.Sprintf(`<img src="data:image/png;base64,%s" alt="Mermaid diagram">`, b64), nil
}
