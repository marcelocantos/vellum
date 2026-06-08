// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mcp exposes vellum's document conversion pipeline as an MCP
// (Model Context Protocol) server over stdio.
package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/marcelocantos/vellum/clipboard"
	"github.com/marcelocantos/vellum/config"
	"github.com/marcelocantos/vellum/convert"
	"github.com/marcelocantos/vellum/importer"
)

// ConvertFile describes a single file to convert.
type ConvertFile struct {
	Input  string `json:"input" jsonschema:"absolute path to a .md file"`
	Output string `json:"output,omitempty" jsonschema:"output PDF path (defaults to input with .pdf extension)"`
}

// ConvertInput is the input schema for the convert tool.
type ConvertInput struct {
	Files   []ConvertFile  `json:"files" jsonschema:"files to convert (at least one)"`
	Style   *convert.Style `json:"style,omitempty" jsonschema:"per-call style overrides; each field overlays the corresponding config-file value"`
	Backend string         `json:"backend,omitempty" jsonschema:"renderer backend for this call: \"weasyprint\" (default, BSD-3) or \"prince\" (proprietary, opt-in); empty falls through to the config file or built-in default"`
}

// ConvertOutput is the structured output schema for the convert tool.
type ConvertOutput struct {
	Converted []string `json:"converted,omitempty" jsonschema:"output PDF paths that were written successfully"`
	Errors    []string `json:"errors,omitempty" jsonschema:"errors encountered, one per failed file"`
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
		Name:        "convert",
		Title:       "Convert Markdown to PDF",
		Description: "Convert one or more Markdown files to PDF. Input paths must be absolute. Each file is rendered via goldmark (GFM + extensions), with server-side KaTeX math and Mermaid diagrams, then typeset by the selected backend (WeasyPrint by default, Prince opt-in). Returns the list of written PDFs and any errors. Optional 'style' and 'backend' fields override the user's config file for this call only.",
	}, makeConvertHandler(baseStyle, baseBackend))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "convert_to_clipboard",
		Title:       "Convert Markdown to clipboard",
		Description: "Render a single Markdown file and place RTF + HTML + plain-text representations on the system clipboard in a single atomic transaction. Designed for handing formatted content to rich-text composers (Slack, Mail, …) without the textutil+osascript dance. Returns when the underlying pasteboard has committed the data — there is no race window where a subsequent paste sees stale content. macOS only currently; other platforms return an unsupported error. Optional 'style' and 'backend' fields override the user's config file for this call only.",
	}, makeClipboardHandler(baseStyle, baseBackend))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "convert_from_clipboard",
		Title:       "Convert clipboard rich text to Markdown",
		Description: "Read the system clipboard's rich-text representation (RTF preferred, HTML fallback) and convert it to GitHub-Flavoured Markdown. Designed for ingesting content copied from rich-text apps (Word, Pages, Mail, Slack composer, browsers) into a Markdown-native working set. Returns the Markdown text directly — no file is written. macOS only currently; other platforms return an unsupported error. Requires pandoc on PATH.",
	}, convertFromClipboardHandler)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "import",
		Title:       "Import a rich-text file to Markdown",
		Description: "Read a rich-text file (RTF, DOCX, HTML, ODT, EPUB, LaTeX, and any other format pandoc accepts) and convert it to GitHub-Flavoured Markdown. Format is auto-detected from the file extension; pass an explicit 'format' field to override. If 'output' is supplied, the Markdown is written to that path; otherwise it's returned in the response. Requires pandoc on PATH.",
	}, importHandler)

	return server.Run(ctx, &mcp.StdioTransport{})
}

func makeConvertHandler(baseStyle *convert.Style, baseBackend string) func(context.Context, *mcp.CallToolRequest, ConvertInput) (*mcp.CallToolResult, ConvertOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input ConvertInput) (*mcp.CallToolResult, ConvertOutput, error) {
		if len(input.Files) == 0 {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "no files specified"}},
			}, ConvertOutput{}, nil
		}

		backend := input.Backend
		if backend == "" {
			backend = baseBackend
		}
		opts := &convert.Options{Style: input.Style.OverlayOn(baseStyle), Backend: backend}

		var out ConvertOutput
		for _, f := range input.Files {
			inputPath, err := resolveInput(f.Input)
			if err != nil {
				out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", f.Input, err))
				continue
			}

			outputPath, err := resolveOutput(inputPath, f.Output)
			if err != nil {
				out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", f.Input, err))
				continue
			}

			if _, err := os.Stat(inputPath); err != nil {
				out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", f.Input, err))
				continue
			}

			if err := convert.Convert(ctx, inputPath, outputPath, opts); err != nil {
				out.Errors = append(out.Errors, fmt.Sprintf("%s: %v", f.Input, err))
				continue
			}

			out.Converted = append(out.Converted, outputPath)
		}

		result := &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: formatSummary(out)}},
			IsError: len(out.Converted) == 0 && len(out.Errors) > 0,
		}
		return result, out, nil
	}
}

// ClipboardInput is the input schema for the convert_to_clipboard tool.
type ClipboardInput struct {
	Input   string         `json:"input" jsonschema:"absolute path to a .md file"`
	Style   *convert.Style `json:"style,omitempty" jsonschema:"per-call style overrides; each field overlays the corresponding config-file value"`
	Backend string         `json:"backend,omitempty" jsonschema:"renderer backend for this call: \"weasyprint\" (default) or \"prince\"; empty falls through to the config file"`
}

