// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package viewer

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
)

// macOS resolves more spellings of any directory, and none is a symlink,
// so EvalSymlinks leaves each as written: /.nofollow/<path> and
// /.vol/<device>/<inode>. The string parent of such a spelling is not
// the directory's parent (filepath.Dir of /.nofollow/Volumes is
// /.nofollow, and /.vol/<device> is not a directory at all), so the
// directly-under-a-root check and the device-id check both answered for
// another path. Fable read of the port, 2026-11-27: opening
// /.nofollow/Volumes/x.md granted /Volumes and every volume in it, and
// /.vol/<device>/2/x.md granted that volume. A third spelling,
// /.resolve/<flags>/<path>, grants nothing: EvalSymlinks cannot lstat
// /.resolve/<flags>, so rootFor fails.

// kernelSpellings returns dir under each of those spellings.
func kernelSpellings(t *testing.T, dir string) map[string]string {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("%s: no Stat_t", dir)
	}
	out := map[string]string{
		"nofollow": "/.nofollow" + dir,
		"volfs":    fmt.Sprintf("/.vol/%d/%d", st.Dev, st.Ino),
	}
	for name, spelled := range out {
		if !sameDir(t, spelled, dir) {
			t.Fatalf("%s spelling %s is not the same directory as %s", name, spelled, dir)
		}
	}
	return out
}

// mountedVolumeTop returns the top of a mounted volume that the other
// wide-folder rules do not answer for: two or more levels below /, not
// sticky, not the Data volume, on another device than its parent. The
// system's own volumes come first, so the test does not depend on an
// external drive being attached.
func mountedVolumeTop(t *testing.T) string {
	t.Helper()
	for _, holder := range []string{"/System/Volumes", "/Volumes"} {
		entries, err := os.ReadDir(holder)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			vol := filepath.Join(holder, name)
			info, err := os.Stat(vol)
			if err != nil || info.Mode()&os.ModeSticky != 0 || isDataVolumeRoot(vol) {
				continue
			}
			if deviceOf(t, vol) != deviceOf(t, holder) {
				return vol
			}
		}
	}
	t.Fatal("no mounted volume under /System/Volumes or /Volumes")
	return ""
}

// homeOutside sets HOME to a temp folder and fails when dir holds it,
// since the holds-home rule would then answer before the rule under test.
func homeOutside(t *testing.T, dirs ...string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	for _, dir := range dirs {
		if rel, err := filepath.Rel(dir, home); err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			t.Fatalf("HOME %s is inside %s; holds-home would answer first", home, dir)
		}
	}
}

// assertNotServedAfterOpening opens an absent document in each folder as
// the user and then asks, as a page, for another absent name in it: 403
// when the grant was the document alone, 404 when the folder was granted.
func assertNotServedAfterOpening(t *testing.T, dirs []string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts := startTestServer(t, &Server{CacheDir: filepath.Join(base, "cache")})
	for _, dir := range dirs {
		doc := filepath.Join(dir, "vellum-absent-notes.md")
		if code := userOpen(t, ts.URL, doc); code == http.StatusOK {
			t.Fatalf("opening absent %s returned 200", doc)
		}
		asked := filepath.Join(dir, "vellum-absent-sentinel.md")
		if code, _ := pageRequest(t, http.MethodGet, ts.URL+pathURL(asked)); code != http.StatusForbidden {
			t.Errorf("after opening %s, %s was HTTP %d, want 403", doc, asked, code)
		}
	}
}

// TestRootFor_KernelSpellingOfTopLevelFolderGrantsOnlyTheDocument: a
// folder directly under / is that folder under every spelling. /Volumes
// holds every mounted volume; /usr and /Library are there so the rule is
// not pinned to one name.
func TestRootFor_KernelSpellingOfTopLevelFolderGrantsOnlyTheDocument(t *testing.T) {
	tops := []string{"/Volumes", "/usr", "/Library"}
	homeOutside(t, tops...)
	var spelled []string
	for _, top := range tops {
		for name, dir := range kernelSpellings(t, top) {
			t.Run(name+" "+top, func(t *testing.T) {
				assertDocumentAlone(t, filepath.Join(dir, "vellum-absent-notes.md"))
			})
			spelled = append(spelled, dir)
		}
		resolve := "/.resolve/1" + top
		if !sameDir(t, resolve, top) {
			t.Fatalf("%s is not the same directory as %s", resolve, top)
		}
		if got, err := rootFor(filepath.Join(resolve, "vellum-absent-notes.md")); err == nil {
			t.Errorf("rootFor through %s = %+v, want an error", resolve, got)
		}
		spelled = append(spelled, resolve)
	}
	sort.Strings(spelled)
	assertNotServedAfterOpening(t, spelled)
}

// TestRootFor_KernelSpellingOfRootGrantsOnlyTheDocument: / itself, with
// HOME somewhere the holds-home walk still reaches it, and with no HOME
// at all, where only the root's own identity can answer.
func TestRootFor_KernelSpellingOfRootGrantsOnlyTheDocument(t *testing.T) {
	for _, home := range []string{t.TempDir(), ""} {
		t.Setenv("HOME", home)
		for name, dir := range kernelSpellings(t, "/") {
			t.Run(fmt.Sprintf("%s home=%q", name, home), func(t *testing.T) {
				assertDocumentAlone(t, filepath.Join(dir, "vellum-absent-notes.md"))
			})
		}
	}
}

// TestRootFor_KernelSpellingOfVolumeTopGrantsOnlyTheDocument: a volume's
// top named by device and inode has no parent path to compare devices
// with; its parent directory is found by "..".
func TestRootFor_KernelSpellingOfVolumeTopGrantsOnlyTheDocument(t *testing.T) {
	vol := mountedVolumeTop(t)
	homeOutside(t, vol)
	var spelled []string
	for name, dir := range kernelSpellings(t, vol) {
		t.Run(name, func(t *testing.T) {
			assertDocumentAlone(t, filepath.Join(dir, "vellum-absent-notes.md"))
		})
		spelled = append(spelled, dir)
	}
	sort.Strings(spelled)
	assertNotServedAfterOpening(t, spelled)
}

// TestRootFor_FolderDirectlyInTheDataVolumeGrantsOnlyTheDocument: a
// folder directly in /System/Volumes/Data is directly under a root. Its
// parent by ".." is the Data volume for a folder only that volume has
// (usr) and / for one the system volume firmlinks (Library), so both are
// here. Neither holds HOME, is sticky, or is another device.
func TestRootFor_FolderDirectlyInTheDataVolumeGrantsOnlyTheDocument(t *testing.T) {
	dirs := []string{dataVolumeRoot + "/usr", dataVolumeRoot + "/Library"}
	homeOutside(t, dirs...)
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSticky != 0 || deviceOf(t, dir) != deviceOf(t, dataVolumeRoot) {
			t.Fatalf("%s is sticky or a volume top; another rule would answer first", dir)
		}
		assertDocumentAlone(t, filepath.Join(dir, "vellum-absent-notes.md"))
	}
	assertNotServedAfterOpening(t, dirs)
}
