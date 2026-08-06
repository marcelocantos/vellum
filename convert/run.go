// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/marcelocantos/vellum/clipboard"
	"github.com/marcelocantos/vellum/importer"
)

// Media is a transport medium for conversion source or sink.
type Media string

const (
	MediaFile          Media = "file"
	MediaContent       Media = "content"
	MediaClipboard     Media = "clipboard"
	MediaFileReference Media = "file_reference"
)

// Canonical document format names used by [Run] inference and validation.
const (
	FormatMarkdown = "markdown"
	FormatHTML     = "html"
	FormatRTF      = "rtf"
	FormatPDF      = "pdf"
	// FormatRich is the multi-representation clipboard sink (RTF+HTML+plain).
	FormatRich = "rich"
)

// Endpoint is one side of a conversion (source or sink).
type Endpoint struct {
	Media   Media
	Path    string   // single path (file media, or file_reference write target)
	Paths   []string // multi-file batch (file media only)
	Content string   // raw text (content media only)
	Format  string   // optional document format override
}

// FilePair is convenience sugar for Markdown → PDF file batches
// (the original MCP convert shape).
type FilePair struct {
	Input  string
	Output string
}

// Request is the unified conversion request.
type Request struct {
	From    Endpoint
	To      Endpoint
	Style   *Style
	Backend string
	// Files, when non-empty, is sugar for a batch of file→file PDF
	// conversions. From/To may be zero in that case.
	Files []FilePair
}

// Result is the unified conversion result.
type Result struct {
	FromMedia  Media
	ToMedia    Media
	FromFormat string
	ToFormat   string
	Paths      []string
	Content    string
	Errors     []string
	// MediaDir / Assets hold extracted import media (images, PDF pages)
	// so Markdown file references resolve for agents.
	MediaDir string
	Assets   []string
}

// Run executes a media-orthogonal conversion.
func Run(ctx context.Context, req *Request) (*Result, error) {
	if req == nil {
		return nil, fmt.Errorf("convert: nil request")
	}

	if len(req.Files) > 0 {
		return runFilesSugar(ctx, req)
	}

	from := req.From
	to := req.To

	if err := validateMedia(from.Media, "from"); err != nil {
		return nil, err
	}
	if err := validateMedia(to.Media, "to"); err != nil {
		return nil, err
	}

	// Resolve file_reference sources into concrete paths.
	if from.Media == MediaFileReference {
		paths, err := clipboard.ReadFileRefs()
		if err != nil {
			return nil, err
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("convert: clipboard has no file references")
		}
		from.Media = MediaFile
		if len(paths) == 1 {
			from.Path = paths[0]
		} else {
			from.Paths = paths
		}
	}

	opts := &Options{Style: req.Style, Backend: req.Backend}
	out := &Result{FromMedia: req.From.Media, ToMedia: to.Media}

	inputs, err := collectInputs(from)
	if err != nil {
		return nil, err
	}

	// Multi-input only for file sinks (or file_reference, which writes files).
	if len(inputs) > 1 {
		switch to.Media {
		case MediaFile, MediaFileReference:
		default:
			return nil, fmt.Errorf("convert: multiple inputs require to.media file or file_reference")
		}
		if to.Path != "" {
			return nil, fmt.Errorf("convert: to.path is only valid with a single input")
		}
	}

	var softMsgs []string
	for _, in := range inputs {
		item, err := runOne(ctx, in, from, to, opts)
		if item != nil {
			if out.FromFormat == "" {
				out.FromFormat = item.FromFormat
			}
			if out.ToFormat == "" {
				out.ToFormat = item.ToFormat
			}
			out.Paths = append(out.Paths, item.Paths...)
			if item.Content != "" {
				out.Content = item.Content
			}
			out.Errors = append(out.Errors, item.Errors...)
			if out.MediaDir == "" && item.MediaDir != "" {
				out.MediaDir = item.MediaDir
			}
			out.Assets = append(out.Assets, item.Assets...)
		}
		if err != nil {
			var se *SoftError
			if errors.As(err, &se) {
				// Soft failure: output was produced; keep going / report at end.
				softMsgs = append(softMsgs, se.Messages...)
				continue
			}
			if len(inputs) == 1 {
				if item != nil {
					return out, err
				}
				return nil, err
			}
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", in.label(), err))
			continue
		}
	}

	if len(out.Paths) == 0 && out.Content == "" && len(out.Errors) > 0 && len(softMsgs) == 0 {
		return out, fmt.Errorf("%s", strings.Join(out.Errors, "; "))
	}
	if len(softMsgs) > 0 {
		return out, softError(softMsgs)
	}
	return out, nil
}

