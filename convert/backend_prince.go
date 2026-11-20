// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/vellum/internal/fileurl"
)

type princeBackend struct{}

func (princeBackend) Name() string { return BackendPrince }

func (princeBackend) Dep() Dep {
	return Dep{
		Name:    "prince",
		Purpose: "HTML to PDF typesetting (Prince — proprietary; free with watermark for non-commercial use)",
		Install: "https://www.princexml.com/download/",
	}
}

func (princeBackend) Render(ctx context.Context, htmlContent, outputPath, inputDir, pdfaProfile string) error {
	// Relative paths in <img src="..."> resolve against the source Markdown
	// file's directory via --baseurl. url.URL ensures unusual paths (spaces,
	// Unicode) are correctly encoded.
	var args []string
	if inputDir != "" {
		args = append(args, "--baseurl="+fileurl.FromPath(inputDir+string(filepath.Separator)))
	}
	return princeRun(ctx, htmlContent, outputPath, pdfaProfile, args)
}

// princeFetchless loads nothing beyond the input file: no HTTP, no local
// file but the one named on the command line. XInclude and XML external
// entities stay off (Prince's default; vellum never turns them on).
var princeFetchless = []string{"--no-network", "--no-local-files"}

func (princeBackend) renderFetchless(ctx context.Context, htmlContent, outputPath, pdfaProfile string) error {
	return princeRun(ctx, htmlContent, outputPath, pdfaProfile, append([]string(nil), princeFetchless...))
}

func princeRun(ctx context.Context, htmlContent, outputPath, pdfaProfile string, args []string) error {
	if pdfaProfile != "" {
		// Prince accepts the canonical form as-is (e.g. "PDF/A-3b").
		args = append(args, "--pdf-profile="+pdfaProfile)
	}
	return runEngine(ctx, "prince", htmlContent, outputPath, strings.TrimSpace, func(input string) []string {
		return append(args, input, "-o", outputPath)
	})
}
