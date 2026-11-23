// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

// Uses the served-roots helpers in roots_test.go, which is Unix-only.

package viewer

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// Opening one document never serves a whole volume, a shared temporary
// folder, the home folder or a folder holding it (render-boundaries
// report, D7). Such a folder grants the document alone, and the search
// for a git work tree stops below it.

// TestRootFor_FolderHoldingHomeGrantsOnlyTheDocument is D7's /Users
// case: the folder is neither home nor a filesystem root, but holds home.
func TestRootFor_FolderHoldingHomeGrantsOnlyTheDocument(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	users := filepath.Join(base, "Users")
	home := filepath.Join(users, "me")
	mustMkdir(t, home)
	t.Setenv("HOME", home)
	mustWrite(t, filepath.Join(home, "secret.md"), "# "+secretBytes+"\n")

	for _, doc := range []string{
		filepath.Join(users, "notes.md"), // absent: a grant needs only the folder
		filepath.Join(base, "notes.md"),  // two levels above home
	} {
		got, err := rootFor(doc)
		if err != nil {
			t.Fatalf("rootFor(%s): %v", doc, err)
		}
		if !got.File || got.Path != doc {
			t.Errorf("rootFor(%s) = %+v, want the document alone", doc, got)
		}
	}

	// End to end: the grant does not open home.
	s := &Server{CacheDir: filepath.Join(base, "cache")}
	ts := startTestServer(t, s)
	userOpen(t, ts.URL, filepath.Join(users, "notes.md"))
	if code, _ := pageRequest(t, http.MethodGet, ts.URL+pathURL(filepath.Join(home, "secret.md"))); code != http.StatusForbidden {
		t.Fatalf("opening a document beside home served home: %d", code)
	}
}

// TestRootFor_TopLevelFolderGrantsOnlyTheDocument is D7's /Volumes and
// /private case: a folder one level below a filesystem root.
func TestRootFor_TopLevelFolderGrantsOnlyTheDocument(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	top, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for filepath.Dir(top) != filepath.Dir(filepath.Dir(top)) {
		top = filepath.Dir(top)
	}
	if filepath.Dir(filepath.Dir(top)) != filepath.Dir(top) {
		t.Fatalf("%s is not one level below a filesystem root", top)
	}
	doc := filepath.Join(top, "vellum-absent-notes.md")
	got, err := rootFor(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !got.File || got.Path != doc {
		t.Errorf("rootFor(%s) = %+v, want the document alone", doc, got)
	}
}

// TestRootFor_SharedTempFolderGrantsOnlyTheDocument is D7's /tmp case:
// a sticky folder (/tmp, /private/tmp, /var/tmp) holds every account's
// and every program's files, and a git marker in one is not a work tree
// to serve.
func TestRootFor_SharedTempFolderGrantsOnlyTheDocument(t *testing.T) {
	if !stickyFoldersAreShared {
		return // no sticky bit on this platform
	}
	t.Setenv("HOME", t.TempDir())
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(base, "tmp")
	mustMkdir(t, shared)
	if err := os.Chmod(shared, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(shared, "notes.md")
	got, err := rootFor(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !got.File || got.Path != doc {
		t.Errorf("rootFor(%s) = %+v, want the document alone", doc, got)
	}

	// A .git planted in the shared folder does not widen a grant below it.
	mustMkdir(t, filepath.Join(shared, ".git"))
	docs := filepath.Join(shared, "proj", "docs")
	mustMkdir(t, docs)
	got, err = rootFor(filepath.Join(docs, "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got.File || got.Path != docs {
		t.Errorf("rootFor(docs/a.md) = %+v, want %s", got, docs)
	}
}

// TestRoots_StoredWideGrantIsNotHonoured: a roots.json an older vellum
// wrote with a folder holding home serves nothing of it.
func TestRoots_StoredWideGrantIsNotHonoured(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	users := filepath.Join(base, "Users")
	home := filepath.Join(users, "me")
	mustMkdir(t, home)
	t.Setenv("HOME", home)
	secret := filepath.Join(home, "secret.md")
	mustWrite(t, secret, "# "+secretBytes+"\n")
	own := filepath.Join(home, "work", "notes")
	mustMkdir(t, own)
	mustWrite(t, filepath.Join(own, "a.md"), "# A\n")

	cache := filepath.Join(base, "cache")
	mustMkdir(t, cache)
	b, err := json.Marshal([]servedRoot{{Path: users}, {Path: own}})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(cache, rootsFileName), string(b))

	s := &Server{CacheDir: cache}
	ts := startTestServer(t, s)
	if code, _ := pageRequest(t, http.MethodGet, ts.URL+pathURL(secret)); code != http.StatusForbidden {
		t.Fatalf("stored grant of a folder holding home still served it: %d", code)
	}
	if code, _ := pageRequest(t, http.MethodGet, ts.URL+pathURL(filepath.Join(own, "a.md"))); code != http.StatusOK {
		t.Fatalf("stored ordinary grant lost: %d", code)
	}
}
