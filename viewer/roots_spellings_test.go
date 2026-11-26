// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package viewer

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// The home check used to compare path strings. On a case-insensitive
// volume another spelling of the same directory is not a string prefix
// of home, and a firmlink (not a symlink) is a second path EvalSymlinks
// does not fold. Fable read, integrator 36, vellum section of
// FABLE-VERDICT.md: each spelling below granted the whole folder, and a
// page request for a file in it was then HTTP 200.

func TestRootFor_CaseVariantHomeGrantsOnlyTheDocument(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "Users", "me")
	mustMkdir(t, home)
	t.Setenv("HOME", home)
	secret := filepath.Join(home, "secret.md")
	mustWrite(t, secret, "# "+secretBytes+"\n")

	lower := filepath.Join(base, "users", "me")
	upper := filepath.Join(base, "USERS", "ME")
	if _, err := os.Stat(lower); err != nil {
		t.Skip("case-sensitive volume")
	}
	if !sameDir(t, lower, home) || !sameDir(t, upper, home) {
		t.Fatal("case variant is not the same directory as home")
	}
	parent := filepath.Join(base, "USERS")
	if !sameDir(t, parent, filepath.Dir(home)) {
		t.Fatal("case variant is not the folder holding home")
	}

	for _, doc := range []string{
		filepath.Join(lower, "notes.md"),
		filepath.Join(upper, "notes.md"),
		filepath.Join(parent, "notes.md"),
	} {
		assertDocumentAlone(t, doc)
	}

	// A folder inside home, named through the alias, is still that folder.
	notes := filepath.Join(home, "notes")
	mustMkdir(t, notes)
	got, err := rootFor(filepath.Join(lower, "notes", "a.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got.File || !sameDir(t, got.Path, notes) {
		t.Errorf("rootFor(alias of a folder inside home) = %+v, want %s", got, notes)
	}

	s := &Server{CacheDir: filepath.Join(base, "cache")}
	ts := startTestServer(t, s)
	userOpen(t, ts.URL, filepath.Join(lower, "notes.md"))
	if code, body := pageRequest(t, http.MethodGet, ts.URL+pathURL(filepath.Join(lower, "secret.md"))); code == http.StatusOK || strings.Contains(body, secretBytes) {
		t.Fatalf("opening an absent document through a case spelling of home served home: HTTP %d", code)
	}
	userOpen(t, ts.URL, filepath.Join(parent, "notes.md"))
	if code, body := pageRequest(t, http.MethodGet, ts.URL+pathURL(filepath.Join(upper, "secret.md"))); code == http.StatusOK || strings.Contains(body, secretBytes) {
		t.Fatalf("opening an absent document in the case spelling of the folder holding home served home: HTTP %d", code)
	}
}

func TestRoots_StoredCaseVariantHomeIsNotHonoured(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "Users", "me")
	mustMkdir(t, home)
	t.Setenv("HOME", home)
	secret := filepath.Join(home, "secret.md")
	mustWrite(t, secret, "# "+secretBytes+"\n")
	alias := filepath.Join(base, "users", "me")
	if _, err := os.Stat(alias); err != nil {
		t.Skip("case-sensitive volume")
	}

	cache := filepath.Join(base, "cache")
	mustMkdir(t, cache)
	b, err := json.Marshal([]servedRoot{{Path: alias}})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(cache, rootsFileName), string(b))

	s := &Server{CacheDir: cache}
	ts := startTestServer(t, s)
	if code, body := pageRequest(t, http.MethodGet, ts.URL+pathURL(filepath.Join(alias, "secret.md"))); code == http.StatusOK || strings.Contains(body, secretBytes) {
		t.Fatalf("stored grant of a case spelling of home still served it: HTTP %d", code)
	}
}

