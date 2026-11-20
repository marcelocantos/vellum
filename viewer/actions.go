// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/marcelocantos/vellum/clipboard"
	"github.com/marcelocantos/vellum/convert"
)

func queryAbsPath(r *http.Request) (string, error) {
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		return "", fmt.Errorf("missing path")
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("path must be absolute")
	}
	cleaned := filepath.Clean(p)
	if filepath.Dir(cleaned) == cleaned { // "/" or a bare drive root

		return "", fmt.Errorf("missing file path")
	}
	return cleaned, nil
}

func (s *Server) handlePDF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	absPath, err := queryAbsPath(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := s.openServed(absPath)
	if servedStatus(w, r, err) {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "is a directory", http.StatusBadRequest)
		return
	}

	cacheRoot, err := resolveCacheDir(s.CacheDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cachePath := filepath.Join(cacheRoot, cacheName(absPath, ".pdf"))
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	maxAge := effectiveMaxAge(s.MaxAge)

	hit := false
	if st, err := os.Stat(cachePath); err == nil && !st.IsDir() {
		ageOK := maxAge < 0 || now().Sub(st.ModTime()) <= maxAge
		if ageOK && stampMatches(cachePath, info) {
			hit = true
			_ = os.Chtimes(cachePath, now(), now())
		}
	}
	if !hit {
		var convErr error
		if s.ConvertPDF != nil {
			convErr = s.ConvertPDF(r.Context(), absPath, cachePath)
		} else {
			// Read the descriptor confine already opened. convert.Convert
			// would open the path again, and the engine would then fetch
			// every relative URL from the document's own directory.
			src, err := io.ReadAll(f)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			convErr = s.renderServedPDF(r.Context(), src, absPath, cachePath)
		}
		if convErr != nil {
			var se *convert.SoftError
			if !errors.As(convErr, &se) {
				http.Error(w, convErr.Error(), http.StatusInternalServerError)
				return
			}
		}
		if err := writeStamp(cachePath, info); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	_ = pruneCache(cacheRoot, cachePath, effectiveMaxBytes(s.MaxBytes), maxAge, now())

	name := downloadName(absPath, ".pdf")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, cachePath)
}

// renderServedPDF renders markdown already read from a served file.
//
// The PDF engine reads nothing by itself. Sanitize cuts the document's
// HTML down to the allow-list, and InlineResources turns every image the
// body names into a data: URL: a local one only through readServed (the
// check every page request passes), a remote one through
// fetchRemoteImage, which tells any viewer it reaches that it is a page's
// image. KaTeX's stylesheet and fonts are inlined from the embedded copy.
// WriteSelfContainedPDF then runs the engine with every fetch but data:
// refused, so what those images name in turn (an SVG's <image> or <use>,
// a file: URL, this viewer's own origin) goes nowhere.
//
// That puts every byte the engine sees through the same decision as the
// page's own requests. The alternatives were a fetcher inside the engine
// that asks the viewer (WeasyPrint's is Python, Prince has none) or a
// credential on the engine's requests (neither engine's command line can
// add a header); both leave the engine able to reach files and the
// network, and a mistake in them is an escape.
func (s *Server) renderServedPDF(ctx context.Context, src []byte, absPath, cachePath string) error {
	if err := convert.CheckDeps(s.Backend); err != nil {
		return err
	}
	page, soft, err := convert.Render(ctx, src, &convert.Options{
		Style:         s.Style,
		Backend:       s.Backend,
		Sanitize:      true,
		KaTeXInline:   true,
		MermaidFormat: convert.MermaidPNG,
	})
	if err != nil {
		return err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, remoteImagesBudget)
	defer cancel()
	page, err = convert.InlineResources(fetchCtx, page, filepath.Dir(absPath), &convert.InlineArgs{
		Read: func(p string) ([]byte, error) {
			b, _, err := s.readServed(p)
			return b, err
		},
		Fetch: fetchRemoteImage,
	})
	if err != nil {
		return err
	}
	if err := convert.WriteSelfContainedPDF(ctx, page, cachePath, &convert.Options{Style: s.Style, Backend: s.Backend}); err != nil {
		return err
	}
	if len(soft) > 0 {
		return &convert.SoftError{Messages: soft}
	}
	return nil
}

func (s *Server) handleClipboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	absPath, err := queryAbsPath(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The clipboard carries the whole document, so every deferred
	// diagram and expression is rendered now (cache hits are free).
	html, _, err := s.completeHTML(r.Context(), absPath)
	if servedStatus(w, r, err) {
		return
	}
	writeFn := s.WriteClipboard
	if writeFn == nil {
		writeFn = func(html string) error {
			_, err := clipboard.Write(clipboard.Payload{HTML: html})
			return err
		}
	}
	if err := writeFn(html); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, "ok\n")
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	absPath, err := queryAbsPath(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// statServed opens through the served root, so a symlink swapped in
	// after the request was parsed cannot point the reveal outside it.
	if _, err := s.statServed(absPath); err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		if servedStatus(w, r, err) {
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	revealFn := s.Reveal
	if revealFn == nil {
		revealFn = revealInFinder
	}
	if err := revealFn(absPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, "ok\n")
}

func revealInFinder(path string) error {
	switch runtime.GOOS {
	case "darwin":
	case "windows":
		// explorer.exe exits 1 even on success, so only a start failure
		// is an error.
		return exec.Command("explorer", "/select,"+filepath.Clean(path)).Start()
	default:
		return fmt.Errorf("reveal in file manager is not supported on %s", runtime.GOOS)
	}
	cmd := exec.Command("open", "-R", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func downloadName(absPath, ext string) string {
	base := filepath.Base(absPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	var b strings.Builder
	for _, r := range base {
		if r == '"' || r == '\\' || r == '/' || unicode.IsControl(r) {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "document" + ext
	}
	return b.String() + ext
}
