// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
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

	// Text first: it is cheap (20 ms for 40 pages, measured 2026-09-06)
	// and its output tells us the page count, which lets the expensive
	// rasterisation fan out across CPUs.
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

	prefix := filepath.Join(absMedia, "page")
	if err := renderPages(ctx, absPDF, prefix, pageCount(textOut.String())); err != nil {
		return Result{}, err
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

// pdftoppm ends every page with a form feed, so the count of form feeds
// in pdftotext output is the page count. Zero means the count is
// unknown and the whole document is rendered in one process.
func pageCount(text string) int { return strings.Count(text, "\f") }

// pageRenderDPI is the pdftoppm resolution for page images: enough for
// an agent to read the page, small enough to keep the cache bounded.
const pageRenderDPI = "120"

// renderPages rasterises every page of absPDF to prefix-N.png.
//
// pdftoppm renders pages serially in one process, and that process
// was the entire cost of a PDF import (1.07 s of 1.09 s for the
// 40-page benchmark fixture, measured 2026-09-06). Pages are
// independent, so the document is split into contiguous ranges and
// one pdftoppm runs per range, bounded by CPU count. pdftoppm pads
// page numbers from the document's total page count regardless of
// the range, so the file names are identical to a single run.
func renderPages(ctx context.Context, absPDF, prefix string, pages int) error {
	workers := runtime.NumCPU()
	if pages < workers {
		workers = pages
	}
	if workers <= 1 {
		return runPDFToPPM(ctx, absPDF, prefix, 0, 0)
	}
	perWorker := (pages + workers - 1) / workers
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		first := w*perWorker + 1
		last := min(first+perWorker-1, pages)
		if first > last {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[w] = runPDFToPPM(ctx, absPDF, prefix, first, last)
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// runPDFToPPM renders pages first..last (1-based, inclusive); zero
// bounds render the whole document.
func runPDFToPPM(ctx context.Context, absPDF, prefix string, first, last int) error {
	args := []string{"-png", "-r", pageRenderDPI}
	if first > 0 {
		args = append(args, "-f", strconv.Itoa(first), "-l", strconv.Itoa(last))
	}
	args = append(args, absPDF, prefix)
	cmd := exec.CommandContext(ctx, PDFToPPMDep.Name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("pdftoppm: %w: %s", err, msg)
		}
		return fmt.Errorf("pdftoppm: %w", err)
	}
	return nil
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
