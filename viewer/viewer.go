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
}

// View renders inputPath to a cache location (keyed by absolute path +
// mtime + format) and opens the result. Unchanged sources hit the cache
// and skip re-render. The cache never writes next to the source file.
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

	if st, err := os.Stat(cachePath); err == nil && !st.IsDir() {
		// Cache hit.
	} else {
		if err := renderToCache(ctx, absInput, cachePath, opts); err != nil {
			return "", err
		}
	}

	openFn := opts.Open
	if openFn == nil {
		openFn = openPath
	}
	if err := openFn(cachePath); err != nil {
		return cachePath, fmt.Errorf("opening %s: %w", cachePath, err)
	}
	return cachePath, nil
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
