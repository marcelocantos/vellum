// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// bundleCount reports how many bundle directories root holds.
func bundleCount(t *testing.T, root string) int {
	t.Helper()
	ents, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ents {
		if e.IsDir() {
			n++
		}
	}
	return n
}

// writeStale creates a bundle whose mtime is past CacheMaxAge, so a
// prune must remove it.
func writeStale(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-CacheMaxAge - time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
}

// The prune walk runs at most once per pruneInterval. The first
// BundleDir on a cache prunes; a second one inside the interval leaves
// stale bundles alone; once the stamp is old enough, pruning resumes.
func TestBundleDir_PruneThrottled(t *testing.T) {
	root := t.TempDir()
	t.Setenv("VELLUM_IMPORT_CACHE", root)

	writeStale(t, root, "stale-1")
	if _, err := BundleDir("live"); err != nil {
		t.Fatal(err)
	}
	if n := bundleCount(t, root); n != 1 {
		t.Fatalf("first BundleDir left %d bundles, want 1 (stale pruned)", n)
	}

	writeStale(t, root, "stale-2")
	if _, err := BundleDir("live"); err != nil {
		t.Fatal(err)
	}
	if n := bundleCount(t, root); n != 2 {
		t.Fatalf("BundleDir inside prune interval left %d bundles, want 2 (no prune)", n)
	}

	stamp := filepath.Join(root, pruneStampName)
	old := time.Now().Add(-pruneInterval - time.Minute)
	if err := os.Chtimes(stamp, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := BundleDir("live"); err != nil {
		t.Fatal(err)
	}
	if n := bundleCount(t, root); n != 1 {
		t.Fatalf("BundleDir after interval left %d bundles, want 1 (prune resumed)", n)
	}
}
