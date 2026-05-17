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

	// Relative paths in <img src="..."> resolve against the source Markdown
	// file's directory via --baseurl. url.URL ensures unusual paths (spaces,
	// Unicode) are correctly encoded.
	args := []string{}
	if inputDir != "" {
		baseURL := url.URL{Scheme: "file", Path: inputDir + string(filepath.Separator)}
		args = append(args, "--baseurl="+baseURL.String())
	}
	if pdfaProfile != "" {
		// Prince accepts the canonical form as-is (e.g. "PDF/A-3b").
		args = append(args, "--pdf-profile="+pdfaProfile)
	}
	args = append(args, tmpFile.Name(), "-o", outputPath)

	cmd := exec.CommandContext(ctx, "prince", args...)
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
