// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package viewer renders Markdown via a localhost view server (HTML) or a
// cache file (PDF) and opens the result. It also installs/uninstalls a macOS
// app bundle that registers vellum as the default handler for .md files.
package viewer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	// applies to HTML rendering on the view server.
	Style   *convert.Style
	Backend string
	// Open, when non-nil, opens the rendered path or view URL. Defaults to
	// OS open. Tests inject a no-op or recorder.
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
	// ViewBaseURL is the view-server origin (e.g. http://127.0.0.1:18742).
	// Empty uses DefaultViewOrigin. Tests point this at an httptest.
	ViewBaseURL string
	// SkipEnsureServer, when true, does not auto-start serve-view (tests
	// that already run a Server, or callers that require brew services).
	SkipEnsureServer bool
}

// View opens inputPath in the OS viewer. HTML mode opens a localhost view
// server URL (one GET = one convert; in-page .md links stay on-origin). PDF
// mode still renders to a path-keyed cache file and opens that file.
//
// HTML conversion happens on the server when the browser loads (or reloads)
// the URL; View itself does not crawl the Markdown link graph. The server
// re-converts when the source stamp/mtime changes.
func View(ctx context.Context, inputPath string, opts *ViewOptions) (opened string, err error) {
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

	if opts.Format != FormatPDF {
		return viewHTML(absInput, opts)
	}
	return viewPDF(ctx, absInput, info, opts)
}

func viewHTML(absInput string, opts *ViewOptions) (string, error) {
	origin := opts.ViewBaseURL
	if origin == "" {
		origin = DefaultViewOrigin()
	}
	if !opts.SkipEnsureServer {
		if err := EnsureViewServer(origin); err != nil {
			return "", err
		}
	} else if err := ProbeViewServer(origin); err != nil {
		return "", fmt.Errorf("view server not reachable at %s: %w", origin, err)
	}
	u := ViewURL(origin, absInput)
	openFn := opts.Open
	if openFn == nil {
		openFn = openPath
	}
	if err := openFn(u); err != nil {
		return u, fmt.Errorf("opening %s: %w", u, err)
	}
	return u, nil
}

func viewPDF(ctx context.Context, absInput string, info os.FileInfo, opts *ViewOptions) (string, error) {
	cacheRoot, err := resolveCacheDir(opts.CacheDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}

	cachePath := filepath.Join(cacheRoot, cacheName(absInput, ".pdf"))
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	maxAge := effectiveMaxAge(opts.MaxAge)

	hit := false
	if st, err := os.Stat(cachePath); err == nil && !st.IsDir() {
		ageOK := maxAge < 0 || now().Sub(st.ModTime()) <= maxAge
		if ageOK && stampMatches(cachePath, info) {
			hit = true
			_ = os.Chtimes(cachePath, now(), now())
		}
	}
	if !hit {
		if err := renderToCache(ctx, absInput, cachePath, opts); err != nil {
			var se *convert.SoftError
			if !errors.As(err, &se) {
				return "", err
			}
		}
	}

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
		if strings.HasSuffix(e.path, ".tmp") || strings.HasSuffix(e.path, ".stamp") {
			// Stamps are companions of the rendered file; drop orphans here
			// and when the HTML/PDF is deleted below.
			if strings.HasSuffix(e.path, ".stamp") {
				rendered := strings.TrimSuffix(e.path, ".stamp")
				if _, err := os.Stat(rendered); err == nil {
					kept = append(kept, e)
					continue
				}
			}
			_ = os.Remove(e.path)
			continue
		}
		if maxAge >= 0 && now.Sub(e.modTime) > maxAge {
			_ = os.Remove(e.path)
			_ = os.Remove(stampPath(e.path))
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
		_ = os.Remove(stampPath(e.path))
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
	// PDF only — HTML is served by Server (one GET = one convert).
	cOpts := &convert.Options{Style: opts.Style, Backend: opts.Backend}
	if err := convert.CheckDeps(opts.Backend); err != nil {
		return err
	}
	if err := convert.Convert(ctx, absInput, cachePath, cOpts); err != nil {
		return err
	}
	return writeStamp(cachePath, absInput)
}

func cacheName(absPath string, ext string) string {
	sum := sha256.Sum256([]byte(absPath))
	return hex.EncodeToString(sum[:8]) + ext
}

func stampPath(cachePath string) string {
	return cachePath + ".stamp"
}

func writeStamp(cachePath, absInput string) error {
	info, err := os.Stat(absInput)
	if err != nil {
		return fmt.Errorf("stamp stat source: %w", err)
	}
	body := strconv.FormatInt(info.ModTime().UnixNano(), 10) + " " + strconv.FormatInt(info.Size(), 10) + "\n"
	if err := os.WriteFile(stampPath(cachePath), []byte(body), 0o644); err != nil {
		return fmt.Errorf("writing cache stamp: %w", err)
	}
	return nil
}

func stampMatches(cachePath string, source os.FileInfo) bool {
	data, err := os.ReadFile(stampPath(cachePath))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return false
	}
	mtime, err1 := strconv.ParseInt(fields[0], 10, 64)
	size, err2 := strconv.ParseInt(fields[1], 10, 64)
	if err1 != nil || err2 != nil {
		return false
	}
	return mtime == source.ModTime().UnixNano() && size == source.Size()
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
