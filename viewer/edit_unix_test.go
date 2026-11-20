// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

// Symlinks need Developer Mode or elevation on Windows, so this lives
// beside the other platform-scoped tests rather than skipping there.

package viewer

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteServed_FollowsSymlinkAndKeepsIt(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Target\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(md, link); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: t.TempDir(), Roots: []string{dir}}
	if err := s.writeServed(link, []byte("# Via link\n")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("link replaced by a regular file (err %v)", err)
	}
	got, err := os.ReadFile(md)
	if err != nil || string(got) != "# Via link\n" {
		t.Fatalf("target %q (err %v)", got, err)
	}
	if fi, _ := os.Stat(md); fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v, want 0640 preserved", fi.Mode().Perm())
	}
}
