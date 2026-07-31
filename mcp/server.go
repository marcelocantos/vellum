// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mcp exposes vellum's document conversion pipeline as an MCP
// (Model Context Protocol) server over stdio.
package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/marcelocantos/vellum/config"
	"github.com/marcelocantos/vellum/convert"
)

// Endpoint is one side of a conversion (source or sink) in the MCP schema.
type Endpoint struct {
	Media   string   `json:"media" jsonschema:"transport medium: file | content | clipboard | file_reference"`
	Path    string   `json:"path,omitempty" jsonschema:"absolute path (file media; or write path for file_reference)"`
	Paths   []string `json:"paths,omitempty" jsonschema:"multiple absolute paths (file media batch only)"`
	Content string   `json:"content,omitempty" jsonschema:"raw text (content media only)"`
	Format  string   `json:"format,omitempty" jsonschema:"optional document format override (markdown, html, rtf, pdf, docx, …)"`
}

// FilePair is sugar for Markdown → PDF file batches (legacy convert shape).
type FilePair struct {
	Input  string `json:"input" jsonschema:"absolute path to a .md file"`
	Output string `json:"output,omitempty" jsonschema:"output PDF path (defaults to input with .pdf extension)"`
}

// ConvertInput is the input schema for the unified convert tool.
type ConvertInput struct {
	From    *Endpoint      `json:"from,omitempty" jsonschema:"source medium descriptor (required unless files is set)"`
	To      *Endpoint      `json:"to,omitempty" jsonschema:"sink medium descriptor (required unless files is set)"`
	Files   []FilePair     `json:"files,omitempty" jsonschema:"legacy sugar: batch Markdown file → PDF file pairs"`
	Style   *convert.Style `json:"style,omitempty" jsonschema:"per-call style overrides; each field overlays the corresponding config-file value"`
	Backend string         `json:"backend,omitempty" jsonschema:"renderer backend for this call: \"weasyprint\" (default) or \"prince\"; empty falls through to the config file"`
}

// ConvertOutput is the structured output schema for convert.
type ConvertOutput struct {
	FromMedia  string   `json:"from_media,omitempty" jsonschema:"resolved source media"`
	ToMedia    string   `json:"to_media,omitempty" jsonschema:"resolved sink media"`
	FromFormat string   `json:"from_format,omitempty" jsonschema:"detected or forced source format"`
	ToFormat   string   `json:"to_format,omitempty" jsonschema:"detected or forced sink format"`
	Paths      []string `json:"paths,omitempty" jsonschema:"written file paths (file / file_reference sinks)"`
	Content    string   `json:"content,omitempty" jsonschema:"inline result text (content sink)"`
	Errors     []string `json:"errors,omitempty" jsonschema:"per-item errors for batch conversions"`
}

// Serve runs a vellum MCP server on stdio until the client disconnects.
// version is reported in the Implementation info sent to the client.
func Serve(ctx context.Context, version string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	baseStyle := cfg.Style
	baseBackend := cfg.Backend

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "vellum",
		Version: version,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:  "convert",
		Title: "Convert between file, content, clipboard, and file references",
		Description: strings.TrimSpace(`
Convert documents across media: file, content (inline text), clipboard (rich text), and file_reference (Finder-style pasteboard file URLs).

Provide from + to (each with media and media-specific fields), or files for legacy Markdown→PDF batch sugar.

Examples:
  { "from": { "media": "content", "content": "# Hi" }, "to": { "media": "clipboard" } }
  { "from": { "media": "clipboard" }, "to": { "media": "content" } }
  { "from": { "media": "file", "path": "/abs/in.md" }, "to": { "media": "file", "path": "/abs/out.pdf" } }
  { "from": { "media": "file", "path": "/abs/doc.docx" }, "to": { "media": "content" } }
  { "from": { "media": "file", "path": "/abs/in.md" }, "to": { "media": "file_reference", "path": "/abs/out.pdf" } }
  { "files": [{ "input": "/abs/a.md" }, { "input": "/abs/b.md", "output": "/abs/b.pdf" }] }

Formats are inferred aggressively (file extension, clipboard UTI, markdown default for content, pdf for md→file, markdown for rich→file). Optional from.format / to.format override.

Disallowed (intractable): to.media content|clipboard with format pdf. macOS required for clipboard and file_reference. Optional style and backend overlay the user config for this call only.
`),
	}, makeConvertHandler(baseStyle, baseBackend))

	return server.Run(ctx, &mcp.StdioTransport{})
}

func makeConvertHandler(baseStyle *convert.Style, baseBackend string) func(context.Context, *mcp.CallToolRequest, ConvertInput) (*mcp.CallToolResult, ConvertOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input ConvertInput) (*mcp.CallToolResult, ConvertOutput, error) {
		backend := input.Backend
		if backend == "" {
			backend = baseBackend
		}
		req := &convert.Request{
			Style:   input.Style.OverlayOn(baseStyle),
			Backend: backend,
		}

		if len(input.Files) > 0 {
			for _, f := range input.Files {
				req.Files = append(req.Files, convert.FilePair{Input: f.Input, Output: f.Output})
			}
		} else {
			if input.From == nil || input.To == nil {
				return errorResult(fmt.Errorf("convert: provide from+to, or files for Markdown→PDF batch sugar")), ConvertOutput{}, nil
			}
			req.From = toEndpoint(input.From)
			req.To = toEndpoint(input.To)
		}

		res, err := convert.Run(ctx, req)
		if err != nil {
			out := ConvertOutput{}
			if res != nil {
				out = fromResult(res)
			}
			return errorResult(err), out, nil
		}
		out := fromResult(res)
		msg := formatMessage(out)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: msg}},
			IsError: len(out.Paths) == 0 && out.Content == "" && len(out.Errors) > 0,
		}, out, nil
	}
}

func toEndpoint(e *Endpoint) convert.Endpoint {
	if e == nil {
		return convert.Endpoint{}
	}
	return convert.Endpoint{
		Media:   convert.Media(e.Media),
		Path:    e.Path,
		Paths:   e.Paths,
		Content: e.Content,
		Format:  e.Format,
	}
}

func fromResult(r *convert.Result) ConvertOutput {
	if r == nil {
		return ConvertOutput{}
	}
	return ConvertOutput{
		FromMedia:  string(r.FromMedia),
		ToMedia:    string(r.ToMedia),
		FromFormat: r.FromFormat,
		ToFormat:   r.ToFormat,
		Paths:      r.Paths,
		Content:    r.Content,
		Errors:     r.Errors,
	}
}

func formatMessage(out ConvertOutput) string {
	if out.Content != "" {
		return out.Content
	}
	var b strings.Builder
	if len(out.Paths) > 0 {
		fmt.Fprintf(&b, "Converted %d path(s) (%s → %s):\n", len(out.Paths), out.FromMedia, out.ToMedia)
		for _, p := range out.Paths {
			fmt.Fprintf(&b, "  %s\n", p)
		}
	} else if out.ToMedia == "clipboard" || out.ToMedia == string(convert.MediaClipboard) {
		fmt.Fprintf(&b, "Copied to clipboard (%s → %s).\n", out.FromMedia, out.ToMedia)
	} else if out.ToMedia == string(convert.MediaFileReference) {
		fmt.Fprintf(&b, "Placed file reference on clipboard.\n")
	}
	if len(out.Errors) > 0 {
		b.WriteString("Errors:\n")
		for _, e := range out.Errors {
			fmt.Fprintf(&b, "  %s\n", e)
		}
	}
	s := strings.TrimRight(b.String(), "\n")
	if s == "" {
		return "OK"
	}
	return s
}

func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}
