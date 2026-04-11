// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"regexp"
	"strings"
	"testing"
)

var mermaidPlaceholderRe = regexp.MustCompile(`<!--MERMAID:(\d+)-->`)

func TestMermaidPreprocessor_SimpleBlock(t *testing.T) {
	p := newMermaidPreprocessor()
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
	p := newMermaidPreprocessor()
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
	p := newMermaidPreprocessor()
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
	p := newMermaidPreprocessor()
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
	p := newMermaidPreprocessor()
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

func TestMermaidPreprocessor_InvalidScaleFallsBackToDefault(t *testing.T) {
	// The regex only captures [0-9.]+, so "abc" wouldn't match at all; a
	// zero or negative value after parsing should also be rejected. Use
	// a value like "0" to exercise the v>0 guard.
	p := newMermaidPreprocessor()
	src := "<!-- vellum:scale 0 -->\n```mermaid\ngraph TD\n  A --> B\n```\n"
	p.Extract(src)
	if len(p.diagrams) != 1 {
		t.Fatalf("expected 1 diagram, got %d", len(p.diagrams))
	}
	if p.diagrams[0].scale != 1.0 {
		t.Errorf("scale with invalid hint = %v, want 1.0 (default)", p.diagrams[0].scale)
	}
}