type inputItem struct {
	path    string // empty for pure content/clipboard
	content string // set when bytes already in memory (content media)
	name    string
}

func (in inputItem) label() string {
	if in.path != "" {
		return in.path
	}
	if in.name != "" {
		return in.name
	}
	return "content"
}

func collectInputs(from Endpoint) ([]inputItem, error) {
	switch from.Media {
	case MediaFile:
		paths := from.Paths
		if from.Path != "" {
			if len(paths) > 0 {
				return nil, fmt.Errorf("convert: from.path and from.paths are mutually exclusive")
			}
			paths = []string{from.Path}
		}
		if len(paths) == 0 {
			return nil, fmt.Errorf("convert: from.media file requires path or paths")
		}
		var out []inputItem
		for _, p := range paths {
			abs, err := filepath.Abs(p)
			if err != nil {
				return nil, err
			}
			out = append(out, inputItem{path: abs})
		}
		return out, nil

	case MediaContent:
		if from.Content == "" {
			return nil, fmt.Errorf("convert: from.media content requires content")
		}
		return []inputItem{{content: from.Content, name: "content"}}, nil

	case MediaClipboard:
		return []inputItem{{name: "clipboard"}}, nil

	default:
		return nil, fmt.Errorf("convert: unsupported from.media %q", from.Media)
	}
}

func runOne(ctx context.Context, in inputItem, from, to Endpoint, opts *Options) (*Result, error) {
	ing, err := ingest(ctx, in, from)
	if err != nil {
		return nil, err
	}
	md, fromFmt, baseDir := ing.md, ing.fromFmt, ing.baseDir

	toFmt := inferToFormat(to, fromFmt)
	if err := checkDisallowed(to.Media, toFmt); err != nil {
		return nil, err
	}

	res := &Result{
		FromMedia:  from.Media,
		ToMedia:    to.Media,
		FromFormat: fromFmt,
		ToFormat:   toFmt,
		MediaDir:   ing.mediaDir,
		Assets:     ing.assets,
	}

	switch to.Media {
	case MediaContent:
		text, soft, err := materializeText(ctx, md, toFmt, opts)
		if err != nil {
			return nil, err
		}
		res.Content = text
		return withSoft(res, soft, in)

	case MediaClipboard:
		html, soft, err := Render(ctx, []byte(md), opts)
		if err != nil {
			return nil, err
		}
		if err := clipboard.Write(clipboard.Payload{HTML: html}); err != nil {
			return nil, err
		}
		return withSoft(res, soft, in)

	case MediaFile, MediaFileReference:
		outPath, err := resolveOutPath(in, to, toFmt)
		if err != nil {
			return nil, err
		}
		soft, err := writeFileOutput(ctx, md, outPath, toFmt, baseDir, opts)
		if err != nil {
			return nil, err
		}
		res.Paths = []string{outPath}
		if to.Media == MediaFileReference {
			if err := clipboard.WriteFileRefs(clipboard.FileRefPayload{Paths: []string{outPath}}); err != nil {
				return nil, err
			}
		}
		return withSoft(res, soft, in)

	default:
		return nil, fmt.Errorf("convert: unsupported to.media %q", to.Media)
	}
}

// withSoft attaches soft diagnostics to res.Errors (path-prefixed when
// known) and returns a *SoftError when any soft messages exist.
func withSoft(res *Result, soft []string, in inputItem) (*Result, error) {
	if len(soft) == 0 {
		return res, nil
	}
	prefix := ""
	if in.path != "" {
		prefix = in.path + ": "
	}
	var msgs []string
	for _, s := range soft {
		msg := prefix + s
		res.Errors = append(res.Errors, msg)
		msgs = append(msgs, msg)
	}
	return res, softError(msgs)
}

type ingestResult struct {
	md       string
	fromFmt  string
	baseDir  string
	mediaDir string
	assets   []string
}

