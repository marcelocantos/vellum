// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/vellum/internal/fileurl"
	"github.com/marcelocantos/vellum/internal/hint"
)

type weasyprintBackend struct{}

func (weasyprintBackend) Name() string { return BackendWeasyPrint }

func (weasyprintBackend) Dep() Dep {
	return Dep{
		Name:    "weasyprint",
		Purpose: "HTML to PDF typesetting (WeasyPrint — BSD-3)",
		Install: hint.WeasyPrint(),
	}
}

func (weasyprintBackend) Render(ctx context.Context, htmlContent, outputPath, inputDir, pdfaProfile string) error {
	args := []string{weasyprintNoFiles}
	if inputDir != "" {
		// WeasyPrint flag is --base-url (with a hyphen). url.URL handles
		// path encoding for spaces/Unicode.
		args = append(args, "--base-url="+fileurl.FromPath(inputDir+string(filepath.Separator)))
	}
	return weasyprintRun(ctx, htmlContent, outputPath, pdfaProfile, args)
}

// weasyprintDataOnly refuses every URL but data:, wherever it appears:
// the page, its stylesheets, SVG drawn as an image, and anything those
// name. WeasyPrint checks it for each fetch, so a redirect never gets as
// far as a request.
const weasyprintDataOnly = "--allowed-protocols=data"

// weasyprintNoFiles refuses file: wherever it appears (an attachment, an
// image, a stylesheet, a redirect) and leaves http and https: the CLI
// and MCP read a document's local files themselves (WritePDF).
const weasyprintNoFiles = "--allowed-protocols=data,http,https"

func (weasyprintBackend) renderFetchless(ctx context.Context, htmlContent, outputPath, baseDir, pdfaProfile string) error {
	args := []string{weasyprintDataOnly}
	if baseDir != "" {
		args = append(args, "--base-url="+fileurl.FromPath(baseDir+string(filepath.Separator)))
	}
	return weasyprintRun(ctx, htmlContent, outputPath, pdfaProfile, args)
}

func weasyprintRun(ctx context.Context, htmlContent, outputPath, pdfaProfile string, args []string) error {
	if pdfaProfile != "" {
		// WeasyPrint --pdf-variant accepts lowercase forms (e.g. pdf/a-3b).
		args = append(args, "--pdf-variant="+strings.ToLower(pdfaProfile))
	}
	return runEngine(ctx, "weasyprint", htmlContent, outputPath, filterWeasyprintNoise, func(input string) []string {
		return append(args, input, outputPath)
	})
}

// filterWeasyprintNoise drops the wall of cosmetic "Ignored …" warnings
// WeasyPrint emits for SVG presentation attributes and vendor-prefix CSS
// (mostly from KaTeX output). The result keeps only real errors and stays
// readable when bubbled into a tool's error string.
func filterWeasyprintNoise(stderr string) string {
	var kept []string
	for line := range strings.SplitSeq(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Drop lines like "WARNING: Ignored `width:min-content` at 1:5197, invalid value."
		if strings.HasPrefix(line, "WARNING: Ignored") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
