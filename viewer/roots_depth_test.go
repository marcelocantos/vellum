// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"path/filepath"
	"testing"
)

// TestTooWideToServe_RootAndFolderDirectlyUnderItByName pins the string
// check: a filesystem root, and a folder directly under one, are too
// wide by name alone. The folders here do not exist, so nothing can be
// compared by identity; that is a stored grant whose folder is gone, or
// a drive that is not attached. On Windows the string check is the only
// rule for a root (C:\ and C:\Users).
func TestTooWideToServe_RootAndFolderDirectlyUnderItByName(t *testing.T) {
	abs, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.VolumeName(abs) + string(filepath.Separator)
	top := filepath.Join(root, "vellum-absent-top-level-folder")
	for _, dir := range []string{root, top} {
		if !tooWideToServe(dir, "") {
			t.Errorf("tooWideToServe(%s) = false, want true", dir)
		}
	}
	if below := filepath.Join(top, "below"); tooWideToServe(below, "") {
		t.Errorf("tooWideToServe(%s) = true, want false: it is two levels below the root", below)
	}
}

// TestHoldsHome_ByNameWhenNothingCanBeCompared pins holdsHome's string
// check. None of these folders exists, so the identity walk has nothing
// to compare and the names alone decide.
func TestHoldsHome_ByNameWhenNothingCanBeCompared(t *testing.T) {
	abs, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(abs, "absent")
	users := filepath.Join(base, "Users")
	home := filepath.Join(users, "me")
	for _, dir := range []string{home, users, base} {
		if !holdsHome(dir, home) {
			t.Errorf("holdsHome(%s, %s) = false, want true", dir, home)
		}
	}
	for _, dir := range []string{filepath.Join(home, "notes"), filepath.Join(users, "other"), filepath.Join(base, "Use")} {
		if holdsHome(dir, home) {
			t.Errorf("holdsHome(%s, %s) = true, want false", dir, home)
		}
	}
}
