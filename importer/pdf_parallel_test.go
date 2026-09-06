// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/perffixture"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

// Fan-out across pdftoppm processes must be invisible in the result:
// every page present exactly once, in order, with the text intact.
func TestImportPDF_ParallelPagesCompleteAndOrdered(t *testing.T) {
	testdeps.Need(t, PDFToPPMDep.Name, PDFToTextDep.Name)
	t.Setenv("VELLUM_IMPORT_CACHE", t.TempDir())
	const pages = 37 // not a multiple of any plausible worker count
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.pdf")
	if err := os.WriteFile(path, perffixture.PDF(pages), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ImportFile(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Assets) != pages {
		t.Fatalf("got %d page assets, want %d: %v", len(res.Assets), pages, res.Assets)
	}
	seen := map[string]bool{}
	for i, a := range res.Assets {
		if seen[a] {
			t.Fatalf("asset %s listed twice", a)
		}
		seen[a] = true
		if _, err := os.Stat(a); err != nil {
			t.Fatalf("asset %d missing: %v", i, err)
		}
	}
	// Headings enumerate pages in order and reference the asset for
	// that page; page N's image must follow "### Page N".
	for i, a := range res.Assets {
		heading := fmt.Sprintf("### Page %d\n\n![page %d](%s)", i+1, i+1, a)
		if !strings.Contains(res.Markdown, heading) {
			t.Fatalf("page %d not bound to %s in Markdown", i+1, a)
		}
	}
	if !strings.Contains(res.Markdown, fmt.Sprintf("page %d of %d", pages, pages)) {
		t.Fatalf("extracted text lacks the last page")
	}
}

func TestPageCount(t *testing.T) {
	if got := pageCount("one\ftwo\fthree\f"); got != 3 {
		t.Fatalf("pageCount = %d, want 3", got)
	}
	if got := pageCount(""); got != 0 {
		t.Fatalf("pageCount(empty) = %d, want 0", got)
	}
}
