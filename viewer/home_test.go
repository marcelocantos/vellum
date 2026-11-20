// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHome_ListsTreeAndRecent(t *testing.T) {
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs")
	nested := filepath.Join(docs, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(docs, ".secret"), 0o755); err != nil {
		t.Fatal(err)
	}
	alpha := filepath.Join(docs, "alpha.md")
	beta := filepath.Join(nested, "beta.md")
	if err := os.WriteFile(alpha, []byte("# Alpha title\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(beta, []byte("# Beta title\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, ".secret", "nope.md"), []byte("# Hidden\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cache := filepath.Join(dir, "cache")
	s := &Server{CacheDir: cache}
	ts := startTestServer(t, s)

	if body := getBody(t, ts.URL+"/"); strings.Contains(body, "Alpha title") {
		t.Fatalf("homepage listed a file before any view:\n%s", body)
	}

	getBody(t, ts.URL+pathURL(alpha))
	getBody(t, ts.URL+pathURL(beta))
	body := getBody(t, ts.URL+"/")
	recent, files := splitHome(t, body)
	if i, j := strings.Index(recent, "Beta title"), strings.Index(recent, "Alpha title"); i < 0 || j < 0 || i > j {
		t.Fatalf("recent order beta then alpha, got indexes %d %d\n%s", i, j, recent)
	}
	if !strings.Contains(recent, pathURL(beta)) || !strings.Contains(recent, pathURL(alpha)) {
		t.Fatalf("recent links missing:\n%s", recent)
	}
	if !strings.Contains(files, pathURL(alpha)) || !strings.Contains(files, pathURL(beta)) {
		t.Fatalf("tree links missing:\n%s", files)
	}
	if strings.Contains(body, "note.txt") || strings.Contains(body, "nope.md") || strings.Contains(body, "Hidden") {
		t.Fatalf("homepage listed a non-markdown or hidden file:\n%s", body)
	}

	getBody(t, ts.URL+pathURL(alpha))
	ts2 := startTestServer(t, &Server{CacheDir: cache})
	recent, _ = splitHome(t, getBody(t, ts2.URL+"/"))
	if i, j := strings.Index(recent, "Alpha title"), strings.Index(recent, "Beta title"); i < 0 || j < 0 || i > j {
		t.Fatalf("persisted recent order alpha then beta, got indexes %d %d\n%s", i, j, recent)
	}
}

func TestHome_IsReadOnlyAndLoopback(t *testing.T) {
	dir := t.TempDir()
	s := &Server{CacheDir: filepath.Join(dir, "cache"), Roots: []string{dir}}
	handler := s.Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.example"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("foreign host status %d, want 403", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:18742"
	req.Header.Set("Origin", "null")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("origin null status %d, want 403", w.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.Host = "127.0.0.1:18742"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / status %d, want 405", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "cache", "mru.json")); err == nil {
		t.Fatal("POST / wrote the recent list")
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:18742"
	req.Header.Set("Sec-Fetch-Site", "none")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("loopback GET / status %d, want 200", w.Code)
	}
	if strings.Contains(w.Body.String(), "<form") || strings.Contains(w.Body.String(), ChromeTaskTogglePath) {
		t.Fatalf("homepage offers a write:\n%s", w.Body.String())
	}
}

func getBody(t *testing.T, u string) string {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status %d: %s", u, resp.StatusCode, body)
	}
	return string(body)
}

func splitHome(t *testing.T, body string) (recent, files string) {
	t.Helper()
	const recentMark = `id="recent"`
	const filesMark = `id="files"`
	ri := strings.Index(body, recentMark)
	fi := strings.Index(body, filesMark)
	if ri < 0 || fi < 0 || ri > fi {
		t.Fatalf("homepage missing recent and files sections:\n%s", body)
	}
	return body[ri:fi], body[fi:]
}
