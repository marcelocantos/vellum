// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

var mermaidPlaceholderRe = regexp.MustCompile(`<!--MERMAID:(\d+)-->`)

func TestMermaidPreprocessor_SimpleBlock(t *testing.T) {
	p := newMermaidPreprocessor(MermaidSVG)
	src := "Intro.\n\n```mermaid\ngraph TD\n  A --> B\n```\n\nOutro."
	out := p.Extract(src)

	if len(p.diagrams) != 1 {
		t.Fatalf("expected 1 diagram, got %d: %#v", len(p.diagrams), p.diagrams)
	}
	if want := "graph TD\n  A --> B"; p.diagrams[0].source != want {
		t.Errorf("source = %q, want %q", p.diagrams[0].source, want)
	}
	if p.diagrams[0].scale != 1.0 {
		t.Errorf("scale = %v, want 1.0 (default)", p.diagrams[0].scale)
	}
	if !strings.Contains(out, "<!--MERMAID:0-->") {
		t.Errorf("output missing placeholder: %q", out)
	}
	if strings.Contains(out, "```mermaid") {
		t.Errorf("raw mermaid block leaked through: %q", out)
	}
}

func TestMermaidPreprocessor_ScaleHint(t *testing.T) {
	p := newMermaidPreprocessor(MermaidSVG)
	src := "<!-- vellum:scale 0.6 -->\n```mermaid\ngraph LR\n  X --> Y\n```\n"
	out := p.Extract(src)

	if len(p.diagrams) != 1 {
		t.Fatalf("expected 1 diagram, got %d", len(p.diagrams))
	}
	if p.diagrams[0].scale != 0.6 {
		t.Errorf("scale = %v, want 0.6", p.diagrams[0].scale)
	}
	if want := "graph LR\n  X --> Y"; p.diagrams[0].source != want {
		t.Errorf("source = %q, want %q", p.diagrams[0].source, want)
	}
	if !strings.Contains(out, "<!--MERMAID:0-->") {
		t.Errorf("output missing placeholder: %q", out)
	}
	// The hint comment is part of the match, so it should be gone from out.
	if strings.Contains(out, "vellum:scale") {
		t.Errorf("scale hint should be consumed with the block: %q", out)
	}
}

func TestMermaidPreprocessor_NoHintDefaultScale(t *testing.T) {
	p := newMermaidPreprocessor(MermaidSVG)
	src := "```mermaid\ngraph TD\n  A --> B\n```\n"
	p.Extract(src)
	if len(p.diagrams) != 1 {
		t.Fatalf("expected 1 diagram, got %d", len(p.diagrams))
	}
	if p.diagrams[0].scale != 1.0 {
		t.Errorf("scale without hint = %v, want 1.0", p.diagrams[0].scale)
	}
}

func TestMermaidPreprocessor_MultipleBlocksInOrder(t *testing.T) {
	p := newMermaidPreprocessor(MermaidSVG)
	src := strings.Join([]string{
		"First:",
		"",
		"```mermaid",
		"graph TD",
		"  A --> B",
		"```",
		"",
		"Second (scaled):",
		"",
		"<!-- vellum:scale 0.5 -->",
		"```mermaid",
		"graph LR",
		"  C --> D",
		"```",
		"",
		"Third:",
		"",
		"```mermaid",
		"graph TD",
		"  E --> F",
		"```",
		"",
	}, "\n")
	out := p.Extract(src)

	if len(p.diagrams) != 3 {
		t.Fatalf("expected 3 diagrams, got %d: %#v", len(p.diagrams), p.diagrams)
	}

	wants := []struct {
		src   string
		scale float64
	}{
		{"graph TD\n  A --> B", 1.0},
		{"graph LR\n  C --> D", 0.5},
		{"graph TD\n  E --> F", 1.0},
	}
	for i, w := range wants {
		if p.diagrams[i].source != w.src {
			t.Errorf("diagram[%d] source = %q, want %q", i, p.diagrams[i].source, w.src)
		}
		if p.diagrams[i].scale != w.scale {
			t.Errorf("diagram[%d] scale = %v, want %v", i, p.diagrams[i].scale, w.scale)
		}
	}

	// All three placeholders must appear in order.
	matches := mermaidPlaceholderRe.FindAllString(out, -1)
	wantPlaceholders := []string{"<!--MERMAID:0-->", "<!--MERMAID:1-->", "<!--MERMAID:2-->"}
	if len(matches) != len(wantPlaceholders) {
		t.Fatalf("placeholder count = %d, want %d; out=%q", len(matches), len(wantPlaceholders), out)
	}
	for i, want := range wantPlaceholders {
		if matches[i] != want {
			t.Errorf("placeholder[%d] = %q, want %q", i, matches[i], want)
		}
	}
}

