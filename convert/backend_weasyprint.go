// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type weasyprintBackend struct{}

func (weasyprintBackend) Name() string { return BackendWeasyPrint }

func (weasyprintBackend) Dep() Dep {
	return Dep{
		Name:    "weasyprint",
		Purpose: "HTML to PDF typesetting (WeasyPrint — BSD-3)",
		Install: "brew install weasyprint  (or: pipx install weasyprint)",
	}
}

func (weasyprintBackend) Render(ctx context.Context, htmlContent, outputPath, inputDir, pdfaProfile string) error {
	if dir := filepath.Dir(outputPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

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

	args := []string{}
	if inputDir != "" {
		// WeasyPrint flag is --base-url (with a hyphen). url.URL handles
		// path encoding for spaces/Unicode.
		baseURL := url.URL{Scheme: "file", Path: inputDir + string(filepath.Separator)}
		args = append(args, "--base-url="+baseURL.String())
	}
	if pdfaProfile != "" {
		// WeasyPrint --pdf-variant accepts lowercase forms (e.g. pdf/a-3b).
		args = append(args, "--pdf-variant="+strings.ToLower(pdfaProfile))
	}
	args = append(args, tmpFile.Name(), outputPath)

	cmd := exec.CommandContext(ctx, "weasyprint", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := filterWeasyprintNoise(stderr.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
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
