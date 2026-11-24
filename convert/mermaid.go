// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/marcelocantos/vellum/internal/shim"
)

//go:embed mmdchook.mjs
var mermaidConfineHook string

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
		// As long as the block it replaces, so later source lines hold.
		p := fmt.Sprintf("<!--MERMAID:%d%s-->", idx, strings.Repeat("\n", lineCount(match)))
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
	// Mermaid never hard-fails: RenderFragments reports mmdc errors per
	// fragment, and the only hard error is the KaTeX batch, absent here.
	results, _ := RenderFragments(ctx, m.fragments())
	return ResolveFragments(m.deferAll(html), results)
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
	} else {
		args = append(args, "--svgId", mermaidSVGID(src))
	}

	cmd, err := mmdcCommand(ctx, args)
	if err != nil {
		return "", err
	}
	// The confine hook writes this token to stderr at exit only when it
	// rewrote mermaid-cli and launched the browser confined. Without it the
	// render ran unconfined, whatever mmdc's exit status says.
	var tokenBytes [16]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return "", fmt.Errorf("mmdc: confine token: %w", err)
	}
	token := "vellum-mmdc-confined-" + hex.EncodeToString(tokenBytes[:])
	cmd.Env = append(os.Environ(), "VELLUM_MMDC_CONFINED_TOKEN="+token)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mmdc: %w: %s", err, string(out))
	}
	if !strings.Contains(string(out), token) {
		return "", fmt.Errorf("mmdc: renderer not confined (the confine hook did not recognise this mermaid-cli); refusing its output: %s", string(out))
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

// mermaidSVGID is the id of one diagram's SVG element. Mermaid scopes a
// diagram's stylesheet and its marker definitions to that id, and inline
// SVG shares the page's id space: with mmdc's default id on every diagram,
// each one's styles applied to all the others (a later diagram's font
// size clipped an earlier one's labels). The id follows from the source,
// so a diagram repeated on a page shares it, with identical styles.
func mermaidSVGID(src string) string {
	// The id is repeated in every rule of the diagram's stylesheet (a few
	// hundred times), so it is kept short: 48 bits tell apart the diagrams
	// of one page.
	sum := sha256.Sum256([]byte(src))
	return "mmd-" + hex.EncodeToString(sum[:6])
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

// mmdcCommand runs mmdc under Node with the confine hook on its import
// path. mmdc's Chromium renders the diagram on a file: page and, unless
// the hook rewrites it, will load any image that page names. A launch
// that is not a Node script is refused: running it would be unconfined.
func mmdcCommand(ctx context.Context, args []string) (*exec.Cmd, error) {
	mmdc, err := exec.LookPath("mmdc")
	if err != nil {
		return nil, fmt.Errorf("mmdc: %w", err)
	}
	node, script, err := mmdcNodeAndScript(mmdc)
	if err != nil {
		return nil, err
	}
	hook, err := mermaidConfineHookPath()
	if err != nil {
		return nil, fmt.Errorf("mmdc: confine hook: %w", err)
	}
	argv := make([]string, 0, 3+len(args))
	argv = append(argv, "--import", hook, script)
	argv = append(argv, args...)
	return exec.CommandContext(ctx, node, argv...), nil
}

// mmdcNodeAndScript returns the node binary and the script it should run.
// On Windows, npm's mmdc.cmd is a batch shim; elsewhere mmdc is a script
// whose shebang names node. Either way the script path is what
// `node --import <hook>` executes, so the hook loads before mermaid-cli.
func mmdcNodeAndScript(mmdcPath string) (node, script string, err error) {
	if runtime.GOOS == "windows" {
		script, ok := shim.NpmShimScript(mmdcPath)
		if !ok {
			return "", "", fmt.Errorf("mmdc: refusing to run %q unconfined: not an npm node shim", mmdcPath)
		}
		node = "node"
		if local := filepath.Join(filepath.Dir(mmdcPath), "node.exe"); regularFile(local) {
			node = local
		}
		return node, script, nil
	}
	f, err := os.Open(mmdcPath)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	buf := make([]byte, 256)
	n, readErr := f.Read(buf)
	if n == 0 {
		if readErr != nil {
			return "", "", readErr
		}
		return "", "", fmt.Errorf("mmdc: refusing to run %q unconfined: not a node script", mmdcPath)
	}
	line := string(buf[:n])
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimRight(line, "\r")
	node, ok := nodeFromShebang(line)
	if !ok {
		return "", "", fmt.Errorf("mmdc: refusing to run %q unconfined: not a node script", mmdcPath)
	}
	return node, mmdcPath, nil
}

// nodeFromShebang accepts `#!/path/to/node` and `#!/usr/bin/env node`
// (with flags between env and node). Anything else is not a Node script.
func nodeFromShebang(line string) (string, bool) {
	if !strings.HasPrefix(line, "#!") {
		return "", false
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(fields) == 0 {
		return "", false
	}
	if filepath.Base(fields[0]) == "env" {
		for _, f := range fields[1:] {
			if f == "node" || filepath.Base(f) == "node" || filepath.Base(f) == "node.exe" {
				return f, true
			}
		}
		return "", false
	}
	base := filepath.Base(fields[0])
	if base == "node" || base == "node.exe" {
		return fields[0], true
	}
	return "", false
}

// mermaidConfineHookPath writes the embedded hook once per content hash.
// The name is the hash, so a changed hook is a different file and a
// concurrent writer that loses the rename still finds the same bytes.
func mermaidConfineHookPath() (string, error) {
	sum := sha256.Sum256([]byte(mermaidConfineHook))
	dst := filepath.Join(os.TempDir(), "vellum-mmdc-confine-"+hex.EncodeToString(sum[:])+".mjs")
	// A predictable name in the temp directory can be planted. Use the
	// file only when its bytes are the hook we embed.
	if b, err := os.ReadFile(dst); err == nil && string(b) == mermaidConfineHook {
		return dst, nil
	}
	_ = os.Remove(dst)
	tmp, err := os.CreateTemp(os.TempDir(), "vellum-mmdc-confine-*.mjs")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(mermaidConfineHook); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		if b, readErr := os.ReadFile(dst); readErr == nil && string(b) == mermaidConfineHook {
			return dst, nil
		}
		return "", fmt.Errorf("installing confine hook: %w", err)
	}
	return dst, nil
}

func regularFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}
