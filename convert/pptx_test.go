// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/embed"
	"github.com/marcelocantos/vellum/internal/pandoc"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

// deckMarkdown exercises every construct the pptx sink promises to
// carry: a title block, a section heading above the slide level, slides
// with a list, a table, a picture, code, math, and speaker notes.
const deckMarkdown = `---
title: Quarterly Review
author: Test Author
---

# Section One

## Agenda

- Item one
- Item two

## Numbers

| Name | Value |
|------|-------|
| a    | 1     |

## Picture

![A square](IMG)

## Code

` + "```go\nfunc main() {}\n```" + `

## Math

Inline $E = mc^2$ here.

::: notes
Speaker note text
:::
`

// slideFileRe matches a slide part inside the pptx zip.
var slideFileRe = regexp.MustCompile(`^ppt/slides/slide\d+\.xml$`)

// readPPTX opens a generated deck and returns its parts by name.
func readPPTX(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("%s is not a zip: %v", path, err)
	}
	defer r.Close()
	parts := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		parts[f.Name] = string(b)
	}
	return parts
}

func countSlides(parts map[string]string) int {
	n := 0
	for name := range parts {
		if slideFileRe.MatchString(name) {
			n++
		}
	}
	return n
}

func hasPart(parts map[string]string, prefix string) bool {
	for name := range parts {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// tinyPNG returns a valid 2x2 PNG, and its data URI, for image fixtures.
func tinyPNG(t *testing.T) ([]byte, string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for x := range 2 {
		for y := range 2 {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestRunMarkdownToPPTXFile(t *testing.T) {
	testdeps.Need(t, pandoc.Binary)
	dir := t.TempDir()
	pngBytes, _ := tinyPNG(t)
	if err := os.WriteFile(filepath.Join(dir, "square.png"), pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	in := filepath.Join(dir, "deck.md")
	// A relative image path proves --resource-path points at the source
	// directory rather than the process working directory.
	if err := os.WriteFile(in, []byte(strings.Replace(deckMarkdown, "IMG", "square.png", 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaFile, Path: in},
		To:   Endpoint{Media: MediaFile, Path: filepath.Join(dir, "deck.pptx")},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ToFormat != FormatPPTX {
		t.Errorf("to_format = %q, want %q (inferred from the .pptx extension)", res.ToFormat, FormatPPTX)
	}
	if len(res.Paths) != 1 || res.Paths[0] != filepath.Join(dir, "deck.pptx") {
		t.Fatalf("paths = %v", res.Paths)
	}
	parts := readPPTX(t, res.Paths[0])

	// Title slide + section slide + five content slides.
	if got := countSlides(parts); got != 7 {
		t.Errorf("slides = %d, want 7", got)
	}
	if !strings.Contains(parts["ppt/slides/slide1.xml"], "Quarterly Review") {
		t.Errorf("slide 1 is not the title slide: %q", parts["ppt/slides/slide1.xml"])
	}
	if !strings.Contains(parts["ppt/slides/slide2.xml"], "Section One") {
		t.Errorf("slide 2 is not the section slide: %q", parts["ppt/slides/slide2.xml"])
	}
	if !hasPart(parts, "ppt/media/") {
		t.Error("deck has no media part; the relative image was dropped")
	}
	if !hasPart(parts, "ppt/notesSlides/") {
		t.Error("deck has no notes slide; ::: notes was dropped")
	}
	var mathSlide, tableSlide, codeSlide bool
	for name, body := range parts {
		if !slideFileRe.MatchString(name) {
			continue
		}
		mathSlide = mathSlide || strings.Contains(body, "<m:oMath")
		tableSlide = tableSlide || strings.Contains(body, "<a:tbl>")
		// Highlighted code arrives as one run per token in a monospace
		// typeface, so look for the token and the face, not the line.
		codeSlide = codeSlide || (strings.Contains(body, "<a:t>func</a:t>") && strings.Contains(body, `typeface="Courier"`))
	}
	if !mathSlide {
		t.Error("no slide carries native math (<m:oMath>)")
	}
	if !tableSlide {
		t.Error("no slide carries a table (<a:tbl>)")
	}
	if !codeSlide {
		t.Error("no slide carries the code block as monospace runs")
	}
}

func TestRunContentToPPTXExplicitFormat(t *testing.T) {
	testdeps.Need(t, pandoc.Binary)
	dir := t.TempDir()
	out := filepath.Join(dir, "deck.bin")
	res, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaContent, Content: "# One\n\n- a\n\n# Two\n\n- b\n"},
		To:   Endpoint{Media: MediaFile, Path: out, Format: "PowerPoint"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ToFormat != FormatPPTX {
		t.Errorf("to_format = %q, want %q", res.ToFormat, FormatPPTX)
	}
	if got := countSlides(readPPTX(t, out)); got != 2 {
		t.Errorf("slides = %d, want 2 (level-1 headings followed by content are the slide level)", got)
	}
}

// TestPPTXMermaidBecomesPicture pins the one gap pandoc leaves: a
// Mermaid block must reach the slide as a PNG picture, and a failed
// render must stay as its source with a soft error, like every other
// sink.
func TestPPTXMermaidBecomesPicture(t *testing.T) {
	testdeps.Need(t, pandoc.Binary)
	const md = "# Deck\n\n## Flow\n\n```mermaid\ngraph LR\n  A-->B\n```\n"

	t.Run("rendered diagram is a picture", func(t *testing.T) {
		prev := renderMermaidFn
		t.Cleanup(func() { renderMermaidFn = prev })
		_, dataURI := tinyPNG(t)
		var gotFormat string
		renderMermaidFn = func(ctx context.Context, src, format string) (string, error) {
			gotFormat = format
			return `<img src="` + dataURI + `" alt="Mermaid diagram">`, nil
		}

		out := filepath.Join(t.TempDir(), "flow.pptx")
		res, err := Run(context.Background(), &Request{
			From: Endpoint{Media: MediaContent, Content: md},
			To:   Endpoint{Media: MediaFile, Path: out},
		})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if gotFormat != MermaidPNG {
			t.Errorf("mermaid format = %q, want %q (PowerPoint cannot embed SVG)", gotFormat, MermaidPNG)
		}
		if len(res.Errors) != 0 {
			t.Errorf("errors = %v, want none", res.Errors)
		}
		parts := readPPTX(t, out)
		if !hasPart(parts, "ppt/media/") {
			t.Error("deck has no media part; the diagram was not embedded")
		}
		for name, body := range parts {
			if slideFileRe.MatchString(name) && strings.Contains(body, "graph LR") {
				t.Errorf("%s still carries the Mermaid source", name)
			}
		}
	})

	t.Run("failed diagram keeps its source and reports", func(t *testing.T) {
		prev := renderMermaidFn
		t.Cleanup(func() { renderMermaidFn = prev })
		renderMermaidFn = func(ctx context.Context, src, format string) (string, error) {
			return "", fmt.Errorf("mmdc: boom")
		}

		out := filepath.Join(t.TempDir(), "flow.pptx")
		res, err := Run(context.Background(), &Request{
			From: Endpoint{Media: MediaContent, Content: md},
			To:   Endpoint{Media: MediaFile, Path: out},
		})
		var se *SoftError
		if !errors.As(err, &se) {
			t.Fatalf("err = %v, want a *SoftError", err)
		}
		if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "mermaid diagram 1: mmdc: boom") {
			t.Errorf("errors = %v, want the diagram failure", res.Errors)
		}
		var kept bool
		for name, body := range readPPTX(t, out) {
			kept = kept || (slideFileRe.MatchString(name) && strings.Contains(body, "graph LR"))
		}
		if !kept {
			t.Error("the failed diagram's source is missing from the deck")
		}
	})
}

// withTheme returns a copy of the built-in reference deck whose theme is
// renamed, so a test can tell which template pandoc actually read.
func withTheme(t *testing.T, path, themeName string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(embed.ReferencePPTX), int64(len(embed.ReferencePPTX)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == "ppt/theme/theme1.xml" {
			b = bytes.Replace(b, []byte(`name="Office Theme"`), []byte(`name="`+themeName+`"`), 1)
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPPTXTemplateSelection(t *testing.T) {
	testdeps.Need(t, pandoc.Binary)
	const md = "# Slide\n\nBody.\n"
	convertWith := func(t *testing.T, template string) map[string]string {
		t.Helper()
		out := filepath.Join(t.TempDir(), "out.pptx")
		_, err := Run(context.Background(), &Request{
			From:     Endpoint{Media: MediaContent, Content: md},
			To:       Endpoint{Media: MediaFile, Path: out},
			Template: template,
		})
		if err != nil {
			t.Fatalf("Run(template=%q): %v", template, err)
		}
		return readPPTX(t, out)
	}

	t.Run("built-in by default", func(t *testing.T) {
		for _, sel := range []string{"", TemplateDefault} {
			parts := convertWith(t, sel)
			if !strings.Contains(parts["ppt/theme/theme1.xml"], `name="Office Theme"`) {
				t.Errorf("template %q: theme is not the built-in deck's", sel)
			}
		}
	})

	t.Run("path", func(t *testing.T) {
		ref := filepath.Join(t.TempDir(), "brand.pptx")
		withTheme(t, ref, "PathTheme")
		parts := convertWith(t, ref)
		if !strings.Contains(parts["ppt/theme/theme1.xml"], `name="PathTheme"`) {
			t.Error("deck does not carry the theme of the template given by path")
		}
	})

	t.Run("bare name under the templates dir", func(t *testing.T) {
		cfg := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", cfg)
		tdir := filepath.Join(cfg, "vellum", templatesDirName)
		if err := os.MkdirAll(tdir, 0o755); err != nil {
			t.Fatal(err)
		}
		withTheme(t, filepath.Join(tdir, "brand.pptx"), "NamedTheme")
		parts := convertWith(t, "brand")
		if !strings.Contains(parts["ppt/theme/theme1.xml"], `name="NamedTheme"`) {
			t.Error("deck does not carry the theme of the template named in the templates dir")
		}
	})
}

func TestResolveTemplateErrors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, cleanup, err := resolveTemplate("missing")
	cleanup()
	if err == nil || !strings.Contains(err.Error(), filepath.Join("vellum", templatesDirName, "missing.pptx")) {
		t.Errorf("bare missing name: err = %v, want one naming the templates-dir path it looked for", err)
	}

	_, cleanup, err = resolveTemplate(filepath.Join(t.TempDir(), "nope.pptx"))
	cleanup()
	if err == nil || !strings.Contains(err.Error(), "nope.pptx") {
		t.Errorf("missing path: err = %v, want one naming the path", err)
	}

	path, cleanup, err := resolveTemplate("")
	if err != nil {
		t.Fatal(err)
	}
	b, rerr := os.ReadFile(path)
	cleanup()
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Equal(b, embed.ReferencePPTX) {
		t.Error("built-in template on disk differs from the embedded bytes")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cleanup left %s behind", path)
	}
}

func TestPPTXIsOutputOnly(t *testing.T) {
	dir := t.TempDir()
	deck := filepath.Join(dir, "in.pptx")
	if err := os.WriteFile(deck, []byte("PK\x03\x04not really"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaFile, Path: deck},
		To:   Endpoint{Media: MediaContent},
	})
	if err == nil || !strings.Contains(err.Error(), "output-only") {
		t.Errorf("pptx source: err = %v, want an output-only rejection", err)
	}

	_, err = Run(context.Background(), &Request{
		From: Endpoint{Media: MediaContent, Content: "x", Format: FormatPPTX},
		To:   Endpoint{Media: MediaContent},
	})
	if err == nil || !strings.Contains(err.Error(), "output-only") {
		t.Errorf("pptx content source: err = %v, want an output-only rejection", err)
	}
}

func TestInlineMermaidPNGLeavesPlainMarkdownAlone(t *testing.T) {
	const md = "# T\n\n```go\nfmt.Println()\n```\n"
	got, soft := inlineMermaidPNG(context.Background(), md)
	if got != md || soft != nil {
		t.Errorf("inlineMermaidPNG changed a document with no diagrams: %q %v", got, soft)
	}
}
