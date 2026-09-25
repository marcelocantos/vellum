// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func watchWSURL(tsURL, absPath string) string {
	return "ws" + strings.TrimPrefix(tsURL, "http") + ChromeWatchPath + "?path=" + url.QueryEscape(absPath)
}

func dialWatch(t *testing.T, origin, absPath string) (*websocket.Conn, context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	conn, _, err := websocket.Dial(ctx, watchWSURL(origin, absPath), nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.CloseNow()
		cancel()
	})
	return conn, ctx, cancel
}

func readWatchText(t *testing.T, ctx context.Context, conn *websocket.Conn) string {
	t.Helper()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("message type %v", typ)
	}
	return strings.TrimSpace(string(data))
}

func TestWatch_RejectsNonMarkdown(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + ChromeWatchPath + "?path=" + url.QueryEscape("/tmp/x.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
}

func TestWatch_RejectsMissingPath(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + ChromeWatchPath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
}

func TestWatch_RejectsMissingFile(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + ChromeWatchPath + "?path=" + url.QueryEscape("/tmp/vellum-no-such.md"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
}

func TestWatch_RejectsPOST(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	resp, err := http.Post(ts.URL+ChromeWatchPath+"?path=/tmp/x.md", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", resp.StatusCode)
	}
}

func TestWatch_InitialStampThenQuietThenChange(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		CacheDir:       filepath.Join(dir, "cache"),
		WatchInterval:  25 * time.Millisecond,
		WatchHeartbeat: time.Hour,
	}
	ts := startTestServer(t, s)
	conn, ctx, _ := dialWatch(t, ts.URL, md)

	first := readWatchText(t, ctx, conn)
	if !strings.HasPrefix(first, "stamp ") {
		t.Fatalf("first message %q", first)
	}
	info, err := os.Stat(md)
	if err != nil {
		t.Fatal(err)
	}
	if first != "stamp "+sourceStamp(info) {
		t.Fatalf("stamp %q, want source %q", first, sourceStamp(info))
	}

	got := make(chan string, 1)
	errc := make(chan error, 1)
	go func() {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			errc <- err
			return
		}
		_ = typ
		got <- strings.TrimSpace(string(data))
	}()
	select {
	case msg := <-got:
		t.Fatalf("unexpected message while source unchanged: %q", msg)
	case err := <-errc:
		t.Fatal(err)
	case <-time.After(80 * time.Millisecond):
	}

	if err := os.WriteFile(md, []byte("# Doc\n\nchanged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var second string
	select {
	case second = <-got:
	case err := <-errc:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stamp change")
	}
	if !strings.HasPrefix(second, "stamp ") {
		t.Fatalf("second message %q", second)
	}
	if second == first {
		t.Fatalf("stamp did not change: %q", second)
	}
	info2, err := os.Stat(md)
	if err != nil {
		t.Fatal(err)
	}
	if second != "stamp "+sourceStamp(info2) {
		t.Fatalf("stamp %q, want %q", second, sourceStamp(info2))
	}
}

func TestWatch_ErrorOnDeletedSource(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		CacheDir:       filepath.Join(dir, "cache"),
		WatchInterval:  20 * time.Millisecond,
		WatchHeartbeat: time.Hour,
	}
	ts := startTestServer(t, s)
	conn, ctx, _ := dialWatch(t, ts.URL, md)
	if msg := readWatchText(t, ctx, conn); !strings.HasPrefix(msg, "stamp ") {
		t.Fatalf("first %q", msg)
	}
	if err := os.Remove(md); err != nil {
		t.Fatal(err)
	}
	msg := readWatchText(t, ctx, conn)
	if !strings.HasPrefix(msg, "error ") {
		t.Fatalf("want error, got %q", msg)
	}
	got := make(chan string, 1)
	errc := make(chan error, 1)
	go func() {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			errc <- err
			return
		}
		_ = typ
		got <- strings.TrimSpace(string(data))
	}()
	select {
	case extra := <-got:
		if strings.HasPrefix(extra, "error ") {
			t.Fatalf("error replayed: %q", extra)
		}
	case err := <-errc:
		t.Fatal(err)
	case <-time.After(60 * time.Millisecond):
	}
}

func TestWatch_HeartbeatPing(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		CacheDir:       filepath.Join(dir, "cache"),
		WatchInterval:  time.Hour,
		WatchHeartbeat: 30 * time.Millisecond,
	}
	ts := startTestServer(t, s)
	conn, ctx, _ := dialWatch(t, ts.URL, md)
	if msg := readWatchText(t, ctx, conn); !strings.HasPrefix(msg, "stamp ") {
		t.Fatalf("first %q", msg)
	}
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	deadline := time.Now().Add(250 * time.Millisecond)
	for s.WatchPingCount.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
	}
	if s.WatchPingCount.Load() < 1 {
		t.Fatalf("WatchPingCount=%d, want at least 1", s.WatchPingCount.Load())
	}
}

func TestInjectChrome_IncludesWatchScript(t *testing.T) {
	in := `<!DOCTYPE html><html><head><title>T</title></head><body><h1 id="hello">Hello</h1></body></html>`
	out := injectChrome(in, "/docs/note.md")
	if !strings.Contains(out, "/_vellum/watch") {
		t.Fatalf("missing watch URL:\n%s", out)
	}
	if !strings.Contains(out, "startWatch()") {
		t.Fatalf("missing startWatch:\n%s", out)
	}
	if !strings.Contains(out, "restoreScroll()") {
		t.Fatalf("missing restoreScroll:\n%s", out)
	}
	if !strings.Contains(out, "restoreFocus(") {
		t.Fatalf("missing restoreFocus:\n%s", out)
	}
	if !strings.Contains(out, "vellum-scroll:") {
		t.Fatalf("missing scroll key:\n%s", out)
	}
}

func TestWatch_HTTPNotUpgradeStillRejectsBodyRead(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache")}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + ChromeWatchPath + "?path=" + url.QueryEscape(md))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK && resp.Header.Get("Upgrade") == "" {
		t.Fatalf("plain GET should not look like a successful watch page: %s", body)
	}
}