func TestMermaidPreprocessor_NonMermaidCodeBlocksUntouched(t *testing.T) {
	p := newMermaidPreprocessor(MermaidSVG)
	src := strings.Join([]string{
		"```go",
		"func main() { println(\"hi\") }",
		"```",
		"",
		"```python",
		"print('hi')",
		"```",
		"",
		"```",
		"plain code block",
		"```",
		"",
	}, "\n")
	out := p.Extract(src)

	if len(p.diagrams) != 0 {
		t.Errorf("non-mermaid blocks should not be captured: %#v", p.diagrams)
	}
	if out != src {
		t.Errorf("Extract mutated source containing only non-mermaid code blocks:\n in:  %q\n out: %q", src, out)
	}
}

func TestMermaidReplaceAll_SoftFailureKeepsFallbackAndReports(t *testing.T) {
	prev := renderMermaidFn
	t.Cleanup(func() { renderMermaidFn = prev })
	renderMermaidFn = func(ctx context.Context, src string, format string) (string, error) {
		return "", fmt.Errorf("mmdc: simulated failure")
	}

	p := newMermaidPreprocessor(MermaidSVG)
	src := "```mermaid\ngraph TD\n  A --> B\n```\n"
	extracted := p.Extract(src)
	// goldmark would leave the placeholder as text; feed it as HTML directly.
	html := "<p>" + extracted + "</p>"
	out, soft := p.ReplaceAll(context.Background(), html)
	if len(soft) != 1 {
		t.Fatalf("soft=%v", soft)
	}
	if !strings.Contains(soft[0], "mermaid diagram 1:") {
		t.Errorf("soft msg=%q", soft[0])
	}
	if !strings.Contains(soft[0], "simulated failure") {
		t.Errorf("soft msg missing cause: %q", soft[0])
	}
	if !strings.Contains(out, `class="mermaid-error"`) {
		t.Errorf("expected source-as-code fallback in HTML: %q", out)
	}
	if !strings.Contains(out, "graph TD") {
		t.Errorf("fallback should include mermaid source: %q", out)
	}
}

func TestRender_MermaidSoftErrorPropagates(t *testing.T) {
	prev := renderMermaidFn
	t.Cleanup(func() { renderMermaidFn = prev })
	renderMermaidFn = func(ctx context.Context, src string, format string) (string, error) {
		return "", fmt.Errorf("mmdc: boom")
	}

	html, soft, err := Render(context.Background(), []byte("# T\n\n```mermaid\ngraph TD\n  A-->B\n```\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if html == "" {
		t.Fatal("expected HTML despite soft failure")
	}
	if len(soft) != 1 || !strings.Contains(soft[0], "mermaid diagram 1") {
		t.Fatalf("soft=%v", soft)
	}
	if !strings.Contains(html, "mermaid-error") {
		t.Errorf("html missing fallback: %s", html[:min(200, len(html))])
	}
}

func TestRun_MermaidSoftErrorInResult(t *testing.T) {
	prev := renderMermaidFn
	t.Cleanup(func() { renderMermaidFn = prev })
	renderMermaidFn = func(ctx context.Context, src string, format string) (string, error) {
		return "", fmt.Errorf("mmdc: boom")
	}

	res, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaContent, Content: "# T\n\n```mermaid\ngraph TD\n  A-->B\n```\n"},
		To:   Endpoint{Media: MediaContent, Format: FormatHTML},
	})
	if err == nil {
		t.Fatal("expected soft error")
	}
	var se *SoftError
	if !errors.As(err, &se) {
		t.Fatalf("want SoftError, got %T %v", err, err)
	}
	if res == nil || res.Content == "" {
		t.Fatal("expected content despite soft error")
	}
	if len(res.Errors) == 0 {
		t.Fatal("expected errors array populated")
	}
}

func TestMermaidPreprocessor_InvalidScaleFallsBackToDefault(t *testing.T) {
	// The regex only captures [0-9.]+, so "abc" wouldn't match at all; a
	// zero or negative value after parsing should also be rejected. Use
	// a value like "0" to exercise the v>0 guard.
	p := newMermaidPreprocessor(MermaidSVG)
	src := "<!-- vellum:scale 0 -->\n```mermaid\ngraph TD\n  A --> B\n```\n"
	p.Extract(src)
	if len(p.diagrams) != 1 {
		t.Fatalf("expected 1 diagram, got %d", len(p.diagrams))
	}
	if p.diagrams[0].scale != 1.0 {
		t.Errorf("scale with invalid hint = %v, want 1.0 (default)", p.diagrams[0].scale)
	}
}