// ClipboardOutput is the structured output schema for convert_to_clipboard.
type ClipboardOutput struct {
	Input string `json:"input" jsonschema:"the input path that was rendered"`
}

func makeClipboardHandler(baseStyle *convert.Style, baseBackend string) func(context.Context, *mcp.CallToolRequest, ClipboardInput) (*mcp.CallToolResult, ClipboardOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ClipboardInput) (*mcp.CallToolResult, ClipboardOutput, error) {
		out := ClipboardOutput{Input: in.Input}

		inputPath, err := resolveInput(in.Input)
		if err != nil {
			return errorResult(err), out, nil
		}
		if _, err := os.Stat(inputPath); err != nil {
			return errorResult(err), out, nil
		}

		backend := in.Backend
		if backend == "" {
			backend = baseBackend
		}
		opts := &convert.Options{Style: in.Style.OverlayOn(baseStyle), Backend: backend}
		html, err := convert.RenderFile(ctx, inputPath, opts)
		if err != nil {
			return errorResult(err), out, nil
		}
		if err := clipboard.Write(clipboard.Payload{HTML: html}); err != nil {
			return errorResult(err), out, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Copied %s to clipboard (RTF + HTML + plain text).", inputPath)}},
		}, out, nil
	}
}

// ImportInput is the input schema for the `import` tool.
type ImportInput struct {
	Input  string `json:"input" jsonschema:"absolute path to the rich-text file"`
	Output string `json:"output,omitempty" jsonschema:"optional absolute path to write the Markdown to; when omitted, the Markdown is returned in the response"`
	Format string `json:"format,omitempty" jsonschema:"input format override (e.g., rtf, docx, html, odt, epub, latex); defaults to pandoc's auto-detection from file extension"`
}

// ImportOutput is the structured output schema for the `import` tool.
type ImportOutput struct {
	Markdown string `json:"markdown,omitempty" jsonschema:"the Markdown text (present when output was not supplied)"`
	Output   string `json:"output,omitempty" jsonschema:"the path the Markdown was written to (present when output was supplied)"`
}

// ClipboardImportOutput is the structured output schema for the
// `convert_from_clipboard` tool.
type ClipboardImportOutput struct {
	Markdown string `json:"markdown" jsonschema:"the Markdown text extracted from the clipboard"`
	Format   string `json:"format,omitempty" jsonschema:"the clipboard format that was read (\"rtf\" or \"html\")"`
}

func importHandler(ctx context.Context, _ *mcp.CallToolRequest, in ImportInput) (*mcp.CallToolResult, ImportOutput, error) {
	var out ImportOutput

	inputPath, err := resolveInput(in.Input)
	if err != nil {
		return errorResult(err), out, nil
	}
	if _, err := os.Stat(inputPath); err != nil {
		return errorResult(err), out, nil
	}
	if err := importer.CheckDep(); err != nil {
		return errorResult(err), out, nil
	}

	md, err := importer.ImportFile(ctx, inputPath, in.Format)
	if err != nil {
		return errorResult(err), out, nil
	}

	if in.Output != "" {
		outputPath, err := filepath.Abs(in.Output)
		if err != nil {
			return errorResult(err), out, nil
		}
		if dir := filepath.Dir(outputPath); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return errorResult(err), out, nil
			}
		}
		if err := os.WriteFile(outputPath, []byte(md), 0o644); err != nil {
			return errorResult(err), out, nil
		}
		out.Output = outputPath
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Imported %s → %s", inputPath, outputPath)}},
		}, out, nil
	}

	out.Markdown = md
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: md}},
	}, out, nil
}

func convertFromClipboardHandler(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ClipboardImportOutput, error) {
	var out ClipboardImportOutput

	if err := importer.CheckDep(); err != nil {
		return errorResult(err), out, nil
	}
	data, format, err := clipboard.ReadRichText()
	if err != nil {
		return errorResult(err), out, nil
	}
	if len(data) == 0 {
		return errorResult(fmt.Errorf("clipboard: no RTF or HTML content found")), out, nil
	}

	md, err := importer.ImportBytes(ctx, data, format)
	if err != nil {
		return errorResult(err), out, nil
	}
	out.Markdown = md
	out.Format = format
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: md}},
	}, out, nil
}

func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

// resolveInput returns an absolute path for an input file. MCP clients are
// expected to pass absolute paths, but we accept relative paths by resolving
// against the server process's working directory as a convenience.
func resolveInput(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("input path is empty")
	}
	return filepath.Abs(p)
}

// resolveOutput picks the output path: either the caller-provided value
// (resolved to absolute) or the input path with its extension replaced by .pdf.
func resolveOutput(inputPath, requested string) (string, error) {
	if requested == "" {
		return strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + ".pdf", nil
	}
	return filepath.Abs(requested)
}

func formatSummary(out ConvertOutput) string {
	var b strings.Builder
	if len(out.Converted) > 0 {
		fmt.Fprintf(&b, "Converted %d file(s):\n", len(out.Converted))
		for _, p := range out.Converted {
			fmt.Fprintf(&b, "  %s\n", p)
		}
	}
	if len(out.Errors) > 0 {
		b.WriteString("Errors:\n")
		for _, e := range out.Errors {
			fmt.Fprintf(&b, "  %s\n", e)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
