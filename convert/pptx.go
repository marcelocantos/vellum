// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/marcelocantos/vellum/embed"
	"github.com/marcelocantos/vellum/internal/pandoc"
	"github.com/marcelocantos/vellum/internal/xdg"
)

// PowerPoint output goes through pandoc's pptx writer rather than a
// hand-rolled OOXML emitter. pandoc is already a runtime dependency for
// rich-text import, its slide-carving rules (title block → title slide,
// headings above the slide level → section slides, headings at the slide
// level → one slide each, horizontal rule → new slide) are exactly the
// "sensible defaults" a Markdown author expects, and it already handles
// lists, tables, images, code, math (as native OMML) and ::: notes
// speaker notes. The one gap is Mermaid, which pandoc does not render;
// the existing mmdc path fills it below (🎯T47).

// TemplateDefault names the reference deck embedded in the binary.
const TemplateDefault = "default"

// templatesDirName is the subdirectory of the vellum config dir where
// bare template names resolve.
const templatesDirName = "templates"

// templateExts are the file extensions a bare template name may carry
// in the templates directory, in lookup order.
var templateExts = []string{".pptx", ".potx"}

// pptxReader is the pandoc input format for the pptx sink. GFM matches
// the goldmark dialect every other sink renders (pipe tables, task
// lists, strikethrough, autolinks, footnotes, $math$, YAML title block);
// fenced_divs adds ::: notes / ::: incremental / ::: columns, which
// pandoc's slide writers interpret; definition_lists matches goldmark's
// DefinitionList extension.
const pptxReader = "gfm+fenced_divs+definition_lists"

// mermaidPNGSrcRe extracts the data URI from the <img> renderMermaid
// emits for PNG output, so the rendered diagram can be re-expressed as a
// Markdown image for pandoc.
var mermaidPNGSrcRe = regexp.MustCompile(`<img src="(data:image/png;base64,[A-Za-z0-9+/=]+)"`)

// TemplatesDir returns the directory bare template names resolve under:
// $XDG_CONFIG_HOME/vellum/templates or ~/.config/vellum/templates.
func TemplatesDir() (string, error) {
	dir, err := xdg.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, templatesDirName), nil
}

// resolveTemplate turns a template selector into the reference-deck path
// pandoc reads, plus a cleanup func the caller must run once pandoc has
// finished. The selector is one of:
//
//   - "" or TemplateDefault: the built-in deck, written to a temp file;
//   - a path (contains a separator, or ends in .pptx/.potx): used as is,
//     and must exist;
//   - a bare name: <TemplatesDir>/<name>.pptx, then .potx.
func resolveTemplate(selector string) (path string, cleanup func(), err error) {
	noop := func() {}
	selector = strings.TrimSpace(selector)
	if selector == "" || selector == TemplateDefault {
		f, err := os.CreateTemp("", "vellum-reference-*.pptx")
		if err != nil {
			return "", noop, fmt.Errorf("convert: writing built-in pptx template: %w", err)
		}
		cleanup := func() { os.Remove(f.Name()) }
		if _, err := f.Write(embed.ReferencePPTX); err != nil {
			f.Close()
			cleanup()
			return "", noop, fmt.Errorf("convert: writing built-in pptx template: %w", err)
		}
		if err := f.Close(); err != nil {
			cleanup()
			return "", noop, fmt.Errorf("convert: writing built-in pptx template: %w", err)
		}
		return f.Name(), cleanup, nil
	}

	ext := strings.ToLower(filepath.Ext(selector))
	isPath := strings.ContainsRune(selector, filepath.Separator) || strings.ContainsRune(selector, '/')
	for _, e := range templateExts {
		isPath = isPath || ext == e
	}
	if isPath {
		abs, err := filepath.Abs(selector)
		if err != nil {
			return "", noop, err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", noop, fmt.Errorf("convert: pptx template %s: %w", selector, err)
		}
		return abs, noop, nil
	}

	dir, err := TemplatesDir()
	if err != nil {
		return "", noop, err
	}
	for _, e := range templateExts {
		p := filepath.Join(dir, selector+e)
		if _, err := os.Stat(p); err == nil {
			return p, noop, nil
		}
	}
	return "", noop, fmt.Errorf(
		"convert: pptx template %q not found: expected %s (or pass a path to a .pptx file; %q is the built-in deck)",
		selector, filepath.Join(dir, selector+templateExts[0]), TemplateDefault)
}

// writePPTX renders Markdown to a PowerPoint deck at outPath. Mermaid
// blocks are rendered to PNG through the shared fragment renderer and
// handed to pandoc as data-URI images; a diagram that fails to render
// stays on its slide as a code block and is reported in soft, matching
// the HTML and PDF sinks. pandoc warnings (dropped constructs, missing
// resources it tolerated) are also soft errors.
func writePPTX(ctx context.Context, md, outPath, baseDir string, opts *Options) (soft []string, err error) {
	src, soft := inlineMermaidPNG(ctx, md)

	var selector string
	if opts != nil {
		selector = opts.Template
	}
	refDoc, cleanup, err := resolveTemplate(selector)
	if err != nil {
		return soft, err
	}
	defer cleanup()

	args := []string{"--reference-doc=" + refDoc}
	if baseDir != "" {
		// Relative image paths resolve against the source document, not
		// the daemon's working directory.
		args = append(args, "--resource-path="+baseDir)
	}
	warnings, err := pandoc.ToFile(ctx, src, pptxReader, FormatPPTX, outPath, args...)
	if err != nil {
		return soft, err
	}
	return append(soft, warnings...), nil
}

// inlineMermaidPNG replaces every ```mermaid block in md with a Markdown
// image whose source is the rendered PNG as a data URI. Failed diagrams
// are restored as fenced code blocks and reported in soft (1-based
// index, same wording as ResolveFragments).
func inlineMermaidPNG(ctx context.Context, md string) (string, []string) {
	pre := newMermaidPreprocessor(MermaidPNG)
	src := pre.Extract(md)
	if len(pre.diagrams) == 0 {
		return md, nil
	}
	frags := pre.fragments()
	// Mermaid never hard-fails RenderFragments; only the KaTeX batch
	// can, and there is no math among these fragments.
	results, _ := RenderFragments(ctx, frags)

	var soft []string
	for i, d := range pre.diagrams {
		res := results[frags[i].ID]
		replacement := ""
		switch m := mermaidPNGSrcRe.FindStringSubmatch(res.HTML); {
		case res.Err != nil:
			msg := fmt.Sprintf("mermaid diagram %d: %v", i+1, res.Err)
			fmt.Fprintln(os.Stderr, "Error:", msg)
			soft = append(soft, msg)
		case m == nil:
			msg := fmt.Sprintf("mermaid diagram %d: renderer returned no PNG image", i+1)
			fmt.Fprintln(os.Stderr, "Error:", msg)
			soft = append(soft, msg)
		default:
			replacement = "\n![Mermaid diagram](" + m[1] + ")\n"
		}
		if replacement == "" {
			replacement = "\n```mermaid\n" + d.source + "\n```\n"
		}
		src = strings.Replace(src, pre.placeholders[i], replacement, 1)
	}
	return src, soft
}
