// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/perffixture"
)

// Import hot paths: PDF page rendering through Poppler, the media
// rewrite over a large Markdown result, and the cache bookkeeping that
// runs on every import. Fixtures are generated into a tempdir.

const (
	benchPDFPages    = 40
	benchCacheBundle = 200 // populated bundles in the import cache
	benchCacheFiles  = 20  // files per bundle
	benchImageRefs   = 2000
)

func haveTool(b *testing.B, tools ...string) {
	b.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			b.Skipf("%s not on PATH", tool)
		}
	}
}

func BenchmarkImportPDF(b *testing.B) {
	haveTool(b, PDFToPPMDep.Name, PDFToTextDep.Name)
	b.Setenv("VELLUM_IMPORT_CACHE", b.TempDir())
	dir := b.TempDir()
	path := filepath.Join(dir, "fixture.pdf")
	if err := os.WriteFile(path, perffixture.PDF(benchPDFPages), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ImportFile(context.Background(), path, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// populateCache fills root with bundles so prune has realistic work.
func populateCache(tb testing.TB, root string) {
	tb.Helper()
	payload := perffixture.PNG(64, 64)
	for i := 0; i < benchCacheBundle; i++ {
		dir := filepath.Join(root, fmt.Sprintf("%064x", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			tb.Fatal(err)
		}
		for f := 0; f < benchCacheFiles; f++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("page-%d.png", f)), payload, 0o644); err != nil {
				tb.Fatal(err)
			}
		}
	}
}

// BenchmarkBundleDir is the per-import cache cost with a populated
// cache: allocate a bundle, then prune by age and size.
func BenchmarkBundleDir(b *testing.B) {
	root := b.TempDir()
	b.Setenv("VELLUM_IMPORT_CACHE", root)
	populateCache(b, root)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := BundleDir(fmt.Sprintf("bench-%d", i%8)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAbsolutizeMedia rewrites a Markdown result with many image
// references against a media directory holding them.
func BenchmarkAbsolutizeMedia(b *testing.B) {
	media := b.TempDir()
	var md strings.Builder
	for i := 0; i < benchImageRefs; i++ {
		name := fmt.Sprintf("image%d.png", i)
		if err := os.WriteFile(filepath.Join(media, name), []byte("png"), 0o644); err != nil {
			b.Fatal(err)
		}
		fmt.Fprintf(&md, "Paragraph %d text.\n\n![figure](media/%s)\n\n<img src=\"%s\">\n\n", i, name, name)
	}
	src := md.String()
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := AbsolutizeMedia(src, media); err != nil {
			b.Fatal(err)
		}
	}
}
