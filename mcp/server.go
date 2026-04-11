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

	"github.com/marcelocantos/vellum/convert"
)

// ConvertFile describes a single file to convert.
type ConvertFile struct {
	Input  string `json:"input" jsonschema:"absolute path to a .md file"`
	Output string `json:"output,omitempty" jsonschema:"output PDF path (defaults to input with .pdf extension)"`
}

// ConvertInput is the input schema for the convert tool.
type ConvertInput struct {
	Files []ConvertFile `json:"files" jsonschema:"files to convert (at least one)"`
}

// ConvertOutput is the structured output schema for the convert tool.
type ConvertOutput struct {
	Converted []string `json:"converted,omitempty" jsonschema:"output PDF paths that were written successfully"`
	Errors    []string `json:"errors,omitempty" jsonschema:"errors encountered, one per failed file"`
}

// Serve runs a vellum MCP server on stdio until the client disconnects.
// version is reported in the Implementation info sent to the client.
func Serve(ctx context.Context, version string) error {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "vellum",
		Version: version,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "convert",
		Title:       "Convert Markdown to PDF",
		Description: "Convert one or more Markdown files to PDF. Input paths must be absolute. Each file is rendered via goldmark (GFM + extensions), with server-side KaTeX math and Mermaid diagrams, then typeset by Prince. Returns the list of written PDFs and any errors.",
	}, convertHandler)

	return server.Run(ctx, &mcp.StdioTransport{})
}

func convertHandler(ctx context.Context, _ *mcp.CallToolRequest, input ConvertInput) (*mcp.CallToolResult, ConvertOutput, error) {
	if len(input.Files) == 0 {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "no files specified"}},
		}, ConvertOutput{}, nil
	}

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

		if err := convert.Convert(ctx, inputPath, outputPath, nil); err != nil {
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
