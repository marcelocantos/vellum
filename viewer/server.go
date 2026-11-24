// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marcelocantos/vellum/convert"
	"github.com/marcelocantos/vellum/embed"
)

const (
	// DefaultViewAddr is the loopback bind address for the Markdown view server.
	DefaultViewAddr = "127.0.0.1:18742"
	// HealthPath is the liveness probe path (not a filesystem path).
	HealthPath = "/healthz"
	// MCPPath is the streamable HTTP MCP endpoint (not a filesystem path).
	MCPPath = "/mcp"

	// readHeaderTimeout bounds how long a client may take to send its
	// request headers; shutdownTimeout bounds a graceful stop.
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 5 * time.Second
)

var errIsDirectory = errors.New("is a directory")

// Server serves one Markdown→HTML conversion per GET. Relative .md links are
// rewritten to same-origin URLs. Non-Markdown paths under the same listener
// are served as static files so relative images resolve without file://.
// When MCP is set, streamable HTTP MCP is mounted at MCPPath on this listener.
type Server struct {
	// Addr is the listen address (host:port). Empty means DefaultViewAddr.
	// Host must be loopback; Serve rejects non-loopback binds.
	Addr string
	// T3Addr, when set, is a second loopback listener for T3 Code, which
	// shows documents inside its own window (see t3.go). ListenAndServe
	// runs it beside the view listener and advertises it to T3.
	T3Addr string
	// Log, when non-nil, receives one-line notes about the T3 listener.
	Log func(msg string)
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

	// RenderFile, when non-nil, replaces convert.RenderFile (tests). The
	// server always asks for a deferred render (convert.Options.Defer):
	// the cached page is a shell whose diagrams and math are placeholders.
	RenderFile func(ctx context.Context, path string, opts *convert.Options) (string, []string, error)
	// RenderFragments, when non-nil, replaces convert.RenderFragments
	// (tests stand in for mmdc and node).
	RenderFragments func(ctx context.Context, frags []convert.Fragment) (map[string]convert.FragmentResult, error)
	// FragmentRenderCount counts fragments handed to RenderFragments
	// (cache misses only). Tests assert the fragment cache is hit.
	FragmentRenderCount atomic.Int64
	flights             flightGroup

	// MCP, when non-nil, is mounted at MCPPath so the brew-service
	// process hosts streamable HTTP MCP on the same listener as the view.
	MCP http.Handler

	// WriteClipboard, when non-nil, replaces clipboard.Write (tests).
	WriteClipboard func(html string) error
	// Reveal, when non-nil, replaces `open -R` (tests).
	Reveal func(path string) error
	// ConvertPDF, when non-nil, replaces convert.Convert for PDF download (tests).
	ConvertPDF func(ctx context.Context, input, output string) error

	// WatchHeartbeat is the WebSocket ping interval.
	// Zero means DefaultWatchHeartbeat. Tests inject a shorter tick.
	WatchHeartbeat time.Duration
	// WatchPingCount increments once per successful protocol ping (tests).
	WatchPingCount atomic.Int64

	watchOnce sync.Once
	watches   *watchHub

	// WriteFile, when non-nil, replaces atomic Markdown writes (task-toggle
	// and editor-save tests). The path has passed the served-roots check.
	WriteFile func(path string, data []byte) error

	// writeMu serializes a task toggle's read, re-check, and write, so two
	// clicks that saw the same stamp cannot both succeed.
	writeMu sync.Mutex

	// Roots are folders served from the start. Every other folder is
	// served once the user opens a document in it (see roots.go).
	Roots []string

	// beforeOpen, when non-nil, runs between the served-roots check and
	// the open (tests swap the path on disk there).
	beforeOpen func(path string)

	mruMu sync.Mutex

	rootsMu      sync.Mutex
	grantsLoaded bool
	granted      []servedRoot
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

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) cacheRoot() (string, error) {
	root, err := resolveCacheDir(s.CacheDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}

// convertOptions is the shared convert configuration for the view page
// and its fragments; ExtractFragments must see the same Mermaid format
// as the deferred render so ids agree.
func (s *Server) convertOptions() *convert.Options {
	return &convert.Options{
		Style:        s.Style,
		Backend:      s.Backend,
		Defer:        true,
		SourceLines:  true,
		Sanitize:     true,
		KaTeXCSSHref: ChromeKaTeXPrefix + embed.KaTeXCSSName,
	}
}

// ViewAddr is the address the view listener binds: Addr, else
// VELLUM_VIEW_ADDR, else DefaultViewAddr.
func (s *Server) ViewAddr() string { return s.addr() }

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
	if s != nil && s.MCP != nil {
		mux.Handle(MCPPath, s.MCP)
		mux.Handle(MCPPath+"/", s.MCP)
	}
	mux.HandleFunc(ChromePDFPath, s.handlePDF)
	mux.HandleFunc(ChromeClipboardPath, s.handleClipboard)
	mux.HandleFunc(ChromeRevealPath, s.handleReveal)
	mux.HandleFunc(ChromeWatchPath, s.handleWatch)
	mux.HandleFunc(ChromeTaskTogglePath, s.handleTaskToggle)
	mux.HandleFunc(ChromeEditPath, s.handleEdit)
	mux.HandleFunc(ChromeEditorPrefix, s.handleEditorAsset)
	mux.HandleFunc(ChromeFragmentPath, s.handleFragment)
	mux.HandleFunc(ChromeFaviconPath, s.handleFavicon)
	mux.HandleFunc(ChromeFileManagerIconPath, s.handleFileManagerIcon)
	mux.Handle(ChromeKaTeXPrefix, http.StripPrefix(ChromeKaTeXPrefix, http.FileServerFS(embed.KaTeX())))
	mux.HandleFunc(FaviconICOPath, s.handleFavicon)
	mux.HandleFunc("/", s.handlePath)
	// Routes that act on the user's machine or files. Cross-site requests
	// never reach them, whatever their method or kind.
	stateChanging := map[string]bool{
		MCPPath:              true,
		MCPPath + "/":        true,
		ChromeClipboardPath:  true,
		ChromeRevealPath:     true,
		ChromeTaskTogglePath: true,
		ChromeEditPath:       true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The liveness probe serves a constant response and needs no browser context.
		if r.URL.Path == HealthPath {
			mux.ServeHTTP(w, r)
			return
		}
		// Ask the mux which registration will serve the request, so the
		// guard and the router cannot disagree about the route.
		_, pattern := mux.Handler(r)
		if !localRequestAllowed(r, stateChanging[pattern]) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Every response is inert unless its handler says otherwise: the
		// viewer's own pages replace this policy with one that admits
		// their nonce'd scripts. nosniff stops a document's text (source,
		// a fragment) being run as a script or styled as a page.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set(cspHeader, lockdownCSP)
		mux.ServeHTTP(w, r)
	})
}

