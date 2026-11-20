// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

// Symlinks need Developer Mode or elevation on Windows; this file is the
// symlink half of the served-roots tests.

package viewer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/convert"
)

const secretBytes = "SECRET-BYTES"

// rootsFixture is a folder tree with a served docs/ folder and secrets
// beside it:
//
//	root/secret.txt, root/secret.md      outside docs/
//	root/docs/note.md, pic.png, sub/inner.md
//	root/docs/alias.md  -> ../secret.txt   (symlink out)
//	root/docs/alias.txt -> ../secret.txt
//	root/docs/up        -> ..              (directory symlink out)
//	root/docs/inner.md  -> sub/inner.md    (symlink that stays inside)
type rootsFixture struct {
	root, docs, note, secret, secretMD string
}

func newRootsFixture(t *testing.T) rootsFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := rootsFixture{
		root:     root,
		docs:     filepath.Join(root, "docs"),
		secret:   filepath.Join(root, "secret.txt"),
		secretMD: filepath.Join(root, "secret.md"),
	}
	f.note = filepath.Join(f.docs, "note.md")
	mustMkdir(t, filepath.Join(f.docs, "sub"))
	mustWrite(t, f.secret, secretBytes)
	mustWrite(t, f.secretMD, "# "+secretBytes+"\n\n- [ ] "+secretBytes+"\n")
	mustWrite(t, f.note, "# Note\n\n- [ ] task\n\n![pic](pic.png)\n")
	mustWrite(t, filepath.Join(f.docs, "pic.png"), "PNG")
	mustWrite(t, filepath.Join(f.docs, "sub", "inner.md"), "# Inner\n")
	mustSymlink(t, f.secret, filepath.Join(f.docs, "alias.md"))
	mustSymlink(t, f.secret, filepath.Join(f.docs, "alias.txt"))
	mustSymlink(t, "..", filepath.Join(f.docs, "up"))
	mustSymlink(t, filepath.Join("sub", "inner.md"), filepath.Join(f.docs, "inner.md"))
	return f
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// pageRequest makes a request the way a viewer page's fetch or link
// does: Sec-Fetch-Site same-origin. It does not follow redirects.
func pageRequest(t *testing.T, method, u string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(secFetchSiteHeader, secFetchSiteSameOrigin)
	req.Header.Set(secFetchModeHeader, "cors")
	req.Header.Set(secFetchDestHeader, "empty")
	if method == http.MethodPut || method == http.MethodPost {
		req.Header.Set(StampHeader, "0 0")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// userOpen is the browser opening a document from outside any page.
func userOpen(t *testing.T, ts string, path string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, ts+pathURL(path), nil)
	req.Header.Set(secFetchSiteHeader, secFetchSiteNone)
	req.Header.Set(secFetchModeHeader, secFetchModeNavigate)
	req.Header.Set(secFetchDestHeader, secFetchDestDocument)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestServed_EveryRouteRefusesPathsOutsideItsRoots(t *testing.T) {
	f := newRootsFixture(t)
	var revealed, clipped, converted, wrote []string
	s := &Server{
		CacheDir:       filepath.Join(t.TempDir(), "cache"),
		Reveal:         func(p string) error { revealed = append(revealed, p); return nil },
		WriteClipboard: func(string) error { clipped = append(clipped, "x"); return nil },
		ConvertPDF: func(_ context.Context, in, out string) error {
			converted = append(converted, in)
			return os.WriteFile(out, []byte("%PDF"), 0o644)
		},
		RenderFragments: func(_ context.Context, frags []convert.Fragment) (map[string]convert.FragmentResult, error) {
			return map[string]convert.FragmentResult{}, nil
		},
		WriteFile: func(p string, data []byte) error { wrote = append(wrote, p); return os.WriteFile(p, data, 0o644) },
	}
	ts := startTestServer(t, s)
	// The user opens docs/note.md: docs/ is served from now on.
	if code := userOpen(t, ts.URL, f.note); code != http.StatusOK {
		t.Fatalf("opening the note: %d", code)
	}

	alias := filepath.Join(f.docs, "alias.md")
	viaUp := filepath.Join(f.docs, "up", "secret.md")
	outside := []string{f.secretMD, alias, viaUp, filepath.Join(f.root, "missing.md")}
	escaped := func(p string) string { return url.QueryEscape(p) }
	for _, p := range outside {
		q := "?path=" + escaped(p)
		for _, c := range []struct{ method, u string }{
			{http.MethodGet, ts.URL + pathURL(p)},
			{http.MethodGet, ts.URL + ChromeEditPath + q},
			{http.MethodPut, ts.URL + ChromeEditPath + q},
			{http.MethodPost, ts.URL + ChromeTaskTogglePath + q + "&index=0&line=3&checked=0"},
			{http.MethodGet, ts.URL + ChromeWatchPath + q},
			{http.MethodGet, ts.URL + ChromeFragmentPath + q + "&id=0123456789abcdef"},
			{http.MethodGet, ts.URL + ChromePDFPath + q},
			{http.MethodPost, ts.URL + ChromeClipboardPath + q},
			{http.MethodPost, ts.URL + ChromeRevealPath + q},
		} {
			code, body := pageRequest(t, c.method, c.u)
			if code != http.StatusForbidden {
				t.Errorf("%s %s: status %d, want 403: %.120s", c.method, c.u, code, body)
			}
			if strings.Contains(body, secretBytes) {
				t.Errorf("%s %s returned the secret", c.method, c.u)
			}
		}
	}
	// Raw files: absolute, through "..", encoded "..", encoded separators,
	// through a symlinked file or directory.
	for _, u := range []string{
		pathURL(f.secret),
		pathURL(f.docs) + "/../secret.txt",
		pathURL(f.docs) + "/%2e%2e/secret.txt",
		pathURL(f.docs) + "/%252e%252e/secret.txt",
		pathURL(f.docs) + "%2F..%2Fsecret.txt",
		pathURL(f.docs) + "/sub/../../secret.txt",
		pathURL(filepath.Join(f.docs, "alias.txt")),
		pathURL(filepath.Join(f.docs, "up", "secret.txt")),
		pathURL(filepath.Join(f.docs, "up", "docs", "..", "secret.txt")),
	} {
		code, body := pageRequest(t, http.MethodGet, ts.URL+u)
		if strings.Contains(body, secretBytes) || code == http.StatusOK {
			t.Errorf("GET %s: status %d: %.80s", u, code, body)
		}
	}
	if len(revealed)+len(clipped)+len(converted)+len(wrote) > 0 {
		t.Errorf("an outside path reached an action: reveal %v clipboard %v pdf %v write %v", revealed, clipped, converted, wrote)
	}
	got, _ := os.ReadFile(f.secretMD)
	if !strings.Contains(string(got), "- [ ] "+secretBytes) {
		t.Errorf("outside file was written: %q", got)
	}

	// Inside docs/ everything still works, symlinks that stay inside too.
	for _, p := range []string{f.note, filepath.Join(f.docs, "pic.png"), filepath.Join(f.docs, "sub", "inner.md"), filepath.Join(f.docs, "inner.md")} {
		if code, body := pageRequest(t, http.MethodGet, ts.URL+pathURL(p)); code != http.StatusOK {
			t.Errorf("GET %s inside docs/: status %d: %.120s", p, code, body)
		}
	}
	if code, _ := pageRequest(t, http.MethodGet, ts.URL+ChromeEditPath+"?path="+escaped(f.note)); code != http.StatusOK {
		t.Errorf("edit GET of the note: %d", code)
	}
	if code, _ := pageRequest(t, http.MethodPost, ts.URL+ChromeRevealPath+"?path="+escaped(f.note)); code != http.StatusOK || len(revealed) != 1 {
		t.Errorf("reveal of the note: %d (%v)", code, revealed)
	}
}

// The report's reproduction: a document in docs/ cannot read ../secret.txt
// by a same-origin fetch, and the edit route does not follow a .md
// symlink out of docs/.
func TestServed_ViewedDocumentCannotReadOutsideItsDirectory(t *testing.T) {
	f := newRootsFixture(t)
	s := &Server{CacheDir: filepath.Join(t.TempDir(), "cache")}
	ts := startTestServer(t, s)
	page, err := http.Get(ts.URL + pathURL(f.note))
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	if page.StatusCode != http.StatusOK {
		t.Fatalf("note: %d", page.StatusCode)
	}
	if code, body := pageRequest(t, http.MethodGet, ts.URL+pathURL(f.secret)); code == http.StatusOK || strings.Contains(body, secretBytes) {
		t.Fatalf("same-origin fetch read a file outside the document directory: %d", code)
	}
	edit, err := http.Get(ts.URL + ChromeEditPath + "?path=" + url.QueryEscape(filepath.Join(f.docs, "alias.md")))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(edit.Body)
	edit.Body.Close()
	if strings.Contains(string(b), secretBytes) {
		t.Fatal("edit GET followed a .md symlink out of the folder")
	}
}

// Only a request from outside any page opens a folder. Opening a symlink
// whose target is outside its own folder is refused even then.
func TestServed_GrantsComeOnlyFromTheUser(t *testing.T) {
	f := newRootsFixture(t)
	s := &Server{CacheDir: filepath.Join(t.TempDir(), "cache")}
	ts := startTestServer(t, s)
	// A page's link (same-origin) and a foreign page's link (cross-site
	// navigation) do not open a folder.
	if code, _ := pageRequest(t, http.MethodGet, ts.URL+pathURL(f.secretMD)); code != http.StatusForbidden {
		t.Fatalf("same-origin open of an unserved document: %d, want 403", code)
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+pathURL(f.secretMD), nil)
	req.Header.Set(secFetchSiteHeader, "cross-site")
	req.Header.Set(secFetchModeHeader, secFetchModeNavigate)
	req.Header.Set(secFetchDestHeader, secFetchDestDocument)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site navigation to an unserved document: %d, want 403", resp.StatusCode)
	}
	// The user opening docs/alias.md serves docs/, not the target's folder.
	if code := userOpen(t, ts.URL, filepath.Join(f.docs, "alias.md")); code != http.StatusForbidden {
		t.Fatalf("user open of a symlink pointing out of its folder: %d, want 403", code)
	}
	if code, _ := pageRequest(t, http.MethodGet, ts.URL+pathURL(f.secret)); code != http.StatusForbidden {
		t.Fatalf("after opening the alias, root/ is served: %d", code)
	}
	// The user can still open any document directly.
	if code := userOpen(t, ts.URL, f.secretMD); code != http.StatusOK {
		t.Fatalf("user open of root/secret.md: %d", code)
	}
}

// Grants persist in the cache directory, so an open tab survives a
// restart of the server.
func TestServed_GrantSurvivesRestart(t *testing.T) {
	f := newRootsFixture(t)
	cache := filepath.Join(t.TempDir(), "cache")
	ts := startTestServer(t, &Server{CacheDir: cache})
	if code := userOpen(t, ts.URL, f.note); code != http.StatusOK {
		t.Fatalf("open: %d", code)
	}
	ts2 := startTestServer(t, &Server{CacheDir: cache})
	if code, _ := pageRequest(t, http.MethodGet, ts2.URL+ChromeEditPath+"?path="+url.QueryEscape(f.note)); code != http.StatusOK {
		t.Fatalf("after restart, edit GET of an opened note: %d", code)
	}
	if code, _ := pageRequest(t, http.MethodGet, ts2.URL+pathURL(f.secret)); code != http.StatusForbidden {
		t.Fatalf("after restart, outside file: %d", code)
	}
	b, err := os.ReadFile(filepath.Join(cache, rootsFileName))
	if err != nil {
		t.Fatal(err)
	}
	var saved []servedRoot
	if err := json.Unmarshal(b, &saved); err != nil || len(saved) != 1 || saved[0].Path != f.docs {
		t.Fatalf("roots.json %s (err %v), want docs/ only", b, err)
	}
}

// The path changes on disk after the check and before the open: a file
// or a directory on the way is swapped for a symlink that leads out.
func TestServed_PathSwappedBetweenCheckAndOpen(t *testing.T) {
	for _, tc := range []struct {
		name string
		swap func(f rootsFixture) string // returns the requested path
	}{
		{"file becomes symlink", func(f rootsFixture) string { return filepath.Join(f.docs, "swap.txt") }},
		{"directory becomes symlink", func(f rootsFixture) string { return filepath.Join(f.docs, "dir", "secret.txt") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRootsFixture(t)
			mustWrite(t, filepath.Join(f.docs, "swap.txt"), "inside")
			mustMkdir(t, filepath.Join(f.docs, "dir"))
			mustWrite(t, filepath.Join(f.docs, "dir", "secret.txt"), "inside")
			target := tc.swap(f)
			swapped := false
			s := &Server{CacheDir: filepath.Join(t.TempDir(), "cache"), Roots: []string{f.docs}}
			s.beforeOpen = func(p string) {
				if p != target || swapped {
					return
				}
				swapped = true
				victim := target
				dest := f.secret
				if filepath.Base(filepath.Dir(target)) == "dir" {
					victim, dest = filepath.Dir(target), f.root
				}
				if err := os.RemoveAll(victim); err != nil {
					t.Error(err)
				}
				if err := os.Symlink(dest, victim); err != nil {
					t.Error(err)
				}
			}
			ts := startTestServer(t, s)
			code, body := pageRequest(t, http.MethodGet, ts.URL+pathURL(target))
			if !swapped {
				t.Fatal("hook did not run")
			}
			if strings.Contains(body, secretBytes) || code == http.StatusOK {
				t.Fatalf("served after the swap: %d %q", code, body)
			}
			if code != http.StatusForbidden {
				t.Fatalf("status %d, want 403", code)
			}
		})
	}
}

// A document directly in the home folder serves itself only, and a git
// work tree is served whole from any document inside it.
func TestServed_HomeDocumentAndGitWorkTree(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	homeDoc := filepath.Join(home, "notes.md")
	mustWrite(t, homeDoc, "# Notes\n")
	mustWrite(t, filepath.Join(home, "private.md"), "# "+secretBytes+"\n")
	repo := filepath.Join(home, "work", "repo")
	mustMkdir(t, filepath.Join(repo, ".git"))
	mustMkdir(t, filepath.Join(repo, "docs"))
	mustWrite(t, filepath.Join(repo, "docs", "a.md"), "[readme](../README.md)\n")
	mustWrite(t, filepath.Join(repo, "README.md"), "# Readme\n")
	mustWrite(t, filepath.Join(home, "work", "other.md"), "# "+secretBytes+"\n")

	cache := filepath.Join(home, "cache")
	s := &Server{CacheDir: cache}
	ts := startTestServer(t, s)
	if code := userOpen(t, ts.URL, homeDoc); code != http.StatusOK {
		t.Fatalf("open home doc: %d", code)
	}
	if code, _ := pageRequest(t, http.MethodGet, ts.URL+pathURL(homeDoc)); code != http.StatusOK {
		t.Fatalf("home doc after opening it: %d", code)
	}
	if code, _ := pageRequest(t, http.MethodGet, ts.URL+pathURL(filepath.Join(home, "private.md"))); code != http.StatusForbidden {
		t.Fatalf("a home document opened another home file: %d", code)
	}
	if code := userOpen(t, ts.URL, filepath.Join(repo, "docs", "a.md")); code != http.StatusOK {
		t.Fatalf("open repo doc: %d", code)
	}
	if code, _ := pageRequest(t, http.MethodGet, ts.URL+pathURL(filepath.Join(repo, "README.md"))); code != http.StatusOK {
		t.Fatalf("../README.md from docs/ in the same work tree: %d", code)
	}
	if code, _ := pageRequest(t, http.MethodGet, ts.URL+pathURL(filepath.Join(home, "work", "other.md"))); code != http.StatusForbidden {
		t.Fatalf("a file above the work tree: %d", code)
	}
	// The homepage lists the home document but not the home folder's tree.
	_, _, page := getText(t, ts.URL+"/")
	if strings.Contains(page, "private.md") || strings.Contains(page, secretBytes) {
		t.Fatalf("homepage lists an unserved home file:\n%s", page)
	}
	if !strings.Contains(page, "README.md") {
		t.Fatalf("homepage lost the served work tree:\n%s", page)
	}
}

// The recent list may hold paths from before this rule (or whose grant
// has aged out); the homepage shows only what is served now.
func TestHome_ListsOnlyServedDocuments(t *testing.T) {
	f := newRootsFixture(t)
	cache := filepath.Join(t.TempDir(), "cache")
	mustMkdir(t, cache)
	stale, _ := json.Marshal([]recentDoc{{Path: f.secretMD, Title: "stale " + secretBytes}, {Path: f.note, Title: "Note"}})
	mustWrite(t, filepath.Join(cache, homeRecentName), string(stale))
	s := &Server{CacheDir: cache, Roots: []string{f.docs}}
	ts := startTestServer(t, s)
	_, _, page := getText(t, ts.URL+"/")
	if strings.Contains(page, secretBytes) || strings.Contains(page, "secret.md") {
		t.Fatalf("homepage lists an unserved document:\n%s", page)
	}
	if !strings.Contains(page, pathURL(f.note)) {
		t.Fatalf("homepage lost the served note:\n%s", page)
	}
}
