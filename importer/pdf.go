// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Poppler tools used for PDF → page images + text (agent-slurp path).
var (
	PDFToPPMDep = struct {
		Name, Purpose, Install string
	}{
		Name:    "pdftoppm",
		Purpose: "PDF page → PNG for import",
		Install: "brew install poppler",
	}
	PDFToTextDep = struct {
		Name, Purpose, Install string
	}{
		Name:    "pdftotext",
		Purpose: "PDF text extraction for import",
		Install: "brew install poppler",
	}
)

// CheckPDFDeps returns an error if pdftoppm or pdftotext is missing.
func CheckPDFDeps() error {
	for _, d := range []struct{ Name, Purpose, Install string }{
		{PDFToPPMDep.Name, PDFToPPMDep.Purpose, PDFToPPMDep.Install},
		{PDFToTextDep.Name, PDFToTextDep.Purpose, PDFToTextDep.Install},
	} {
		if _, err := exec.LookPath(d.Name); err != nil {
			return fmt.Errorf("required dependency %q not found on PATH (%s).\nInstall: %s",
				d.Name, d.Purpose, d.Install)
		}
	}
	return nil
}

// ImportPDF renders pages to PNG and extracts text into GFM Markdown.
// Images land under mediaDir; Markdown uses absolute image paths.
func ImportPDF(ctx context.Context, pdfPath, mediaDir string) (Result, error) {
	if err := CheckPDFDeps(); err != nil {
		return Result{}, err
	}
	absPDF, err := filepath.Abs(pdfPath)
	if err != nil {
		return Result{}, err
	}
	if _, err := os.Stat(absPDF); err != nil {
		return Result{}, err
	}
	absMedia, err := filepath.Abs(mediaDir)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(absMedia, 0o755); err != nil {
		return Result{}, err
	}

	// pdftoppm -png -r 120 absPDF absMedia/page  → page-1.png, …
	prefix := filepath.Join(absMedia, "page")
	cmd := exec.CommandContext(ctx, PDFToPPMDep.Name, "-png", "-r", "120", absPDF, prefix)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return Result{}, fmt.Errorf("pdftoppm: %w: %s", err, msg)
		}
		return Result{}, fmt.Errorf("pdftoppm: %w", err)
	}

	textCmd := exec.CommandContext(ctx, PDFToTextDep.Name, "-layout", absPDF, "-")
	var textOut, textErr bytes.Buffer
	textCmd.Stdout = &textOut
	textCmd.Stderr = &textErr
	if err := textCmd.Run(); err != nil {
		msg := strings.TrimSpace(textErr.String())
		if msg != "" {
			return Result{}, fmt.Errorf("pdftotext: %w: %s", err, msg)
		}
		return Result{}, fmt.Errorf("pdftotext: %w", err)
	}

	pages, err := filepath.Glob(prefix + "-*.png")
	if err != nil {
		return Result{}, err
	}
	// Sort by name (page-1, page-2, … page-10 needs numeric — pdftoppm
	// zero-pads inconsistently; use filepath walk + simple sort).
	if len(pages) == 0 {
		// Some versions use page-01.png; also try without hyphen pattern.
		pages, _ = filepath.Glob(filepath.Join(absMedia, "page*.png"))
	}
	sortPagePaths(pages)

	var b strings.Builder
	b.WriteString("# PDF import\n\n")
	b.WriteString(fmt.Sprintf("Source: `%s`\n\n", absPDF))
	if len(pages) > 0 {
		b.WriteString("## Pages\n\n")
		for i, p := range pages {
			ap, _ := filepath.Abs(p)
			fmt.Fprintf(&b, "### Page %d\n\n![page %d](%s)\n\n", i+1, i+1, ap)
		}
	}
	text := strings.TrimSpace(textOut.String())
	if text != "" {
		b.WriteString("## Extracted text\n\n")
		b.WriteString("```\n")
		b.WriteString(text)
		if !strings.HasSuffix(text, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("```\n")
	}

	md := b.String()
	md, assets, err := AbsolutizeMedia(md, absMedia)
	if err != nil {
		return Result{}, err
	}
	if len(assets) == 0 {
		assets = pages
	}
	return Result{Markdown: md, MediaDir: absMedia, Assets: assets}, nil
}

func sortPagePaths(paths []string) {
	// Natural-ish: shorter names first then lexical (page-2 before page-10
	// fails; pdftoppm typically uses page-1.png … — extract number).
	type item struct {
		n int
		p string
	}
	items := make([]item, 0, len(paths))
	for _, p := range paths {
		base := filepath.Base(p)
		n := 0
		for _, r := range base {
			if r >= '0' && r <= '9' {
				n = n*10 + int(r-'0')
			}
		}
		items = append(items, item{n: n, p: p})
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].n < items[i].n || (items[j].n == items[i].n && items[j].p < items[i].p) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	for i := range items {
		paths[i] = items[i].p
	}
}
