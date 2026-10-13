// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"path/filepath"
	"testing"
)

func TestWindowsURLPathToFS(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"/C:/docs/a.md", filepath.Clean(filepath.FromSlash("C:/docs/a.md")), true},
		{"/c:/x/../y/b.md", filepath.Clean(filepath.FromSlash("c:/y/b.md")), true},
		{"/C:/", "", false},
		{"/docs/a.md", "", false},
		{"/C:a.md", "", false},
		{"/1:/a.md", "", false},
	} {
		got, err := windowsURLPathToFS(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("windowsURLPathToFS(%q) = %q, %v; want %q, ok=%v", tc.in, got, err, tc.want, tc.ok)
		}
	}
}
