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
	"os/exec"
	"path/filepath"
	"strings"

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

// Options configures the conversion pipeline.
type Options struct {
	// CSS overrides the default GitHub-style CSS.
	CSS string
	// HeadExtra is additional HTML to inject into <head> (e.g., KaTeX CSS).
	HeadExtra string
}

var htmlTmpl = template.Must(template.New("page").Parse(embed.HTMLTemplate))

func newGoldmark() goldmark.Markdown {
	return goldmark.New(
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
}

// Convert reads a Markdown file at inputPath and writes a PDF to outputPath.
func Convert(ctx context.Context, inputPath, outputPath string, opts *Options) error {
	src, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}

	htmlContent, title, err := renderMarkdown(src)
	if err != nil {
		return fmt.Errorf("rendering markdown: %w", err)
	}

	css := embed.GitHubCSS
	headExtra := ""
	if opts != nil {
		if opts.CSS != "" {
			css = opts.CSS
		}
		headExtra = opts.HeadExtra
	}

	// Generate syntax highlighting CSS from chroma.
	var chromaBuf bytes.Buffer
	formatter := chromahtml.New(chromahtml.WithClasses(true), chromahtml.WithAllClasses(true))
	if err := formatter.WriteCSS(&chromaBuf, styles.Get("github")); err != nil {
		return fmt.Errorf("generating chroma CSS: %w", err)
	}
	css += "\n" + chromaBuf.String()

	fullHTML, err := assembleHTML(title, css, headExtra, htmlContent)
	if err != nil {
		return fmt.Errorf("assembling HTML: %w", err)
	}

	if err := prince(ctx, fullHTML, outputPath); err != nil {
		return fmt.Errorf("prince: %w", err)
	}

	return nil
}

func renderMarkdown(src []byte) (htmlContent string, title string, err error) {
	md := newGoldmark()

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

func assembleHTML(title, css, headExtra, body string) (string, error) {
	data := struct {
		Title     string
		CSS       template.CSS
		HeadExtra template.HTML
		Body      template.HTML
	}{
		Title:     title,
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

func prince(ctx context.Context, htmlContent, outputPath string) error {
	// Ensure output directory exists.
	if dir := filepath.Dir(outputPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	// Write HTML to a temp file for Prince to consume.
	tmpFile, err := os.CreateTemp("", "vellum-*.html")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(htmlContent); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	cmd := exec.CommandContext(ctx, "prince", tmpFile.Name(), "-o", outputPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}

	return nil
}
