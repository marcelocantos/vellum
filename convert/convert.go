// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package convert implements the Markdown → HTML → PDF pipeline.
package convert

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"os"
	"path/filepath"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"

	"github.com/marcelocantos/vellum/embed"
)

const katexCSSLink = `<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/katex@0.16.21/dist/katex.min.css">`

// Options configures the conversion pipeline.
type Options struct {
	// CSS overrides the default GitHub-style CSS.
	CSS string
	// HeadExtra is additional HTML to inject into <head> (e.g., KaTeX CSS).
	HeadExtra string
	// Style applies a small set of CSS overrides on top of the base
	// stylesheet (whether default or supplied via CSS).
	Style *Style
	// Backend names the renderer engine. Empty resolves to DefaultBackend
	// (WeasyPrint). Use BackendPrince to opt into Prince.
	Backend string
}

var htmlTmpl = template.Must(template.New("page").Parse(embed.HTMLTemplate))

var md = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		extension.Footnote,
		extension.DefinitionList,
		extension.Typographer,
		meta.Meta,
		highlighting.NewHighlighting(
			highlighting.WithStyle("github"),
			highlighting.WithFormatOptions(
				chromahtml.WithClasses(true),
				chromahtml.WithAllClasses(true),
			),
		),
	),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
	),
	goldmark.WithRendererOptions(
		html.WithUnsafe(),
	),
)

// Convert reads a Markdown file at inputPath and writes a PDF to outputPath
// using the backend selected via opts.Backend (defaults to WeasyPrint).
func Convert(ctx context.Context, inputPath, outputPath string, opts *Options) error {
	absInput, err := filepath.Abs(inputPath)
	if err != nil {
		return fmt.Errorf("resolving input path: %w", err)
	}
	html, err := RenderFile(ctx, absInput, opts)
	if err != nil {
		return err
	}

	var (
		backendName string
		pdfa        string
	)
	if opts != nil {
		backendName = opts.Backend
		if opts.Style != nil {
			pdfa = opts.Style.PDFA
		}
	}
	backend, err := ResolveBackend(backendName)
	if err != nil {
		return err
	}
	if err := backend.Render(ctx, html, outputPath, filepath.Dir(absInput), pdfa); err != nil {
		return fmt.Errorf("%s: %w", backend.Name(), err)
	}
	return nil
}

// RenderFile reads a Markdown file and returns the fully assembled HTML
// page (template + CSS + body), ready to hand to Prince or to a clipboard
// backend that expects rich HTML.
func RenderFile(ctx context.Context, inputPath string, opts *Options) (string, error) {
	src, err := os.ReadFile(inputPath)
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return Render(ctx, src, opts)
}

// Render runs the conversion pipeline on src and returns the assembled
// HTML page. Math and Mermaid blocks are pre-extracted, rendered, and
// reinjected; chroma syntax-highlighting CSS is appended; the result is
// wrapped in the embedded HTML template.
func Render(ctx context.Context, src []byte, opts *Options) (string, error) {
	// Pre-process: extract math and mermaid blocks before goldmark sees them.
	// This prevents goldmark from mangling backslashes in LaTeX and from
	// treating mermaid blocks as regular code.
	math := newMathPreprocessor()
	mermaid := newMermaidPreprocessor()
	processed := math.Extract(string(src))
	processed = mermaid.Extract(processed)

	htmlContent, title, err := renderMarkdown([]byte(processed))
	if err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}

	htmlContent, err = math.ReplaceAll(ctx, htmlContent)
	if err != nil {
		return "", fmt.Errorf("rendering math: %w", err)
	}
	htmlContent, err = mermaid.ReplaceAll(ctx, htmlContent)
	if err != nil {
		return "", fmt.Errorf("rendering mermaid: %w", err)
	}

	css := embed.GitHubCSS
	headExtra := katexCSSLink
	if opts != nil {
		if opts.CSS != "" {
			css = opts.CSS
		}
		if opts.HeadExtra != "" {
			headExtra += "\n" + opts.HeadExtra
		}
	}

	var chromaBuf bytes.Buffer
	formatter := chromahtml.New(chromahtml.WithClasses(true), chromahtml.WithAllClasses(true))
	if err := formatter.WriteCSS(&chromaBuf, styles.Get("github")); err != nil {
		return "", fmt.Errorf("generating chroma CSS: %w", err)
	}
	css += "\n" + chromaBuf.String()

	// Style overrides cascade last so they win regardless of how the base CSS
	// was supplied (default or via opts.CSS).
	if opts != nil {
		if override := opts.Style.CSS(); override != "" {
			css += "\n" + override
		}
	}

	var lang string
	if opts != nil {
		lang = opts.Style.EffectiveLang()
	}
	fullHTML, err := assembleHTML(title, lang, css, headExtra, htmlContent)
	if err != nil {
		return "", fmt.Errorf("assembling HTML: %w", err)
	}

	if debugPath := os.Getenv("VELLUM_DEBUG_HTML"); debugPath != "" {
		os.WriteFile(debugPath, []byte(fullHTML), 0o644)
	}

	return fullHTML, nil
}

func renderMarkdown(src []byte) (htmlContent string, title string, err error) {
	pctx := parser.NewContext()
	var buf bytes.Buffer
	if err := md.Convert(src, &buf, parser.WithContext(pctx)); err != nil {
		return "", "", err
	}

	// Extract title from front-matter if present.
	metadata := meta.Get(pctx)
	if t, ok := metadata["title"]; ok {
		if s, ok := t.(string); ok {
			title = s
		}
	}

	return buf.String(), title, nil
}

func assembleHTML(title, lang, css, headExtra, body string) (string, error) {
	data := struct {
		Title     string
		Lang      string
		CSS       template.CSS
		HeadExtra template.HTML
		Body      template.HTML
	}{
		Title:     title,
		Lang:      lang,
		CSS:       template.CSS(css),
		HeadExtra: template.HTML(headExtra),
		Body:      template.HTML(body),
	}

	var buf bytes.Buffer
	if err := htmlTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

