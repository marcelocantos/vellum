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
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("path must be absolute")
	}
	cleaned := filepath.Clean(p)
	if cleaned == "/" {
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
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
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
		convertFn := s.ConvertPDF
		if convertFn == nil {
			convertFn = func(ctx context.Context, in, out string) error {
				if err := convert.CheckDeps(s.Backend); err != nil {
					return err
				}
				return convert.Convert(ctx, in, out, &convert.Options{Style: s.Style, Backend: s.Backend})
			}
		}
		if err := convertFn(r.Context(), absPath, cachePath); err != nil {
			var se *convert.SoftError
			if !errors.As(err, &se) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if err := writeStamp(cachePath, absPath); err != nil {
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
	html, err := s.cachedHTML(r.Context(), absPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, errIsDirectory) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeFn := s.WriteClipboard
	if writeFn == nil {
		writeFn = func(html string) error {
			_, err := clipboard.Write(clipboard.Payload{HTML: html})
			return err
		}
	}
	if err := writeFn(string(html)); err != nil {
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
	if _, err := os.Stat(absPath); err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
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
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("reveal in Finder is only supported on macOS")
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
