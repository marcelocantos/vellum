// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package perffixture builds large, deterministic inputs for the
// conversion benchmarks: Markdown documents, PNG images, and multi-page
// PDFs. Everything is generated at run time so no large binary ever
// enters the repository; benchmarks write the bytes into a test tempdir.
package perffixture

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"strings"
)

// Seed keeps every generated fixture identical across runs so that
// before/after numbers compare the same bytes.
const Seed = 20260906

// MarkdownArgs shapes a generated document. Counts are per section;
// zero disables the feature. Sections is the number of top-level
// sections, each with its own heading, paragraphs, and optional extras.
type MarkdownArgs struct {
	Sections   int
	Paragraphs int // prose paragraphs per section
	CodeBlocks int // fenced Go code blocks per section (exercises chroma)
	InlineMath int // $…$ expressions per section
	BlockMath  int // $$…$$ displays per section
	Tables     int // GFM tables per section
	Images     int // ![…](img-N.png) references per section
	Mermaid    int // ```mermaid blocks per section
}

// Markdown returns a document built from args. The text is lorem-like
// but drawn from a fixed word list with a fixed seed, so two calls with
// the same args return byte-identical output.
func Markdown(args *MarkdownArgs) string {
	rng := rand.New(rand.NewSource(Seed))
	var b strings.Builder
	b.WriteString("---\ntitle: Perf fixture\n---\n\n")
	b.WriteString("# Perf fixture\n\n")
	imageIndex := 0
	for s := 0; s < args.Sections; s++ {
		fmt.Fprintf(&b, "## Section %d\n\n", s+1)
		for p := 0; p < args.Paragraphs; p++ {
			b.WriteString(paragraph(rng, 60))
			b.WriteString("\n\n")
		}
		if args.Paragraphs > 0 {
			b.WriteString("- ")
			b.WriteString(paragraph(rng, 12))
			b.WriteString("\n- ")
			b.WriteString(paragraph(rng, 12))
			b.WriteString(" with `inline code` and **bold** text\n\n")
		}
		for c := 0; c < args.CodeBlocks; c++ {
			b.WriteString("```go\n")
			fmt.Fprintf(&b, "// Block %d of section %d.\nfunc f%d(xs []int) int {\n\ttotal := 0\n\tfor _, x := range xs {\n\t\tif x%%2 == 0 {\n\t\t\ttotal += x\n\t\t}\n\t}\n\treturn total\n}\n", c, s, c)
			b.WriteString("```\n\n")
		}
		for m := 0; m < args.InlineMath; m++ {
			fmt.Fprintf(&b, "The value $x_{%d}^2 + y_{%d}$ appears inline. ", m, s)
		}
		if args.InlineMath > 0 {
			b.WriteString("\n\n")
		}
		for m := 0; m < args.BlockMath; m++ {
			fmt.Fprintf(&b, "$$\n\\int_0^{%d} e^{-t} \\, dt = 1 - e^{-%d}\n$$\n\n", m+1, m+1)
		}
		for t := 0; t < args.Tables; t++ {
			b.WriteString("| Name | Count | Note |\n|------|------:|------|\n")
			for r := 0; r < 8; r++ {
				fmt.Fprintf(&b, "| row-%d | %d | %s |\n", r, rng.Intn(1000), word(rng))
			}
			b.WriteString("\n")
		}
		for i := 0; i < args.Images; i++ {
			fmt.Fprintf(&b, "![figure %d](img-%d.png)\n\n", imageIndex, imageIndex)
			imageIndex++
		}
		for d := 0; d < args.Mermaid; d++ {
			fmt.Fprintf(&b, "```mermaid\ngraph TD\n  A%d[Start] --> B%d[Work]\n  B%d --> C%d[End]\n```\n\n", d, d, d, d)
		}
	}
	return b.String()
}

// ImageCount reports how many image references Markdown(args) emits,
// so callers can materialise exactly that many files.
func ImageCount(args *MarkdownArgs) int { return args.Sections * args.Images }

var words = strings.Fields(`
document conversion pipeline renders markdown into typeset pages while
keeping structure headings lists tables and code intact across every
medium the server supports including clipboard file references and
inline content so agents can move text between tools without loss`)

func word(rng *rand.Rand) string { return words[rng.Intn(len(words))] }

func paragraph(rng *rand.Rand, n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = word(rng)
	}
	s := strings.Join(parts, " ")
	return strings.ToUpper(s[:1]) + s[1:] + "."
}

// PNG returns an encoded w×h image with a deterministic gradient, large
// enough to be a realistic photo-like asset once w and h are in the
// hundreds.
func PNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // encoding an in-memory RGBA cannot fail
	}
	return buf.Bytes()
}

// PDF returns a minimal but valid multi-page PDF with a line of text on
// each page, enough for pdftoppm and pdftotext to have real work to do.
// Object numbering: 1 catalog, 2 pages, 3 font, then (page, content)
// pairs.
func PDF(pages int) []byte {
	const (
		firstPageObj = 4
		objsPerPage  = 2
	)
	var out bytes.Buffer
	offsets := make([]int, 0, firstPageObj+pages*objsPerPage)
	obj := func(body string) {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	out.WriteString("%PDF-1.4\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	var kids strings.Builder
	for p := 0; p < pages; p++ {
		fmt.Fprintf(&kids, "%d 0 R ", firstPageObj+p*objsPerPage)
	}
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.TrimSpace(kids.String()), pages))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for p := 0; p < pages; p++ {
		pageObj := firstPageObj + p*objsPerPage
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 3 0 R >> >> >>", pageObj+1))
		stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (Perf fixture page %d of %d) Tj ET", p+1, pages)
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return out.Bytes()
}
