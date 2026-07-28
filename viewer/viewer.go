// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package viewer renders Markdown to a cache location and opens it in the
// OS default viewer. It also installs/uninstalls a macOS app bundle that
// registers vellum as the default handler for .md files.
package viewer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/marcelocantos/vellum/convert"
)

// Format selects the rendered form for View.
type Format int

const (
	// FormatHTML is the fast default: full HTML opened in the browser.
	FormatHTML Format = iota
	// FormatPDF is the high-fidelity option: PDF via WeasyPrint/Prince.
	FormatPDF
)

// Cache health defaults. Pruned on every View after the current entry is
// ready: drop entries older than CacheMaxAge, then if total size still
// exceeds CacheMaxBytes drop oldest-by-mtime until under the cap. The
// entry about to be opened is never deleted.
const (
	CacheMaxBytes int64 = 50 * 1024 * 1024 // 50 MiB
	CacheMaxAge         = 7 * 24 * time.Hour
)

// ViewOptions configures a single View call.
type ViewOptions struct {
	// Format is the rendered form (HTML default, PDF optional).
	Format Format
	// Style and Backend are forwarded to convert for PDF mode; Style also
	// applies to HTML rendering.
	Style   *convert.Style
	Backend string
	// Open, when non-nil, opens the rendered path. Defaults to OS open.
	// Tests inject a no-op or recorder.
	Open func(path string) error
	// CacheDir overrides the default cache root (for tests).
	CacheDir string
	// MaxBytes overrides CacheMaxBytes (for tests). Zero means default;
	// negative disables the size cap.
	MaxBytes int64
	// MaxAge overrides CacheMaxAge (for tests). Zero means default;
	// negative disables age expiry.
	MaxAge time.Duration
	// Now, when non-nil, supplies the clock for age checks (tests).
	Now func() time.Time
}

// View renders inputPath to a cache location (keyed by absolute path +
// mtime + format) and opens the result. Unchanged sources hit the cache
// and skip re-render. The cache never writes next to the source file.
//
// After the entry is ready, the cache is pruned to CacheMaxAge and
// CacheMaxBytes (see package constants; overridable via ViewOptions).
func View(ctx context.Context, inputPath string, opts *ViewOptions) (renderedPath string, err error) {
	if opts == nil {
		opts = &ViewOptions{}
	}
	absInput, err := filepath.Abs(inputPath)
	if err != nil {
		return "", fmt.Errorf("resolving input path: %w", err)
	}
	info, err := os.Stat(absInput)
	if err != nil {
		return "", fmt.Errorf("stat input: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("input is a directory: %s", absInput)
	}

	cacheRoot, err := resolveCacheDir(opts.CacheDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}

	ext := ".html"
	if opts.Format == FormatPDF {
		ext = ".pdf"
	}
	cachePath := filepath.Join(cacheRoot, cacheName(absInput, info.ModTime(), ext))
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	maxAge := effectiveMaxAge(opts.MaxAge)

	hit := false
	if st, err := os.Stat(cachePath); err == nil && !st.IsDir() {
		// Age-expired entries are treated as misses so content re-renders
		// and the stale file is eligible for prune.
		if maxAge < 0 || now().Sub(st.ModTime()) <= maxAge {
			hit = true
			// Touch mtime so LRU-by-mtime size eviction prefers active entries.
			_ = os.Chtimes(cachePath, now(), now())
		}
	}
	if !hit {
		if err := renderToCache(ctx, absInput, cachePath, opts); err != nil {
			return "", err
		}
	}

	// Best-effort prune; never fail the view because cleanup failed.
	_ = pruneCache(cacheRoot, cachePath, effectiveMaxBytes(opts.MaxBytes), maxAge, now())

	openFn := opts.Open
	if openFn == nil {
		openFn = openPath
	}
	if err := openFn(cachePath); err != nil {
		return cachePath, fmt.Errorf("opening %s: %w", cachePath, err)
	}
	return cachePath, nil
}

func effectiveMaxBytes(override int64) int64 {
	if override == 0 {
		return CacheMaxBytes
	}
	return override
}

func effectiveMaxAge(override time.Duration) time.Duration {
	if override == 0 {
		return CacheMaxAge
	}
	return override
}

// cacheEntry is one regular file in the view cache directory.
type cacheEntry struct {
	path    string
	size    int64
	modTime time.Time
}

// pruneCache removes age-expired entries, then drops oldest-by-mtime until
// total size is within maxBytes. keepPath is never deleted. maxAge < 0
// disables age pruning; maxBytes < 0 disables the size cap.
func pruneCache(dir, keepPath string, maxBytes int64, maxAge time.Duration, now time.Time) error {
	entries, err := listCacheEntries(dir)
	if err != nil {
		return err
	}
	keepBase := filepath.Base(keepPath)

	// Pass 1: drop write temps and age-expired entries.
	var kept []cacheEntry
	for _, e := range entries {
		if e.path == keepPath || filepath.Base(e.path) == keepBase {
			kept = append(kept, e)
			continue
		}
		if strings.HasSuffix(e.path, ".tmp") {
			_ = os.Remove(e.path)
			continue
		}
		if maxAge >= 0 && now.Sub(e.modTime) > maxAge {
			_ = os.Remove(e.path)
			continue
		}
		kept = append(kept, e)
	}
	entries = kept

	// Pass 2: size cap — delete oldest first until under budget.
	if maxBytes < 0 {
		return nil
	}
	var total int64
	for _, e := range entries {
		total += e.size
	}
	if total <= maxBytes {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].modTime.Before(entries[j].modTime)
	})
	for _, e := range entries {
		if total <= maxBytes {
			break
		}
		if e.path == keepPath || filepath.Base(e.path) == keepBase {
			continue
		}
		if err := os.Remove(e.path); err != nil {
			continue
		}
		total -= e.size
	}
	return nil
}

