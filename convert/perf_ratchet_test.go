// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"fmt"
	"testing"

	"github.com/marcelocantos/vellum/internal/perfbase"
	"github.com/marcelocantos/vellum/internal/perffixture"
)

// Allocation ratchets for the pure-Go render paths. External renderers
// (node, mmdc, WeasyPrint) are excluded or stubbed so the numbers are
// deterministic; see internal/perfbase for the band and how to
// re-record.
func TestRatchet_RenderProse(t *testing.T) {
	src := []byte(perffixture.Markdown(proseArgs))
	perfbase.Ratchet(t, "render_prose", func() {
		if _, _, err := Render(context.Background(), src, nil); err != nil {
			t.Fatal(err)
		}
	})
}

// A quarter of the benchmark's code fixture: chroma under the race
// detector is slow enough that the full fixture cost 40 s per gate.
var codeRatchetArgs = &perffixture.MarkdownArgs{Sections: 25, Paragraphs: 2, CodeBlocks: 10}

func TestRatchet_RenderCode(t *testing.T) {
	src := []byte(perffixture.Markdown(codeRatchetArgs))
	perfbase.Ratchet(t, "render_code", func() {
		if _, _, err := Render(context.Background(), src, nil); err != nil {
			t.Fatal(err)
		}
	})
}

// Math with node stubbed out: the placeholder extraction and
// substitution around the renderer, which is where the quadratic copy
// lived.
func TestRatchet_RenderMathSubstitution(t *testing.T) {
	src := perffixture.Markdown(mathArgs)
	perfbase.Ratchet(t, "render_math_substitution", func() {
		m := newMathPreprocessor()
		processed := m.Extract(src)
		html, _, err := renderMarkdown([]byte(processed))
		if err != nil {
			t.Fatal(err)
		}
		rendered := make([]string, len(m.exprs))
		for i := range rendered {
			rendered[i] = `<span class="katex">x</span>`
		}
		substitutePlaceholders(html, mathPlaceholderTag, rendered)
	})
}

func TestRatchet_RenderMermaidStub(t *testing.T) {
	png := perffixture.PNG(800, 600)
	restore := renderMermaidFn
	renderMermaidFn = func(context.Context, string, string) (string, error) {
		return fmt.Sprintf(`<img src="data:image/png;base64,%x">`, png[:len(png)/2]), nil
	}
	t.Cleanup(func() { renderMermaidFn = restore })
	src := []byte(perffixture.Markdown(mermaidArgs))
	perfbase.Ratchet(t, "render_mermaid_stub", func() {
		if _, _, err := Render(context.Background(), src, &Options{MermaidFormat: MermaidPNG}); err != nil {
			t.Fatal(err)
		}
	})
}
