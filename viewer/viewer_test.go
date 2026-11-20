// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/vellum/embed"
)

func startTestServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	if s.CacheDir == "" {
		s.CacheDir = t.TempDir()
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestServer_OneConvertPerGET(t *testing.T) {
	dir := t.TempDir()
	linked := filepath.Join(dir, "other.md")
	if err := os.WriteFile(linked, []byte("# Other\n\nlinked body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.md")
	if err := os.WriteFile(main, []byte("# Main\n\nSee [other](other.md).\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Server{CacheDir: filepath.Join(dir, "cache")}
	ts := startTestServer(t, s)

	if s.ConvertCount.Load() != 0 {
		t.Fatalf("ConvertCount before GET: %d", s.ConvertCount.Load())
	}

	resp, err := http.Get(ViewURL(ts.URL, main))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if s.ConvertCount.Load() != 1 {
		t.Fatalf("ConvertCount after one GET: %d (eager crawl?)", s.ConvertCount.Load())
	}
	html := string(body)
	if !strings.Contains(html, "See") {
		t.Errorf("missing body:\n%s", html)
	}
	wantLink := ViewURL(ts.URL, linked)
	if !strings.Contains(html, wantLink) {
		t.Errorf("expected rewritten link %q in:\n%s", wantLink, html)
	}
	if strings.Contains(html, `href="other.md"`) {
		t.Errorf("relative .md href should have been rewritten:\n%s", html)
	}

	// Second GET must cache-hit (no second convert) while linked file stays unconverted.
	resp2, err := http.Get(ViewURL(ts.URL, main))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if s.ConvertCount.Load() != 1 {
		t.Fatalf("ConvertCount after cache hit: %d", s.ConvertCount.Load())
	}
}

func TestServer_ReloadReconvertsOnMtime(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache")}
	ts := startTestServer(t, s)

	get := func() string {
		resp, err := http.Get(ViewURL(ts.URL, md))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	if !strings.Contains(get(), "v1") {
		t.Fatal("expected v1")
	}
	if s.ConvertCount.Load() != 1 {
		t.Fatalf("count=%d", s.ConvertCount.Load())
	}

	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(md, []byte("# v2 updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(md, future, future); err != nil {
		t.Fatal(err)
	}

	body := get()
	if !strings.Contains(body, "v2 updated") {
		t.Errorf("reload missing updated content:\n%s", body)
	}
	if s.ConvertCount.Load() != 2 {
		t.Fatalf("expected re-convert after mtime, count=%d", s.ConvertCount.Load())
	}
}

func TestServer_BindsLoopbackOnly(t *testing.T) {
	s := &Server{Addr: "0.0.0.0:0"}
	err := s.ListenAndServe(context.Background())
	if err == nil {
		t.Fatal("expected non-loopback bind to fail")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Errorf("error should mention loopback: %v", err)
	}
}

func TestServer_Healthz(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + HealthPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestServer_RequestBoundaries(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	asset := filepath.Join(dir, "image.txt")
	for _, path := range []string{md, asset} {
		if err := os.WriteFile(path, []byte("viewer fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{
		CacheDir: filepath.Join(dir, "cache"),
		MCP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	handler := s.Handler()

	// Every registration in Server.Handler, with both branches of the
	// filesystem catch-all and concrete files under each asset prefix.
	routes := []struct {
		name, method, path string
	}{
		{"health", http.MethodGet, HealthPath},
		{"mcp", http.MethodPost, MCPPath},
		{"mcp subtree", http.MethodPost, MCPPath + "/"},
		{"pdf", http.MethodGet, ChromePDFPath},
		{"clipboard", http.MethodPost, ChromeClipboardPath},
		{"reveal", http.MethodPost, ChromeRevealPath},
		{"watch", http.MethodGet, ChromeWatchPath},
		{"task toggle", http.MethodPost, ChromeTaskTogglePath},
		{"edit read", http.MethodGet, ChromeEditPath},
		{"edit write", http.MethodPut, ChromeEditPath},
		{"editor bundle", http.MethodGet, ChromeEditorPrefix + EditorEntry},
		{"fragment", http.MethodGet, ChromeFragmentPath},
		{"favicon", http.MethodGet, ChromeFaviconPath},
		{"file manager icon", http.MethodGet, ChromeFileManagerIconPath},
		{"katex", http.MethodGet, ChromeKaTeXPrefix + embed.KaTeXCSSName},
		{"favicon ico", http.MethodGet, FaviconICOPath},
		{"markdown", http.MethodGet, pathURL(md)},
		{"static file", http.MethodGet, pathURL(asset)},
	}
	const localHost = "127.0.0.1:18742"
	const foreignHost = "foreign.example:18742"
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			requests := []struct {
				name, method, host, origin, site string
				healthStatus                     int
			}{
				{"foreign host", route.method, foreignHost, "", "", http.StatusOK},
				{"foreign origin", route.method, localHost, "https://foreign.example", "", http.StatusOK},
				{"cross-origin POST", http.MethodPost, localHost, "https://foreign.example", "", http.StatusMethodNotAllowed},
				{"cross-site POST", http.MethodPost, localHost, "", "cross-site", http.StatusMethodNotAllowed},
			}
			for _, tc := range requests {
				t.Run(tc.name, func(t *testing.T) {
					req := httptest.NewRequest(tc.method, route.path, nil)
					req.Host = tc.host
					if tc.origin != "" {
						req.Header.Set("Origin", tc.origin)
					}
					if tc.site != "" {
						req.Header.Set("Sec-Fetch-Site", tc.site)
					}
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, req)
					want := http.StatusForbidden
					if route.path == HealthPath {
						want = tc.healthStatus
					}
					if w.Code != want {
						t.Errorf("status %d, want %d", w.Code, want)
					}
				})
			}
		})
	}
}

func TestServer_AllowsLocalRequests(t *testing.T) {
	s := &Server{MCP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}
	handler := s.Handler()
	for _, host := range []string{"127.0.0.1:18742", "127.0.0.1:18744", "127.0.0.2", "localhost:18742", "LOCALHOST", "[::1]:18742", "[::1]"} {
		t.Run(host, func(t *testing.T) {
			// A client without browser metadata can still POST to MCP.
			req := httptest.NewRequest(http.MethodPost, MCPPath, nil)
			req.Host = host
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusNoContent {
				t.Errorf("MCP POST status %d, want 204", w.Code)
			}

			req = httptest.NewRequest(http.MethodGet, ChromeFaviconPath, nil)
			req.Host = host
			req.Header.Set("Origin", "http://"+host)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("same-origin GET status %d, want 200", w.Code)
			}
		})
	}
}

// TestServer_CrossSiteRule pins the request rule in localRequestAllowed
// case by case: which requests caused by another site are refused, and the
// one kind that is let through (a link followed to a page).
func TestServer_CrossSiteRule(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	asset := filepath.Join(dir, "image.txt")
	for _, path := range []string{md, asset} {
		if err := os.WriteFile(path, []byte("viewer fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{
		CacheDir: filepath.Join(dir, "cache"),
		MCP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	handler := s.Handler()

	const (
		localHost   = "127.0.0.1:18742"
		localOrigin = "http://" + localHost
		foreign     = "https://foreign.example"
		ok          = http.StatusOK
		forbidden   = http.StatusForbidden
	)
	page, static := pathURL(md), pathURL(asset)
	cases := []struct {
		name               string
		method, path, host string
		origin             string // "" omits the header
		site, mode, dest   string // Sec-Fetch-*; "" omits the header
		want               int
	}{
		// A link on another site followed to a page: the case that used to get 403.
		{"cross-site link to a page", "GET", page, localHost, "", "cross-site", "navigate", "document", ok},
		{"same-site link to a page", "GET", page, localHost, "", "same-site", "navigate", "document", ok},
		{"cross-site link to a static file", "GET", static, localHost, "", "cross-site", "navigate", "document", ok},
		{"cross-site link to the homepage", "GET", "/", localHost, "", "cross-site", "navigate", "document", ok},

		// Cross-site requests that are not top-level GET navigations.
		{"cross-site image load", "GET", static, localHost, "", "cross-site", "no-cors", "image", forbidden},
		{"cross-site script load", "GET", page, localHost, "", "cross-site", "no-cors", "script", forbidden},
		{"cross-site fetch", "GET", page, localHost, foreign, "cross-site", "cors", "empty", forbidden},
		{"same-site fetch", "GET", page, localHost, "", "same-site", "cors", "empty", forbidden},
		{"cross-site iframe", "GET", page, localHost, "", "cross-site", "navigate", "iframe", forbidden},
		{"cross-site frame", "GET", page, localHost, "", "cross-site", "navigate", "frame", forbidden},
		{"cross-site embed", "GET", page, localHost, "", "cross-site", "navigate", "embed", forbidden},
		{"cross-site object", "GET", page, localHost, "", "cross-site", "navigate", "object", forbidden},
		{"cross-site form post", "POST", page, localHost, foreign, "cross-site", "navigate", "document", forbidden},
		{"cross-site post without Origin", "POST", page, localHost, "", "cross-site", "navigate", "document", forbidden},
		{"cross-site HEAD", "HEAD", page, localHost, "", "cross-site", "navigate", "document", forbidden},
		{"cross-site preflight", "OPTIONS", page, localHost, foreign, "cross-site", "cors", "empty", forbidden},
		{"cross-site with no mode or dest", "GET", page, localHost, "", "cross-site", "", "", forbidden},
		{"cross-site navigation with no dest", "GET", page, localHost, "", "cross-site", "navigate", "", forbidden},
		{"cross-site document dest without navigate mode", "GET", page, localHost, "", "cross-site", "no-cors", "document", forbidden},
		{"cross-site document dest with no mode", "GET", page, localHost, "", "cross-site", "", "document", forbidden},
		{"unknown Sec-Fetch-Site value", "GET", page, localHost, "", "elsewhere", "cors", "empty", forbidden},
		{"cross-site link carrying a foreign Origin", "GET", page, localHost, foreign, "cross-site", "navigate", "document", forbidden},

		// State-changing routes refuse cross-site requests of every kind,
		// a top-level GET navigation included.
		{"cross-site link to mcp", "GET", MCPPath, localHost, "", "cross-site", "navigate", "document", forbidden},
		{"cross-site link to mcp subtree", "GET", MCPPath + "/session", localHost, "", "cross-site", "navigate", "document", forbidden},
		{"cross-site link to clipboard", "GET", ChromeClipboardPath, localHost, "", "cross-site", "navigate", "document", forbidden},
		{"cross-site link to reveal", "GET", ChromeRevealPath, localHost, "", "cross-site", "navigate", "document", forbidden},
		{"cross-site link to task toggle", "GET", ChromeTaskTogglePath, localHost, "", "cross-site", "navigate", "document", forbidden},
		{"cross-site link to edit", "GET", ChromeEditPath, localHost, "", "cross-site", "navigate", "document", forbidden},
		{"same-site link to edit", "GET", ChromeEditPath, localHost, "", "same-site", "navigate", "document", forbidden},
		{"cross-site post to mcp", "POST", MCPPath, localHost, foreign, "cross-site", "cors", "empty", forbidden},
		{"cross-site put to edit", "PUT", ChromeEditPath, localHost, foreign, "cross-site", "cors", "empty", forbidden},

		// The viewer's own pages and the browser's address bar.
		{"same-origin fetch of a page", "GET", page, localHost, "", "same-origin", "cors", "empty", ok},
		{"same-origin post to mcp", "POST", MCPPath, localHost, localOrigin, "same-origin", "cors", "empty", http.StatusNoContent},
		{"typed URL or bookmark", "GET", page, localHost, "", "none", "navigate", "document", ok},

		// No fetch metadata: Origin decides.
		{"plain client GET", "GET", page, localHost, "", "", "", "", ok},
		{"plain client post to mcp", "POST", MCPPath, localHost, "", "", "", "", http.StatusNoContent},
		{"own Origin post to mcp", "POST", MCPPath, localHost, localOrigin, "", "", "", http.StatusNoContent},
		{"foreign Origin GET", "GET", page, localHost, foreign, "", "", "", forbidden},
		{"null Origin GET", "GET", page, localHost, "null", "", "", "", forbidden},
		{"foreign Origin post to mcp", "POST", MCPPath, localHost, foreign, "", "", "", forbidden},
		{"own host on another port as Origin", "POST", MCPPath, localHost, "http://127.0.0.1:3000", "", "", "", forbidden},

		// Host is checked on every route, before anything else.
		{"rebinding Host on a link to a page", "GET", page, "foreign.example:18742", "", "cross-site", "navigate", "document", forbidden},
		{"rebinding Host, same-origin metadata", "GET", page, "foreign.example:18742", "", "same-origin", "navigate", "document", forbidden},
		{"rebinding Host, plain client", "GET", static, "foreign.example", "", "", "", "", forbidden},
		{"rebinding Host, typed URL", "GET", "/", "127.0.0.1.foreign.example:18742", "", "none", "navigate", "document", forbidden},

		// The liveness probe answers anyone.
		{"health under a foreign Host", "GET", HealthPath, "foreign.example:18742", "", "", "", "", ok},
		{"health cross-site", "GET", HealthPath, localHost, foreign, "cross-site", "cors", "empty", ok},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Host = tc.host
			for header, value := range map[string]string{
				"Origin":         tc.origin,
				"Sec-Fetch-Site": tc.site,
				"Sec-Fetch-Mode": tc.mode,
				"Sec-Fetch-Dest": tc.dest,
			} {
				if value != "" {
					req.Header.Set(header, value)
				}
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Errorf("status %d, want %d", w.Code, tc.want)
			}
		})
	}
}

func TestServer_MCPPathReserved(t *testing.T) {
	hit := false
	s := &Server{
		CacheDir: t.TempDir(),
		MCP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hit = true
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	ts := startTestServer(t, s)

	resp, err := http.Get(ts.URL + MCPPath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !hit {
		t.Fatal("GET /mcp must hit the MCP handler, not the view filesystem")
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}

	// View routes still work alongside MCP.
	health, err := http.Get(ts.URL + HealthPath)
	if err != nil {
		t.Fatal(err)
	}
	health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Fatalf("healthz status %d", health.StatusCode)
	}
}

func TestServer_MCPPathNotFilesystemWhenUnset(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + MCPPath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unset MCP /mcp status %d, want 404 (reserved, not a file)", resp.StatusCode)
	}
}

func TestView_OpensServerURLNotFile(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Hello\n\ncache test body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache")}
	ts := startTestServer(t, s)

	var opened []string
	u, err := View(context.Background(), md, &ViewOptions{
		Format:           FormatHTML,
		ViewBaseURL:      ts.URL,
		SkipEnsureServer: true,
		Open: func(p string) error {
			opened = append(opened, p)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := ViewURL(ts.URL, md)
	if u != want {
		t.Errorf("View returned %q, want %q", u, want)
	}
	if len(opened) != 1 || opened[0] != want {
		t.Errorf("Open got %v, want [%s]", opened, want)
	}
	if strings.HasPrefix(u, "/") || strings.HasSuffix(u, ".html") {
		t.Errorf("HTML View must open server URL, not cache file: %s", u)
	}
	// View itself must not convert — only the subsequent GET does.
	if s.ConvertCount.Load() != 0 {
		t.Fatalf("View must not eagerly convert; ConvertCount=%d", s.ConvertCount.Load())
	}
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "cache test body") {
		t.Errorf("GET missing body:\n%s", body)
	}
	if !strings.Contains(string(body), "Cache-Control") {
		t.Errorf("missing Cache-Control")
	}
	if s.ConvertCount.Load() != 1 {
		t.Fatalf("after GET ConvertCount=%d", s.ConvertCount.Load())
	}
}

func TestView_MissingFile(t *testing.T) {
	_, err := View(context.Background(), filepath.Join(t.TempDir(), "nope.md"), &ViewOptions{
		ViewBaseURL:      "http://127.0.0.1:9",
		SkipEnsureServer: true,
		Open:             func(string) error { return nil },
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestRewriteMarkdownHrefs(t *testing.T) {
	dir := "/docs/notes"
	src := dir + "/index.md"
	origin := "http://127.0.0.1:18742"
	in := `<p><a href="other.md">o</a> <a href="./sub/x.markdown#frag">x</a> ` +
		`<a href="https://example.com/a.md">ext</a> <a href="img.png">img</a></p>`
	out := rewriteMarkdownHrefs(in, src, origin)
	if !strings.Contains(out, origin+"/docs/notes/other.md") {
		t.Errorf("other.md: %s", out)
	}
	if !strings.Contains(out, origin+"/docs/notes/sub/x.markdown#frag") {
		t.Errorf("x.markdown#frag: %s", out)
	}
	if !strings.Contains(out, `href="https://example.com/a.md"`) {
		t.Errorf("external md must stay: %s", out)
	}
	if !strings.Contains(out, `href="img.png"`) {
		t.Errorf("non-md must stay: %s", out)
	}
}

func TestCacheNameStable(t *testing.T) {
	a := cacheName("/abs/path.md", ".html")
	b := cacheName("/abs/path.md", ".html")
	if a != b {
		t.Errorf("unstable cache name: %s vs %s", a, b)
	}
	if strings.Contains(a, "-") {
		t.Errorf("cache name should not include mtime suffix, got %s", a)
	}
	c := cacheName("/other.md", ".html")
	if a == c {
		t.Error("different paths produced same cache name")
	}
	if cacheName("/abs/path.md", ".pdf") == a {
		t.Error("html and pdf must not share a cache name")
	}
}

func TestCacheNameIndependentOfMtime(t *testing.T) {
	a := cacheName("/docs/note.md", ".html")
	b := cacheName("/docs/note.md", ".html")
	if a != b {
		t.Fatalf("%s vs %s", a, b)
	}
}

func TestPruneCache_AgeExpiry(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.html")
	old := filepath.Join(dir, "old.html")
	fresh := filepath.Join(dir, "fresh.html")
	for _, p := range []string{keep, old, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	_ = os.Chtimes(old, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour))
	_ = os.Chtimes(fresh, now, now)
	_ = os.Chtimes(keep, now, now)

	if err := pruneCache(dir, keep, -1 /* no size cap */, 7*24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("expected old entry removed, stat=%v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh entry should remain: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("keep entry should remain: %v", err)
	}
}

func TestPruneCache_SizeCapDropsOldest(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	files := []struct {
		name string
		age  time.Duration
	}{
		{"a-oldest.html", 3 * time.Hour},
		{"b-mid.html", 2 * time.Hour},
		{"c-newest.html", 0},
	}
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := os.WriteFile(p, []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(-f.age)
		_ = os.Chtimes(p, mt, mt)
	}
	keep := filepath.Join(dir, "c-newest.html")
	if err := pruneCache(dir, keep, 25, -1 /* no age */, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a-oldest.html")); !os.IsNotExist(err) {
		t.Error("expected oldest entry removed under size cap")
	}
	if _, err := os.Stat(filepath.Join(dir, "b-mid.html")); err != nil {
		t.Errorf("mid entry should remain: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("keep entry should remain: %v", err)
	}
}

func TestPruneCache_NeverDeletesKeepEvenIfOverCap(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.html")
	if err := os.WriteFile(keep, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := pruneCache(dir, keep, 10, -1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("keep must survive even when alone over cap: %v", err)
	}
}
