// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestView_HTMLCacheHit(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Hello\n\ncache test body\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var opened []string
	opts := &ViewOptions{
		Format:   FormatHTML,
		CacheDir: cache,
		Open: func(p string) error {
			opened = append(opened, p)
			return nil
		},
	}

	ctx := context.Background()
	p1, err := View(ctx, md, opts)
	if err != nil {
		t.Fatalf("first View: %v", err)
	}
	if !strings.HasSuffix(p1, ".html") {
		t.Errorf("expected .html cache path, got %s", p1)
	}
	body, err := os.ReadFile(p1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "cache test body") {
		t.Errorf("rendered HTML missing body text:\n%s", body)
	}
	if !strings.Contains(string(body), `<base href="`) {
		t.Errorf("rendered HTML missing <base href> for relative assets")
	}

	// Touch nothing; second view must reuse the same path (cache hit).
	info1, _ := os.Stat(p1)
	p2, err := View(ctx, md, opts)
	if err != nil {
		t.Fatalf("second View: %v", err)
	}
	if p1 != p2 {
		t.Errorf("cache miss: %s vs %s", p1, p2)
	}
	info2, _ := os.Stat(p2)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Errorf("cache file was rewritten on hit")
	}
	if len(opened) != 2 {
		t.Errorf("open called %d times, want 2", len(opened))
	}
}

func TestView_CacheInvalidatesOnMtime(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := &ViewOptions{
		Format:   FormatHTML,
		CacheDir: cache,
		Open:     func(string) error { return nil },
	}
	ctx := context.Background()
	p1, err := View(ctx, md, opts)
	if err != nil {
		t.Fatalf("first View: %v", err)
	}

	// Bump mtime and content.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(md, []byte("# v2 updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Ensure mtime advances even on coarse filesystems.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(md, future, future); err != nil {
		t.Fatal(err)
	}

	p2, err := View(ctx, md, opts)
	if err != nil {
		t.Fatalf("second View: %v", err)
	}
	if p1 == p2 {
		t.Errorf("expected new cache path after mtime change; both %s", p1)
	}
	body, _ := os.ReadFile(p2)
	if !strings.Contains(string(body), "v2 updated") {
		t.Errorf("new cache missing updated content:\n%s", body)
	}
}

func TestView_MissingFile(t *testing.T) {
	_, err := View(context.Background(), filepath.Join(t.TempDir(), "nope.md"), &ViewOptions{
		CacheDir: t.TempDir(),
		Open:     func(string) error { return nil },
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCacheNameStable(t *testing.T) {
	mt := time.Unix(1_700_000_000, 123)
	a := cacheName("/abs/path.md", mt, ".html")
	b := cacheName("/abs/path.md", mt, ".html")
	if a != b {
		t.Errorf("unstable cache name: %s vs %s", a, b)
	}
	c := cacheName("/other.md", mt, ".html")
	if a == c {
		t.Error("different paths produced same cache name")
	}
}
