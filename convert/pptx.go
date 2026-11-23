// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/marcelocantos/vellum/internal/pandoc"
	"github.com/marcelocantos/vellum/internal/remoteimage"
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

// TemplateDefault selects the installed pandoc's default reference deck.
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
// $XDG_CONFIG_HOME/vellum/templates or ~/.config/vellum/templates
// (%APPDATA%\vellum\templates on Windows).
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
//   - "" or TemplateDefault: pandoc's default, represented by an empty path;
//   - a path (contains a separator, or ends in .pptx/.potx): used as is,
//     and must exist;
//   - a bare name: <TemplatesDir>/<name>.pptx, then .potx.
func resolveTemplate(selector string) (path string, cleanup func(), err error) {
	noop := func() {}
	selector = strings.TrimSpace(selector)
	if selector == "" || selector == TemplateDefault {
		return "", noop, nil
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
		"convert: pptx template %q not found: expected %s (or pass a path to a .pptx file; %q is pandoc's default deck)",
		selector, filepath.Join(dir, selector+templateExts[0]), TemplateDefault)
}

// writePPTX renders Markdown to a PowerPoint deck at outPath. Mermaid
// blocks are rendered to PNG through the shared fragment renderer and
// handed to pandoc as data-URI images; a diagram that fails to render
// stays on its slide as a code block and is reported in soft, matching
// the HTML and PDF sinks. pandoc warnings (dropped constructs, missing
// resources it tolerated) are also soft errors.
//
// pandoc embeds whatever an image names, byte for byte, so it reads
// nothing here: it parses the Markdown, every image of that parse is
// inlined as a data: URL by inlineDeckImages (an image file inside
// files, or a remote image vellum fetched), and the deck is written from
// the result, both runs under --sandbox.
func writePPTX(ctx context.Context, md, outPath, baseDir string, files docFiles, opts *Options) (soft []string, err error) {
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
	if refDoc == "" {
		// pandoc under --sandbox no longer finds its user default deck
		// by itself; hand it over, so an omitted template is still the
		// installed pandoc's choice.
		if refDoc, err = pandoc.UserDataFile(ctx, pandocUserReferencePPTX); err != nil {
			return soft, err
		}
	}

	ast, err := pandoc.ToJSON(ctx, src, pptxReader)
	if err != nil {
		return soft, err
	}
	ast, dropped, err := inlineDeckImages(ctx, ast, baseDir, files)
	if err != nil {
		return soft, err
	}
	soft = append(soft, dropped...)

	var args []string
	if refDoc != "" {
		args = append(args, "--reference-doc="+refDoc)
	}
	warnings, err := pandoc.ToFile(ctx, string(ast), pandocJSON, FormatPPTX, outPath, args...)
	if err != nil {
		return soft, err
	}
	return append(soft, warnings...), nil
}

// pandocJSON is pandoc's name for its own AST as JSON.
const pandocJSON = "json"

// pandocUserReferencePPTX is the default reference deck pandoc reads
// from its user data directory.
const pandocUserReferencePPTX = "reference.pptx"

// inlineDeckImages rewrites every Image in a pandoc JSON AST to a data:
// URL: a relative name, absolute path or file: URL only when it is an
// image file inside files, an http or https URL fetched by vellum
// (remoteimage.Fetch, which says it is an image). Any other image
// becomes its description, and is reported in soft. Because the images
// are pandoc's own parse, every spelling Markdown has for one (inline,
// reference, angle brackets, escapes) is covered.
func inlineDeckImages(ctx context.Context, ast []byte, baseDir string, files docFiles) ([]byte, []string, error) {
	dec := json.NewDecoder(bytes.NewReader(ast))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, nil, fmt.Errorf("pandoc AST: %w", err)
	}
	fetchCtx, cancel := context.WithTimeout(ctx, remoteimage.Budget)
	defer cancel()
	in := &inliner{
		ctx:  fetchCtx,
		base: baseDir,
		args: &InlineArgs{Read: files.readImage, Fetch: remoteimage.Fetch},
		seen: map[string]refDecision{},
	}
	var soft []string
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
		case map[string]any:
			// Image: {"t":"Image","c":[attr, [alt inlines], [url, title]]}
			if x["t"] == "Image" {
				c, _ := x["c"].([]any)
				var target []any
				if len(c) == 3 {
					target, _ = c[2].([]any)
				}
				var ref string
				ok := len(target) == 2
				if ok {
					ref, ok = target[0].(string)
				}
				if !ok {
					return map[string]any{"t": "Str", "c": ""}
				}
				d := in.decide(ref)
				switch {
				case d.kind == refReplace:
					target[0] = d.repl
				case d.kind == refKeep && strings.HasPrefix(strings.ToLower(strings.TrimSpace(ref)), "data:"):
				default:
					soft = append(soft, fmt.Sprintf(
						"pptx: image %s left out: only image files inside the document's folder, and http(s) images, are embedded", ref))
					return map[string]any{"t": "Span", "c": []any{c[0], walk(c[1])}}
				}
			}
			for k, e := range x {
				x[k] = walk(e)
			}
		}
		return v
	}
	doc = walk(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, nil, err
	}
	return out, soft, nil
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