func ingest(ctx context.Context, in inputItem, from Endpoint) (ingestResult, error) {
	switch from.Media {
	case MediaFile:
		baseDir := filepath.Dir(in.path)
		fromFmt := normalizeFormat(from.Format)
		// inferred records that no caller-supplied format was given, so
		// the markdown fallback below is a guess rather than an
		// instruction and must not be applied to binary content.
		inferred := fromFmt == ""
		if inferred {
			fromFmt = formatFromExt(in.path)
		}
		if fromFmt == "" {
			fromFmt = FormatMarkdown
		}
		if isMarkdownFormat(fromFmt) {
			b, rerr := os.ReadFile(in.path)
			if rerr != nil {
				return ingestResult{}, rerr
			}
			if inferred {
				if err := checkInferredText(b, in.path); err != nil {
					return ingestResult{}, err
				}
			}
			return ingestResult{md: string(b), fromFmt: FormatMarkdown, baseDir: baseDir}, nil
		}
		fmtHint := from.Format
		if isMarkdownFormat(normalizeFormat(fmtHint)) {
			fmtHint = ""
		}
		if fromFmt == FormatPDF {
			fmtHint = "pdf"
		}
		ir, err := importer.ImportFile(ctx, in.path, &importer.Options{Format: fmtHint})
		if err != nil {
			return ingestResult{}, err
		}
		return ingestResult{
			md: ir.Markdown, fromFmt: fromFmt, baseDir: baseDir,
			mediaDir: ir.MediaDir, assets: ir.Assets,
		}, nil

	case MediaContent:
		fromFmt := normalizeFormat(from.Format)
		if fromFmt == "" {
			fromFmt = FormatMarkdown
		}
		if isMarkdownFormat(fromFmt) {
			return ingestResult{md: in.content, fromFmt: FormatMarkdown}, nil
		}
		ir, err := importer.ImportBytes(ctx, []byte(in.content), &importer.Options{Format: fromFmt})
		if err != nil {
			return ingestResult{}, err
		}
		return ingestResult{
			md: ir.Markdown, fromFmt: fromFmt,
			mediaDir: ir.MediaDir, assets: ir.Assets,
		}, nil

	case MediaClipboard:
		data, detected, err := clipboard.ReadImportable()
		if err != nil {
			return ingestResult{}, err
		}
		if len(data) == 0 {
			return ingestResult{}, fmt.Errorf("convert: clipboard has no RTF, HTML, or PDF content")
		}
		fromFmt := normalizeFormat(from.Format)
		if fromFmt == "" {
			fromFmt = detected
		}
		if fromFmt == "" {
			fromFmt = FormatRTF
		}
		ir, err := importer.ImportBytes(ctx, data, &importer.Options{Format: fromFmt})
		if err != nil {
			return ingestResult{}, err
		}
		return ingestResult{
			md: ir.Markdown, fromFmt: fromFmt,
			mediaDir: ir.MediaDir, assets: ir.Assets,
		}, nil

	default:
		return ingestResult{}, fmt.Errorf("convert: unsupported from.media %q", from.Media)
	}
}

func materializeText(ctx context.Context, md, toFmt string, opts *Options) (string, []string, error) {
	switch {
	case isMarkdownFormat(toFmt) || toFmt == "":
		return md, nil, nil
	case toFmt == FormatHTML:
		return Render(ctx, []byte(md), opts)
	default:
		return "", nil, fmt.Errorf("convert: to.media content does not support format %q", toFmt)
	}
}