// Fetch metadata request headers (https://www.w3.org/TR/fetch-metadata/).
// A page cannot set or alter them; the browser does.
const (
	secFetchSiteHeader = "Sec-Fetch-Site"
	secFetchModeHeader = "Sec-Fetch-Mode"
	secFetchDestHeader = "Sec-Fetch-Dest"

	secFetchSiteSameOrigin = "same-origin"
	secFetchSiteNone       = "none" // typed URL, bookmark, link opened from another app
	secFetchModeNavigate   = "navigate"
	secFetchDestDocument   = "document" // top-level; frames send iframe, frame, embed or object
)

// localRequestAllowed is the viewer's request rule. A loopback listener
// still receives requests that foreign browser pages cause, so:
//
//  1. Host must be a loopback IP literal or localhost on every route. It is
//     checked as written, never resolved, which stops DNS rebinding.
//  2. A present Origin must be this server. A browser attaches Origin to
//     cross-origin scripted requests and form posts, never to a link
//     navigation, so a foreign Origin always marks a request to refuse.
//  3. When the browser sends Sec-Fetch-Site, same-origin and none pass.
//     Any other value is a cross-site request, and passes only as a plain
//     top-level GET navigation (Sec-Fetch-Mode navigate, Sec-Fetch-Dest
//     document) to a route that changes nothing. That is a link followed
//     from another page: the user sees their own document and the linking
//     page cannot read it. Subresource loads, fetches, frames, non-GET
//     methods and every state-changing route are refused.
//  4. Without Sec-Fetch-Site (curl, MCP clients, browsers that predate
//     fetch metadata) rules 1 and 2 are the whole check: such clients may
//     omit Origin, with any method.
func localRequestAllowed(r *http.Request, stateChanging bool) bool {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			host = host[1 : len(host)-1]
		}
	}
	if !strings.EqualFold(host, "localhost") && !net.ParseIP(host).IsLoopback() {
		return false
	}
	for _, origin := range r.Header.Values("Origin") {
		if !strings.EqualFold(origin, requestOrigin(r)) {
			return false
		}
	}
	crossSite := false
	for _, site := range r.Header.Values(secFetchSiteHeader) {
		if site != secFetchSiteSameOrigin && site != secFetchSiteNone {
			crossSite = true
		}
	}
	if !crossSite {
		return true
	}
	return !stateChanging &&
		r.Method == http.MethodGet &&
		r.Header.Get(secFetchModeHeader) == secFetchModeNavigate &&
		r.Header.Get(secFetchDestHeader) == secFetchDestDocument
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
		ReadHeaderTimeout: readHeaderTimeout,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()
	t3Done := make(chan struct{})
	t3Ctx, stopT3 := context.WithCancel(ctx)
	go func() {
		defer close(t3Done)
		if s.T3Addr != "" {
			s.serveT3(t3Ctx)
		}
	}()
	// The discovery file is removed before this returns, so a stopped
	// viewer is never left advertised.
	defer func() {
		stopT3()
		<-t3Done
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
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

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = io.WriteString(w, faviconSVG)
}