func TestRootFor_VerdictSpellingsGrantOnlyTheDocument(t *testing.T) {
	home := resolvedHome()
	if home == "" {
		t.Fatal("no home directory")
	}
	me := filepath.Base(home)
	users := filepath.Dir(home)
	const absent = "vellum-i36-absent.md"
	cases := []struct {
		name string
		doc  string
		dir  string
		same string
	}{
		{"lower home", filepath.Join("/users", me, absent), filepath.Join("/users", me), home},
		{"upper home", filepath.Join("/USERS", strings.ToUpper(me), absent), filepath.Join("/USERS", strings.ToUpper(me)), home},
		{"lower folder holding home", filepath.Join("/users", absent), "/users", users},
		{"upper folder holding home", filepath.Join("/USERS", absent), "/USERS", users},
		{"data home", "/System/Volumes/Data" + home + "/" + absent, "/System/Volumes/Data" + home, home},
		{"data every home", "/System/Volumes/Data/Users/" + absent, "/System/Volumes/Data/Users", users},
		{"data volume", "/System/Volumes/Data/" + absent, "/System/Volumes/Data", "/System/Volumes/Data"},
		{"data volume case", "/system/volumes/data/" + absent, "/system/volumes/data", "/System/Volumes/Data"},
		{"data every home case", "/system/volumes/data/users/" + absent, "/system/volumes/data/users", users},
		{"data home case", "/system/volumes/data" + strings.ToLower(home) + "/" + absent, "/system/volumes/data" + strings.ToLower(home), home},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !sameDir(t, c.dir, c.same) {
				t.Fatalf("%s (%s) is not the same directory as %s", c.name, c.dir, c.same)
			}
			assertDocumentAlone(t, c.doc)
		})
	}

	// End to end, without reading a real file: an absent name inside the
	// granted folder is 404 when the folder was granted and 403 when the
	// grant was the absent document alone.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(base, "cache")}
	ts := startTestServer(t, s)
	sentinel := "vellum-i36-absent-sentinel.md"
	for _, c := range cases {
		if code := userOpen(t, ts.URL, c.doc); code == http.StatusOK {
			t.Fatalf("opening absent %s returned 200", c.doc)
		}
		asked := filepath.Join(c.dir, sentinel)
		code, _ := pageRequest(t, http.MethodGet, ts.URL+pathURL(asked))
		if code != http.StatusForbidden {
			t.Fatalf("after opening %s, %s was HTTP %d, want 403", c.doc, asked, code)
		}
	}
}

// TestRootFor_DirectoryDirectlyUnderRootGrantsOnlyTheDocument is the
// "directly under a root" rule on a folder no other rule refuses.
// TestRootFor_TopLevelFolderGrantsOnlyTheDocument walks up from the temp
// directory, so its folder holds HOME and the holds-home rule answers
// first. /usr does not hold HOME, is not sticky, and is the same device
// as /. It is refused by name and by identity (isUnderRoot); each of
// those alone is pinned by TestTooWideToServe_RootAndFolderDirectlyUnderItByName
// and TestRootFor_KernelSpellingOfTopLevelFolderGrantsOnlyTheDocument.
func TestRootFor_DirectoryDirectlyUnderRootGrantsOnlyTheDocument(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	const dir = "/usr"
	if rel, err := filepath.Rel(dir, home); err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		t.Fatalf("HOME %s is inside %s; holds-home would answer first", home, dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSticky != 0 {
		t.Fatalf("%s is sticky; the sticky check would answer first", dir)
	}
	if deviceOf(t, dir) != deviceOf(t, filepath.Dir(dir)) {
		t.Fatalf("%s is a volume top; the device check would answer first", dir)
	}
	parent := filepath.Dir(dir)
	if filepath.Dir(parent) != parent {
		t.Fatalf("%s is not directly under a filesystem root", dir)
	}
	assertDocumentAlone(t, filepath.Join(dir, "vellum-absent-notes.md"))
}

// TestRootFor_VolumeTopGrantsOnlyTheDocument pins the device-id line.
// A mounted volume's top (mountedVolumeTop: /System/Volumes/Hardware on
// this Mac, so an external drive need not be attached) is two levels
// below /, does not hold HOME, and is not sticky. Its device differs
// from its parent's. Removing st.Dev != pst.Dev grants the volume.
func TestRootFor_VolumeTopGrantsOnlyTheDocument(t *testing.T) {
	vol := mountedVolumeTop(t)
	homeOutside(t, vol)
	parent := filepath.Dir(vol)
	if parent == vol || filepath.Dir(parent) == parent {
		t.Fatalf("%s is a root or directly under one; the depth check would answer first", vol)
	}
	assertDocumentAlone(t, filepath.Join(vol, "vellum-absent-notes.md"))
}

func assertDocumentAlone(t *testing.T, doc string) {
	t.Helper()
	got, err := rootFor(doc)
	if err != nil {
		t.Fatalf("rootFor(%s): %v", doc, err)
	}
	if !got.File || got.Path != doc {
		t.Errorf("rootFor(%s) = %+v, want the document alone", doc, got)
	}
}

func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ia, err := os.Stat(a)
	if err != nil {
		t.Fatalf("stat %s: %v", a, err)
	}
	ib, err := os.Stat(b)
	if err != nil {
		t.Fatalf("stat %s: %v", b, err)
	}
	return os.SameFile(ia, ib)
}

func deviceOf(t *testing.T, p string) uint64 {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat %s: %v", p, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("%s: no Stat_t", p)
	}
	return uint64(st.Dev)
}