// 🎯T17: HTML path embeds vector SVG (not raster-only PNG).
func TestRender_MermaidHTMLUsesSVG(t *testing.T) {
	prev := renderMermaidFn
	t.Cleanup(func() { renderMermaidFn = prev })
	var gotFormat string
	renderMermaidFn = func(ctx context.Context, src string, format string) (string, error) {
		gotFormat = format
		return `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><text>A</text></svg>`, nil
	}

	html, soft, err := Render(context.Background(), []byte("# T\n\n```mermaid\ngraph TD\n  A-->B\n```\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(soft) != 0 {
		t.Fatalf("soft=%v", soft)
	}
	if gotFormat != MermaidSVG {
		t.Errorf("Render format = %q, want %q", gotFormat, MermaidSVG)
	}
	if !strings.Contains(html, "<svg") {
		t.Errorf("HTML path must embed vector SVG; missing <svg in output")
	}
	if strings.Contains(html, "data:image/png") {
		t.Errorf("HTML path must not embed PNG data URI")
	}
}

// 🎯T17: explicit PNG option still produces PNG img.
func TestRender_MermaidPNGWhenRequested(t *testing.T) {
	prev := renderMermaidFn
	t.Cleanup(func() { renderMermaidFn = prev })
	var gotFormat string
	renderMermaidFn = func(ctx context.Context, src string, format string) (string, error) {
		gotFormat = format
		return `<img src="data:image/png;base64,QQ==" alt="Mermaid diagram">`, nil
	}

	html, soft, err := Render(context.Background(), []byte("```mermaid\ngraph TD\n  A-->B\n```\n"),
		&Options{MermaidFormat: MermaidPNG})
	if err != nil {
		t.Fatal(err)
	}
	if len(soft) != 0 {
		t.Fatalf("soft=%v", soft)
	}
	if gotFormat != MermaidPNG {
		t.Errorf("format = %q, want %q", gotFormat, MermaidPNG)
	}
	if !strings.Contains(html, "data:image/png") {
		t.Errorf("PNG path must embed data:image/png")
	}
	if strings.Contains(html, "<svg") {
		t.Errorf("PNG path must not embed raw <svg")
	}
}

// 🎯T17: Run HTML content sink requests SVG.
func TestRun_MermaidHTMLContentUsesSVG(t *testing.T) {
	prev := renderMermaidFn
	t.Cleanup(func() { renderMermaidFn = prev })
	var gotFormat string
	renderMermaidFn = func(ctx context.Context, src string, format string) (string, error) {
		gotFormat = format
		return `<svg xmlns="http://www.w3.org/2000/svg"><text>ok</text></svg>`, nil
	}

	res, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaContent, Content: "```mermaid\ngraph TD\n  A-->B\n```\n"},
		To:   Endpoint{Media: MediaContent, Format: FormatHTML},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotFormat != MermaidSVG {
		t.Errorf("HTML Run format = %q, want %q", gotFormat, MermaidSVG)
	}
	if res == nil || !strings.Contains(res.Content, "<svg") {
		t.Errorf("HTML content missing <svg")
	}
	if res != nil && strings.Contains(res.Content, "data:image/png") {
		t.Errorf("HTML content must not be PNG-only")
	}
}

// 🎯T17: PDF materialization forces PNG (Prince-safe labels).
func TestWithMermaidFormat_PDFForcesPNG(t *testing.T) {
	prev := renderMermaidFn
	t.Cleanup(func() { renderMermaidFn = prev })
	var gotFormat string
	renderMermaidFn = func(ctx context.Context, src string, format string) (string, error) {
		gotFormat = format
		return `<img src="data:image/png;base64,QQ==" alt="Mermaid diagram">`, nil
	}

	// writeFileOutput PDF path uses withMermaidFormat(..., MermaidPNG).
	// Exercise via Render with the same opts Convert/writeFileOutput use.
	opts := withMermaidFormat(nil, MermaidPNG)
	html, soft, err := Render(context.Background(), []byte("```mermaid\ngraph TD\n  A-->B\n```\n"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(soft) != 0 {
		t.Fatalf("soft=%v", soft)
	}
	if gotFormat != MermaidPNG {
		t.Errorf("PDF opts format = %q, want %q", gotFormat, MermaidPNG)
	}
	if !strings.Contains(html, "data:image/png") {
		t.Errorf("PDF path must keep PNG")
	}
}

func TestEmbedMermaidSVG_StripsProlog(t *testing.T) {
	in := []byte(`<?xml version="1.0"?><!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd"><svg xmlns="http://www.w3.org/2000/svg"><text>A</text></svg>`)
	out := embedMermaidSVG(in)
	if !strings.HasPrefix(out, "<svg") {
		t.Errorf("want leading <svg, got %q", out[:min(40, len(out))])
	}
	if strings.Contains(out, "<?xml") || strings.Contains(out, "<!DOCTYPE") {
		t.Errorf("prolog should be stripped: %q", out)
	}
}

func TestResolveMermaidFormat(t *testing.T) {
	if got := resolveMermaidFormat(""); got != MermaidSVG {
		t.Errorf("empty = %q, want svg", got)
	}
	if got := resolveMermaidFormat("PNG"); got != MermaidPNG {
		t.Errorf("PNG = %q, want png", got)
	}
	if got := resolveMermaidFormat("svg"); got != MermaidSVG {
		t.Errorf("svg = %q", got)
	}
}