func (s *Server) handlePath(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == MCPPath || strings.HasPrefix(r.URL.Path, MCPPath+"/") {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == strings.TrimSuffix(ChromePrefix, "/") || strings.HasPrefix(r.URL.Path, ChromePrefix) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/" {
		s.serveHome(w, r)
		return
	}
	fsPath, err := urlPathToFS(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ext := strings.ToLower(filepath.Ext(fsPath))
	if ext == ".md" || ext == ".markdown" {
		// Opening a document from outside any page serves its folder. A
		// grant that fails (no such folder) leaves the check below to
		// refuse the path.
		if r.Method == http.MethodGet && openedByUser(r) {
			_ = s.grant(fsPath)
		}
		s.serveMarkdown(w, r, fsPath, false)
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
	if runtime.GOOS == "windows" {
		return windowsURLPathToFS(u)
	}
	cleaned := filepath.Clean(u)
	if cleaned == "/" {
		return "", fmt.Errorf("missing file path")
	}
	return cleaned, nil
}

// serveMarkdown writes the page for absPath. embedded is the page shown
// inside T3 Code (t3.go): every URL in it is relative and it offers no
// action that would act on this machine instead of the reader's.
func (s *Server) serveMarkdown(w http.ResponseWriter, r *http.Request, absPath string, embedded bool) {
	body, stamp, err := s.cachedHTML(r.Context(), absPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.serveMissingMarkdown(w, r, absPath, embedded)
			return
		}
		servedStatus(w, r, err)
		return
	}
	page := string(body)
	if embedded {
		page = embedLocalRefs(page, absPath)
	} else {
		origin := requestOrigin(r)
		page = rewriteMarkdownHrefs(page, absPath, origin)
		// The <base> below points at the source directory (for relative images
		// and links), which would also capture fragment-only hrefs: [x](#id)
		// would navigate to the directory. Pin them to this document's URL.
		page = rewriteFragmentHrefs(page, documentURLPath(r))
		// Ensure <base> matches this request's origin (cache may predate a port change).
		page = ensureBaseHref(page, origin+pathURL(filepath.Dir(absPath))+"/")
	}
	// First paint waits on text only: the cached page is a shell whose
	// Mermaid diagrams and math are placeholders that chrome.js fills from
	// ChromeFragmentPath on demand, and images load lazily.
	if cacheRoot, err := s.cacheRoot(); err == nil {
		page = reserveDiagramBoxes(page, cacheRoot)
	}
	page = lazyLoadImages(page)
	nonce := newNonce()
	page = injectChrome(page, &chromePage{SourcePath: absPath, Nonce: nonce, Stamp: stamp, Embedded: embedded})
	// A cache-dir failure must not hide a document that is already rendered.
	_ = s.noteRecent(absPath)

	w.Header().Set(cspHeader, documentCSP(r, nonce))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = io.WriteString(w, page)
}

func (s *Server) serveMissingMarkdown(w http.ResponseWriter, r *http.Request, absPath string, embedded bool) {
	name := html.EscapeString(filepath.Base(absPath))
	doc := `<!DOCTYPE html><html><head><title>` + name + `</title></head><body><p>` + name + ` is not here yet. This page will show the document when the file appears.</p></body></html>`
	nonce := newNonce()
	page := injectChrome(doc, &chromePage{SourcePath: absPath, Nonce: nonce, Waiting: true, Embedded: embedded})
	w.Header().Set(cspHeader, documentCSP(r, nonce))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, page)
}

