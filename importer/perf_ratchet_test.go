// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/perfbase"
)

// Allocation ratchets for the pure-Go import paths; see
// internal/perfbase for the band and how to re-record.
func TestRatchet_AbsolutizeMedia(t *testing.T) {
	media := t.TempDir()
	var md strings.Builder
	for i := 0; i < benchImageRefs; i++ {
		name := fmt.Sprintf("image%d.png", i)
		if err := os.WriteFile(filepath.Join(media, name), []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&md, "Paragraph %d text.\n\n![figure](media/%s)\n\n<img src=\"%s\">\n\n", i, name, name)
	}
	src := md.String()
	perfbase.Ratchet(t, "absolutize_media", func() {
		if _, _, err := AbsolutizeMedia(src, media); err != nil {
			t.Fatal(err)
		}
	})
}

// The steady-state BundleDir: prune throttled, so an import's cache
// bookkeeping must stay a handful of syscalls, not a cache walk.
func TestRatchet_BundleDirSteadyState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VELLUM_IMPORT_CACHE", root)
	populateCache(t, root)
	perfbase.Ratchet(t, "bundle_dir_steady_state", func() {
		if _, err := BundleDir("ratchet"); err != nil {
			t.Fatal(err)
		}
	})
}
