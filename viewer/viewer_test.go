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

	// Second view must reuse the same path (cache hit). Content is not
	// re-rendered; mtime may advance (LRU touch for size eviction).
	size1, _ := os.Stat(p1)
	p2, err := View(ctx, md, opts)
	if err != nil {
		t.Fatalf("second View: %v", err)
	}
	if p1 != p2 {
		t.Errorf("cache miss: %s vs %s", p1, p2)
	}
	size2, _ := os.Stat(p2)
	if size1.Size() != size2.Size() {
		t.Errorf("cache file size changed on hit: %d → %d", size1.Size(), size2.Size())
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

func TestPruneCache_AgeExpiry(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.html")
	old := filepath.Join(dir, "old.html")
	fresh := filepath.Join(dir, "fresh.html")
	for _, p := range []string{keep, old, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	// old: 8 days ago; fresh + keep: now
	_ = os.Chtimes(old, now.Add(-8*24*time.Hour), now.Add(-8*24*time.Hour))
	_ = os.Chtimes(fresh, now, now)
	_ = os.Chtimes(keep, now, now)

	if err := pruneCache(dir, keep, -1 /* no size cap */, 7*24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("expected old entry removed, stat=%v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh entry should remain: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("keep entry should remain: %v", err)
	}
}

func TestPruneCache_SizeCapDropsOldest(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	// Three 10-byte files; cap at 25 bytes → one oldest must go.
	// Names encode order for debugging.
	files := []struct {
		name string
		age  time.Duration
	}{
		{"a-oldest.html", 3 * time.Hour},
		{"b-mid.html", 2 * time.Hour},
		{"c-newest.html", 0},
	}
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := os.WriteFile(p, []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(-f.age)
		_ = os.Chtimes(p, mt, mt)
	}
	keep := filepath.Join(dir, "c-newest.html")
	if err := pruneCache(dir, keep, 25, -1 /* no age */, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a-oldest.html")); !os.IsNotExist(err) {
		t.Error("expected oldest entry removed under size cap")
	}
	if _, err := os.Stat(filepath.Join(dir, "b-mid.html")); err != nil {
		t.Errorf("mid entry should remain: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("keep entry should remain: %v", err)
	}
}

func TestPruneCache_NeverDeletesKeepEvenIfOverCap(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.html")
	// Single 100-byte file with 10-byte cap — still kept.
	if err := os.WriteFile(keep, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := pruneCache(dir, keep, 10, -1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("keep must survive even when alone over cap: %v", err)
	}
}

func TestView_ExpiredCacheIsMiss(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# body once\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fixed clock for deterministic age.
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	clock := base
	opts := &ViewOptions{
		Format:   FormatHTML,
		CacheDir: cache,
		MaxAge:   7 * 24 * time.Hour,
		MaxBytes: -1,
		Now:      func() time.Time { return clock },
		Open:     func(string) error { return nil },
	}
	ctx := context.Background()
	p1, err := View(ctx, md, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Age the cache file past MaxAge without changing source mtime.
	old := clock.Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(p1, old, old); err != nil {
		t.Fatal(err)
	}
	// Advance wall clock so age check fires; re-render should rewrite p1.
	clock = base.Add(time.Hour)
	infoBefore, _ := os.Stat(p1)
	p2, err := View(ctx, md, opts)
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		// Same source mtime → same cache path; content re-written in place.
		t.Errorf("path changed unexpectedly: %s → %s", p1, p2)
	}
	infoAfter, _ := os.Stat(p2)
	if !infoAfter.ModTime().After(infoBefore.ModTime()) {
		t.Error("expired entry should have been re-rendered (mtime advanced)")
	}
}

func TestView_SizeCapDuringView(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-seed a large old file that must be evicted.
	junk := filepath.Join(cache, "deadbeef-oldjunk.html")
	if err := os.WriteFile(junk, make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(junk, old, old)

	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# tiny\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := View(context.Background(), md, &ViewOptions{
		Format:   FormatHTML,
		CacheDir: cache,
		MaxBytes: 150, // junk alone is 200 → must go after new render
		MaxAge:   -1,
		Open:     func(string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(junk); !os.IsNotExist(err) {
		t.Error("size cap should have removed the large old entry during View")
	}
}