func writeFileOutput(ctx context.Context, md, outPath, toFmt, baseDir string, opts *Options) (soft []string, err error) {
	if dir := filepath.Dir(outPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	switch {
	case isMarkdownFormat(toFmt):
		return nil, os.WriteFile(outPath, []byte(md), 0o644)
	case toFmt == FormatHTML:
		html, soft, err := Render(ctx, []byte(md), opts)
		if err != nil {
			return soft, err
		}
		return soft, os.WriteFile(outPath, []byte(html), 0o644)
	case toFmt == FormatPDF || toFmt == "":
		// PDF: PNG mermaid (Prince foreignObject labels); HTML sinks use SVG.
		html, soft, err := Render(ctx, []byte(md), withMermaidFormat(opts, MermaidPNG))
		if err != nil {
			return soft, err
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
			return soft, err
		}
		if baseDir == "" {
			baseDir = filepath.Dir(outPath)
		}
		if err := backend.Render(ctx, html, outPath, baseDir, pdfa); err != nil {
			return soft, fmt.Errorf("%s: %w", backend.Name(), err)
		}
		return soft, nil
	default:
		return nil, fmt.Errorf("convert: to.media file does not support format %q", toFmt)
	}
}

func resolveOutPath(in inputItem, to Endpoint, toFmt string) (string, error) {
	if to.Path != "" {
		return filepath.Abs(to.Path)
	}
	if in.path == "" {
		return "", fmt.Errorf("convert: to.media %s requires to.path when source has no path", to.Media)
	}
	ext := "." + toFmt
	switch {
	case isMarkdownFormat(toFmt):
		ext = ".md"
	case toFmt == FormatPDF || toFmt == "":
		ext = ".pdf"
	case toFmt == FormatHTML:
		ext = ".html"
	}
	return strings.TrimSuffix(in.path, filepath.Ext(in.path)) + ext, nil
}

func inferToFormat(to Endpoint, fromFmt string) string {
	if f := normalizeFormat(to.Format); f != "" {
		return f
	}
	switch to.Media {
	case MediaClipboard:
		return FormatRich
	case MediaContent:
		return FormatMarkdown
	case MediaFile, MediaFileReference:
		if to.Path != "" {
			if f := formatFromExt(to.Path); f != "" {
				return f
			}
		}
		// From rich-text import → markdown file; from markdown → pdf.
		if !isMarkdownFormat(fromFmt) && fromFmt != "" && fromFmt != FormatHTML {
			return FormatMarkdown
		}
		if fromFmt == FormatHTML {
			return FormatHTML
		}
		return FormatPDF
	default:
		return FormatMarkdown
	}
}

func checkDisallowed(media Media, format string) error {
	f := normalizeFormat(format)
	if f == FormatPDF {
		switch media {
		case MediaContent, MediaClipboard:
			return fmt.Errorf("convert: to.media %s cannot use format pdf", media)
		}
	}
	return nil
}

func validateMedia(m Media, side string) error {
	switch m {
	case MediaFile, MediaContent, MediaClipboard, MediaFileReference:
		return nil
	case "":
		return fmt.Errorf("convert: %s.media is required", side)
	default:
		return fmt.Errorf("convert: unknown %s.media %q", side, m)
	}
}

func normalizeFormat(f string) string {
	f = strings.ToLower(strings.TrimSpace(f))
	switch f {
	case "md", "gfm", "markdown":
		return FormatMarkdown
	case "htm":
		return FormatHTML
	default:
		return f
	}
}

func isMarkdownFormat(f string) bool {
	return normalizeFormat(f) == FormatMarkdown
}

// checkInferredText rejects binary content that reached the Markdown
// path only because the extension was unrecognised. Without this, a
// .doc, .key, or .jpg is read verbatim and handed back as "Markdown"
// with a nil error (🎯T20). An explicit from.format still bypasses the
// check, so callers who really mean "treat these bytes as Markdown"
// keep that option.
func checkInferredText(b []byte, path string) error {
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		ext := strings.ToLower(filepath.Ext(path))
		if ext == "" {
			ext = "(none)"
		}
		return fmt.Errorf(
			"convert: %s is binary and its extension %s is not a recognised document format; "+
				"pass from.format explicitly if it really is Markdown", path, ext)
	}
	return nil
}

func formatFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown":
		return FormatMarkdown
	case ".html", ".htm":
		return FormatHTML
	case ".rtf":
		return FormatRTF
	case ".pdf":
		return FormatPDF
	case ".docx":
		return "docx"
	case ".odt":
		return "odt"
	case ".epub":
		return "epub"
	case ".tex", ".latex":
		return "latex"
	default:
		return ""
	}
}

func runFilesSugar(ctx context.Context, req *Request) (*Result, error) {
	out := &Result{
		FromMedia:  MediaFile,
		ToMedia:    MediaFile,
		FromFormat: FormatMarkdown,
		ToFormat:   FormatPDF,
	}
	var softMsgs []string
	for _, f := range req.Files {
		if f.Input == "" {
			out.Errors = append(out.Errors, "files entry missing input")
			continue
		}
		r, err := Run(ctx, &Request{
			From:    Endpoint{Media: MediaFile, Path: f.Input},
			To:      Endpoint{Media: MediaFile, Path: f.Output, Format: FormatPDF},
			Style:   req.Style,
			Backend: req.Backend,
		})
		if r != nil {
			out.Paths = append(out.Paths, r.Paths...)
			out.Errors = append(out.Errors, r.Errors...)
		}
		if err != nil {
			var se *SoftError
			if errors.As(err, &se) {
				softMsgs = append(softMsgs, se.Messages...)
				continue
			}
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", f.Input, err))
			continue
		}
	}
	if len(out.Paths) == 0 && len(out.Errors) > 0 && len(softMsgs) == 0 {
		return out, fmt.Errorf("%s", strings.Join(out.Errors, "; "))
	}
	if len(softMsgs) > 0 {
		return out, softError(softMsgs)
	}
	return out, nil
}