func listCacheEntries(dir string) ([]cacheEntry, error) {
	dents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []cacheEntry
	for _, d := range dents {
		if d.IsDir() {
			continue
		}
		info, err := d.Info()
		if err != nil {
			continue
		}
		out = append(out, cacheEntry{
			path:    filepath.Join(dir, d.Name()),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
	}
	return out, nil
}

func renderToCache(ctx context.Context, absInput, cachePath string, opts *ViewOptions) error {
	cOpts := &convert.Options{Style: opts.Style, Backend: opts.Backend}

	switch opts.Format {
	case FormatPDF:
		if err := convert.CheckDeps(opts.Backend); err != nil {
			return err
		}
		return convert.Convert(ctx, absInput, cachePath, cOpts)
	default:
		// HTML: inject <base> so relative images resolve against the
		// source Markdown's directory even though the HTML lives in cache.
		base := url.URL{Scheme: "file", Path: filepath.Dir(absInput) + string(filepath.Separator)}
		cOpts.HeadExtra = fmt.Sprintf(`<base href="%s">`, base.String())
		html, err := convert.RenderFile(ctx, absInput, cOpts)
		if err != nil {
			return err
		}
		tmp := cachePath + ".tmp"
		if err := os.WriteFile(tmp, []byte(html), 0o644); err != nil {
			return fmt.Errorf("writing cache: %w", err)
		}
		if err := os.Rename(tmp, cachePath); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("finalising cache: %w", err)
		}
		return nil
	}
}

func cacheName(absPath string, mtime time.Time, ext string) string {
	sum := sha256.Sum256([]byte(absPath))
	return hex.EncodeToString(sum[:8]) + "-" + strconv.FormatInt(mtime.UnixNano(), 16) + ext
}

func resolveCacheDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving user cache dir: %w", err)
	}
	return filepath.Join(base, "vellum", "view"), nil
}

func openPath(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		return fmt.Errorf("viewer: open not supported on %s", runtime.GOOS)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