// cachedHTML returns the convert shell for absPath (chrome-free): the
// goldmark page with Mermaid diagrams and math left as placeholders. It
// is the payload for the view page (chrome is injected after); the
// clipboard wants completeHTML, which resolves the placeholders. stamp is
// the source stamp the shell was rendered from: a task toggle from the
// page names it, so an edit made since then is refused, not misapplied.
func (s *Server) cachedHTML(ctx context.Context, absPath string) (shell []byte, stamp string, err error) {
	f, err := s.openServed(absPath)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	if info.IsDir() {
		return nil, "", errIsDirectory
	}

	cacheRoot, err := s.cacheRoot()
	if err != nil {
		return nil, "", err
	}
	cachePath := filepath.Join(cacheRoot, cacheName(absPath, ".html"))
	now := s.now()
	maxAge := effectiveMaxAge(s.MaxAge)

	hit := false
	if st, err := os.Stat(cachePath); err == nil && !st.IsDir() {
		ageOK := maxAge < 0 || now.Sub(st.ModTime()) <= maxAge
		if ageOK && stampMatches(cachePath, info) {
			hit = true
			_ = os.Chtimes(cachePath, now, now)
		}
	}
	if !hit {
		if err := s.renderMarkdown(ctx, absPath, f, info, cachePath); err != nil {
			var se *convert.SoftError
			if !errors.As(err, &se) {
				return nil, "", err
			}
		}
	}
	_ = pruneCache(cacheRoot, cachePath, effectiveMaxBytes(s.MaxBytes), maxAge, now)
	shell, err = os.ReadFile(cachePath)
	if err != nil {
		return nil, "", err
	}
	// A hit matched this info's stamp; a render recorded it.
	return shell, sourceStamp(info), nil
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// renderMarkdown renders src (the open document at absInput, stat'd as
// info before reading) into cachePath. The stamp records info, so it is
// never newer than the bytes rendered.
func (s *Server) renderMarkdown(ctx context.Context, absInput string, src io.Reader, info os.FileInfo, cachePath string) error {
	s.ConvertCount.Add(1)
	cOpts := s.convertOptions()
	// Placeholder base; rewritten per-response in serveMarkdown.
	cOpts.HeadExtra = `<meta http-equiv="Cache-Control" content="no-store, no-cache, must-revalidate">` + "\n" +
		`<meta http-equiv="Pragma" content="no-cache">` + "\n" +
		`<base href="about:blank">`

	var (
		html string
		soft []string
		err  error
	)
	if s.RenderFile != nil {
		html, soft, err = s.RenderFile(ctx, absInput, cOpts)
	} else {
		var data []byte
		if data, err = io.ReadAll(src); err != nil {
			return err
		}
		html, soft, err = convert.Render(ctx, data, cOpts)
	}
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
	if err := writeStamp(cachePath, info); err != nil {
		return err
	}
	if len(soft) > 0 {
		return &convert.SoftError{Messages: soft}
	}
	return nil
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, absPath string) {
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
	ctype := mime.TypeByExtension(filepath.Ext(absPath))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	// The lockdown policy (sandbox) keeps an HTML or SVG file a document
	// links to from running script on this origin. A browser's PDF viewer
	// will not open in a sandbox, and a PDF runs nothing here.
	if strings.HasPrefix(ctype, "application/pdf") {
		w.Header().Del(cspHeader)
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

// windowsURLPathToFS maps a view-server URL path such as /C:/docs/a.md
// (what pathURL produces for C:\docs\a.md) back to a drive-letter path.
// Only absolute drive paths are served; anything else is rejected so a
// request cannot resolve relative to the server's working directory.
func windowsURLPathToFS(u string) (string, error) {
	rest := strings.TrimPrefix(u, "/")
	if !hasDrivePrefix(rest) {
		return "", fmt.Errorf("bad path %q: want /<drive>:/...", u)
	}
	cleaned := filepath.Clean(filepath.FromSlash(rest))
	if len(cleaned) <= 3 { // "C:\"
		return "", fmt.Errorf("missing file path")
	}
	return cleaned, nil
}

// hasDrivePrefix reports whether s starts with a drive letter and colon
// followed by a separator, e.g. "C:/".
func hasDrivePrefix(s string) bool {
	if len(s) < 3 || s[1] != ':' || (s[2] != '/' && s[2] != '\\') {
		return false
	}
	c := s[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
