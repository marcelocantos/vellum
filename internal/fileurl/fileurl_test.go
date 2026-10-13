// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fileurl

import (
	"path/filepath"
	"testing"
)

func TestFromPathUnixStyle(t *testing.T) {
	if got := FromPath("/tmp/a b/c.css"); got != "file:///tmp/a%20b/c.css" {
		t.Fatalf("got %q", got)
	}
}

func TestFromPathDriveLetter(t *testing.T) {
	got := FromPath(filepath.FromSlash("C:/Users/m/x y/k.css"))
	if got != "file:///C:/Users/m/x%20y/k.css" {
		t.Fatalf("got %q", got)
	}
}

func TestToPathRoundTrip(t *testing.T) {
	p, ok := ToPath("file:///C:/Users/m/x%20y/k.css")
	if !ok || p != filepath.FromSlash("C:/Users/m/x y/k.css") {
		t.Fatalf("got %q, %v", p, ok)
	}
	p, ok = ToPath("file:///tmp/a%20b")
	if !ok || p != filepath.FromSlash("/tmp/a b") {
		t.Fatalf("got %q, %v", p, ok)
	}
	if _, ok := ToPath("https://x"); ok {
		t.Fatal("https: want ok=false")
	}
}
