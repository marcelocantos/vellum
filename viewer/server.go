// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/marcelocantos/vellum/convert"
)

const (
	// DefaultViewAddr is the loopback bind address for the Markdown view server.
	DefaultViewAddr = "127.0.0.1:18742"
	// HealthPath is the liveness probe path (not a filesystem path).
	HealthPath = "/healthz"
)

// Server serves one Markdown→HTML conversion per GET. Relative .md links are
// rewritten to same-origin URLs. Non-Markdown paths under the same listener
// are served as static files so relative images resolve without file://.
type Server struct {
	// Addr is the listen address (host:port). Empty means DefaultViewAddr.
	// Host must be loopback; Serve rejects non-loopback binds.
	Addr string
	// CacheDir overrides the default view cache root (tests).
	CacheDir string
	Style    *convert.Style
	Backend  string
	// MaxBytes / MaxAge / Now mirror ViewOptions cache knobs (tests).
	MaxBytes int64
	MaxAge   time.Duration
	Now      func() time.Time

	// ConvertCount increments once per Markdown convert (not cache hits).
	// Tests assert a single-file GET never crawls the link graph.
	ConvertCount atomic.Int64

	// RenderFile, when non-nil, replaces convert.RenderFile (tests).
	RenderFile func(ctx context.Context, path string, opts *convert.Options) (string, []string, error)
}

// Origin returns the http://host:port origin for Addr (no trailing slash).
func (s *Server) Origin() string {
	addr := s.addr()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func (s *Server) addr() string {
	if s != nil && s.Addr != "" {
		return s.Addr
	}
	if env := strings.TrimSpace(os.Getenv("VELLUM_VIEW_ADDR")); env != "" {
		return env
	}
	return DefaultViewAddr
}

// Handler returns the HTTP handler for the view server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(HealthPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/", s.handlePath)
	return mux
}

// ListenAndServe binds Addr (localhost only) and serves until ctx is cancelled
// or the listener fails. Returns http.ErrServerClosed on graceful shutdown.
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := s.addr()
	if err := requireLoopbackAddr(addr); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("view server listen %s: %w", addr, err)
	}
	// Re-check after listen in case Addr was ":port" (rejected above) or
	// the kernel remapped — refuse non-loopback local addresses.
	if ta, ok := ln.Addr().(*net.TCPAddr); ok && ta.IP != nil && !ta.IP.IsLoopback() {
		_ = ln.Close()
		return fmt.Errorf("view server refuses non-loopback bind %s", ln.Addr())
	}

	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		err := <-errCh
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return ctx.Err()
		}
		return err
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func requireLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// bare port ":18742" binds all interfaces — refuse.
		if strings.HasPrefix(addr, ":") {
			return fmt.Errorf("view server must bind loopback (got %q); use 127.0.0.1:%s", addr, strings.TrimPrefix(addr, ":"))
		}
		return fmt.Errorf("view server addr: %w", err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return fmt.Errorf("view server must bind loopback (got %q)", addr)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		// Literal parse
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("view server resolve %q: %w", host, err)
		}
		if !ip.IsLoopback() {
			return fmt.Errorf("view server must bind loopback (got %q)", addr)
		}
		return nil
	}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return fmt.Errorf("view server must bind loopback (got %q)", addr)
		}
	}
	return nil
}

func (s *Server) handlePath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fsPath, err := urlPathToFS(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ext := strings.ToLower(filepath.Ext(fsPath))
	if ext == ".md" || ext == ".markdown" {
		s.serveMarkdown(w, r, fsPath)
		return
	}
	s.serveFile(w, r, fsPath)
}

func urlPathToFS(p string) (string, error) {
	if p == "" || p == "/" {
		return "", fmt.Errorf("missing file path")
	}
	// Request path is URL-escaped; Unescape to filesystem form.
	u, err := url.PathUnescape(p)
	if err != nil {
		return "", fmt.Errorf("bad path: %w", err)
	}
	if !strings.HasPrefix(u, "/") {
		u = "/" + u
	}
	cleaned := filepath.Clean(u)
	if cleaned == "/" {
		return "", fmt.Errorf("missing file path")
	}
	return cleaned, nil
}

func (s *Server) serveMarkdown(w http.ResponseWriter, r *http.Request, absPath string) {
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
	cachePath := filepath.Join(cacheRoot, cacheName(absPath, ".html"))
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	maxAge := effectiveMaxAge(s.MaxAge)
	origin := requestOrigin(r)

	hit := false
	if st, err := os.Stat(cachePath); err == nil && !st.IsDir() {
		ageOK := maxAge < 0 || now().Sub(st.ModTime()) <= maxAge
		if ageOK && stampMatches(cachePath, info) {
			hit = true
			_ = os.Chtimes(cachePath, now(), now())
		}
	}
	if !hit {
		if err := s.renderMarkdown(r.Context(), absPath, cachePath); err != nil {
			var se *convert.SoftError
			if !errors.As(err, &se) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}
	_ = pruneCache(cacheRoot, cachePath, effectiveMaxBytes(s.MaxBytes), maxAge, now())

	body, err := os.ReadFile(cachePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	html := rewriteMarkdownHrefs(string(body), absPath, origin)
	// Ensure <base> matches this request's origin (cache may predate a port change).
	html = ensureBaseHref(html, origin+pathURL(filepath.Dir(absPath))+"/")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = io.WriteString(w, html)
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) renderMarkdown(ctx context.Context, absInput, cachePath string) error {
	s.ConvertCount.Add(1)
	cOpts := &convert.Options{Style: s.Style, Backend: s.Backend}
	// Placeholder base; rewritten per-response in serveMarkdown.
	cOpts.HeadExtra = `<meta http-equiv="Cache-Control" content="no-store, no-cache, must-revalidate">` + "\n" +
		`<meta http-equiv="Pragma" content="no-cache">` + "\n" +
		`<base href="about:blank">`

	render := s.RenderFile
	if render == nil {
		render = convert.RenderFile
	}
	html, soft, err := render(ctx, absInput, cOpts)
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
	if err := writeStamp(cachePath, absInput); err != nil {
		return err
	}
	if len(soft) > 0 {
		return &convert.SoftError{Messages: soft}
	}
	return nil
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, absPath string) {
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
	f, err := os.Open(absPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	ctype := mime.TypeByExtension(filepath.Ext(absPath))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// ViewURL returns the same-origin URL the view server uses for absPath.
func ViewURL(origin, absPath string) string {
	origin = strings.TrimRight(origin, "/")
	return origin + pathURL(absPath)
}

// DefaultViewOrigin is http://127.0.0.1:18742 (or VELLUM_VIEW_ADDR).
func DefaultViewOrigin() string {
	s := &Server{}
	return s.Origin()
}

// ProbeViewServer returns nil if HealthPath responds OK at origin.
func ProbeViewServer(origin string) error {
	origin = strings.TrimRight(origin, "/")
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(origin + HealthPath)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("view server health: %s", resp.Status)
	}
	return nil
}
