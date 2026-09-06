// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/vellum/internal/perffixture"
)

// Benchmark fixtures for the conversion hot paths. Numbers are recorded
// in docs/perf/baseline.md; the pure-Go ones are also locked as a
// ratchet in perf_ratchet_test.go. Run with:
//
//	cv bench
//
// Fixtures are generated procedurally (internal/perffixture) into a
// tempdir, never checked in.

// Shapes chosen so that each benchmark isolates one cost centre.
var (
	// proseArgs is ~1 MiB of headings, paragraphs, lists and tables:
	// goldmark plus template assembly, nothing external.
	proseArgs = &perffixture.MarkdownArgs{Sections: 200, Paragraphs: 12, Tables: 2}
	// codeArgs leans on chroma highlighting.
	codeArgs = &perffixture.MarkdownArgs{Sections: 100, Paragraphs: 2, CodeBlocks: 10}
	// mathArgs has many small expressions: one node spawn, then
	// placeholder substitution in the rendered HTML.
	mathArgs = &perffixture.MarkdownArgs{Sections: 100, Paragraphs: 2, InlineMath: 20, BlockMath: 4}
	// mermaidArgs has many diagrams; renderMermaidFn is stubbed so the
	// cost measured is placeholder substitution with a large PNG payload.
	mermaidArgs = &perffixture.MarkdownArgs{Sections: 50, Paragraphs: 1, Mermaid: 4}
	// imageArgs references many image files (materialised in a tempdir).
	imageArgs = &perffixture.MarkdownArgs{Sections: 40, Paragraphs: 2, Images: 5}
)

func haveTool(b *testing.B, tool string) {
	b.Helper()
	if _, err := exec.LookPath(tool); err != nil {
		b.Skipf("%s not on PATH", tool)
	}
}

func BenchmarkRender(b *testing.B) {
	cases := []struct {
		name string
		args *perffixture.MarkdownArgs
		tool string
	}{
		{"prose", proseArgs, ""},
		{"code", codeArgs, ""},
		{"math", mathArgs, "node"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			if c.tool != "" {
				haveTool(b, c.tool)
			}
			src := []byte(perffixture.Markdown(c.args))
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := Render(context.Background(), src, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkRenderMermaid stubs mmdc with a fixed PNG so the measurement
// is the Go-side cost of embedding many diagrams, not browser startup.
func BenchmarkRenderMermaid(b *testing.B) {
	png := perffixture.PNG(800, 600)
	restore := renderMermaidFn
	renderMermaidFn = func(context.Context, string, string) (string, error) {
		return fmt.Sprintf(`<img src="data:image/png;base64,%x">`, png[:len(png)/2]), nil
	}
	b.Cleanup(func() { renderMermaidFn = restore })
	src := []byte(perffixture.Markdown(mermaidArgs))
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := Render(context.Background(), src, &Options{MermaidFormat: MermaidPNG}); err != nil {
			b.Fatal(err)
		}
	}
}

// writeImageFixture materialises imageArgs into dir and returns the
// Markdown path.
func writeImageFixture(tb testing.TB, dir string) string {
	tb.Helper()
	png := perffixture.PNG(640, 480)
	for i := 0; i < perffixture.ImageCount(imageArgs); i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("img-%d.png", i)), png, 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	path := filepath.Join(dir, "images.md")
	if err := os.WriteFile(path, []byte(perffixture.Markdown(imageArgs)), 0o644); err != nil {
		tb.Fatal(err)
	}
	return path
}

// BenchmarkConvertPDF measures the full Markdown → PDF path through the
// default backend. Dominated by WeasyPrint; recorded so a regression in
// what vellum hands the backend (HTML size, image handling) is visible.
func BenchmarkConvertPDF(b *testing.B) {
	haveTool(b, "weasyprint")
	dir := b.TempDir()
	prosePath := filepath.Join(dir, "prose.md")
	if err := os.WriteFile(prosePath, []byte(perffixture.Markdown(
		&perffixture.MarkdownArgs{Sections: 30, Paragraphs: 6, Tables: 1})), 0o644); err != nil {
		b.Fatal(err)
	}
	imagePath := writeImageFixture(b, dir)
	for _, c := range []struct{ name, path string }{{"prose", prosePath}, {"images", imagePath}} {
		b.Run(c.name, func(b *testing.B) {
			out := filepath.Join(dir, c.name+".pdf")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := Convert(context.Background(), c.path, out, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
