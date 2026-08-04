// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Import cache defaults (aligned with the view-cache policy idea:
// age expiry then size cap). Holds extracted images and PDF page renders
// so Markdown from clipboard/content keeps resolvable paths.
const (
	CacheMaxBytes int64 = 200 * 1024 * 1024 // 200 MiB
	CacheMaxAge         = 7 * 24 * time.Hour
)

// CacheRoot returns the directory that holds import media bundles.
// Override with VELLUM_IMPORT_CACHE for tests.
func CacheRoot() (string, error) {
	if d := os.Getenv("VELLUM_IMPORT_CACHE"); d != "" {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
		return d, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("import cache: user cache dir: %w", err)
	}
	dir := filepath.Join(base, "vellum", "import")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// BundleDir returns a fresh-or-existing subdirectory for key and prunes
// the cache. key should be a stable content identity (hex hash).
func BundleDir(key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("import cache: empty key")
	}
	// Keep path segment short and filesystem-safe.
	if len(key) > 64 {
		key = key[:64]
	}
	root, err := CacheRoot()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Touch so LRU by mtime prefers active bundles.
	now := time.Now()
	_ = os.Chtimes(dir, now, now)
	_ = pruneImportCache(root, dir, CacheMaxBytes, CacheMaxAge, now)
	return dir, nil
}

// HashBytes returns a hex SHA-256 of data for cache keys.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// HashFile returns a cache key from path + size + mtime.
func HashFile(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%d\n%d\n", path, st.Size(), st.ModTime().UnixNano())
	return hex.EncodeToString(h.Sum(nil)), nil
}

type dirEntry struct {
	path    string
	size    int64
	modTime time.Time
}

func pruneImportCache(root, keepDir string, maxBytes int64, maxAge time.Duration, now time.Time) error {
	ents, err := listBundleDirs(root)
	if err != nil {
		return err
	}
	keepAbs, _ := filepath.Abs(keepDir)

	var kept []dirEntry
	for _, e := range ents {
		abs, _ := filepath.Abs(e.path)
		if abs == keepAbs {
			kept = append(kept, e)
			continue
		}
		if maxAge >= 0 && now.Sub(e.modTime) > maxAge {
			_ = os.RemoveAll(e.path)
			continue
		}
		kept = append(kept, e)
	}
	ents = kept

	if maxBytes < 0 {
		return nil
	}
	var total int64
	for _, e := range ents {
		total += e.size
	}
	if total <= maxBytes {
		return nil
	}
	sort.Slice(ents, func(i, j int) bool {
		return ents[i].modTime.Before(ents[j].modTime)
	})
	for _, e := range ents {
		if total <= maxBytes {
			break
		}
		abs, _ := filepath.Abs(e.path)
		if abs == keepAbs {
			continue
		}
		if err := os.RemoveAll(e.path); err != nil {
			continue
		}
		total -= e.size
	}
	return nil
}

func listBundleDirs(root string) ([]dirEntry, error) {
	dents, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []dirEntry
	for _, d := range dents {
		if !d.IsDir() {
			continue
		}
		p := filepath.Join(root, d.Name())
		info, err := d.Info()
		if err != nil {
			continue
		}
		size, _ := dirSize(p)
		out = append(out, dirEntry{path: p, size: size, modTime: info.ModTime()})
	}
	return out, nil
}

func dirSize(root string) (int64, error) {
	var n int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			n += info.Size()
		}
		return nil
	})
	return n, err
}
