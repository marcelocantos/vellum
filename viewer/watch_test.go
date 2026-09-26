// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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

func TestWatch_MissingFileWaitsUntilItAppears(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "later.md")
	s := &Server{CacheDir: filepath.Join(dir, "cache"), WatchHeartbeat: time.Hour}
	ts := startTestServer(t, s)
	conn, ctx, _ := dialWatch(t, ts.URL, md)

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
		t.Fatalf("unexpected message before the file exists: %q", msg)
	case err := <-errc:
		t.Fatal(err)
	case <-time.After(80 * time.Millisecond):
	}

	if err := os.WriteFile(md, []byte("# Later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var msg string
	select {
	case msg = <-got:
	case err := <-errc:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for the file to appear")
	}
	info, err := os.Stat(md)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "stamp "+sourceStamp(info) {
		t.Fatalf("stamp %q, want %q", msg, sourceStamp(info))
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

func TestWatch_DeleteLeavesConnectionQuiet(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		CacheDir:       filepath.Join(dir, "cache"),
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
	if msg != "delete" {
		t.Fatalf("want delete, got %q", msg)
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
		t.Fatalf("unexpected follow-up %q", extra)
	case err := <-errc:
		t.Fatal(err)
	case <-time.After(80 * time.Millisecond):
	}
}

// readWatchWithin waits up to d for one text message. A timeout leaves the
// read in flight, so it must be the last read on conn.
func readWatchWithin(t *testing.T, ctx context.Context, conn *websocket.Conn, d time.Duration) (string, bool) {
	t.Helper()
	got := make(chan string, 1)
	errc := make(chan error, 1)
	go func() {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			errc <- err
			return
		}
		if typ != websocket.MessageText {
			errc <- fmt.Errorf("message type %v", typ)
			return
		}
		got <- strings.TrimSpace(string(data))
	}()
	select {
	case msg := <-got:
		return msg, true
	case err := <-errc:
		t.Fatal(err)
	case <-time.After(d):
		return "", false
	}
	return "", false
}

func TestWatch_CoalescesRapidWrites(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache"), WatchHeartbeat: time.Hour}
	ts := startTestServer(t, s)
	conn, ctx, _ := dialWatch(t, ts.URL, md)
	if msg := readWatchText(t, ctx, conn); !strings.HasPrefix(msg, "stamp ") {
		t.Fatalf("first %q", msg)
	}

	until := time.Now().Add(40 * time.Millisecond)
	n := 0
	for time.Now().Before(until) {
		n++
		body := []byte("# Doc\n\n" + strconv.Itoa(n) + "\n")
		if err := os.WriteFile(md, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(md)
	if err != nil {
		t.Fatal(err)
	}
	want := "stamp " + sourceStamp(info)
	msg, ok := readWatchWithin(t, ctx, conn, 2*time.Second)
	if !ok {
		t.Fatal("timeout waiting for coalesced stamp")
	}
	if msg != want {
		t.Fatalf("stamp %q, want %q", msg, want)
	}
	if extra, ok := readWatchWithin(t, ctx, conn, 150*time.Millisecond); ok {
		t.Fatalf("extra message %q", extra)
	}
}

func TestWatch_RenameRedirects(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache"), WatchHeartbeat: time.Hour}
	ts := startTestServer(t, s)
	conn, ctx, _ := dialWatch(t, ts.URL, md)
	if msg := readWatchText(t, ctx, conn); !strings.HasPrefix(msg, "stamp ") {
		t.Fatalf("first %q", msg)
	}
	dest := filepath.Join(dir, "moved.md")
	if err := os.Rename(md, dest); err != nil {
		t.Fatal(err)
	}
	msg := readWatchText(t, ctx, conn)
	want := "rename " + pathURL(dest)
	if msg != want {
		if resolved, err := filepath.EvalSymlinks(dest); err == nil && msg == "rename "+pathURL(resolved) {
			return
		}
		t.Fatalf("rename %q, want %q", msg, want)
	}
}

func TestWatch_AtomicReplaceIsStamp(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache"), WatchHeartbeat: time.Hour}
	ts := startTestServer(t, s)
	conn, ctx, _ := dialWatch(t, ts.URL, md)
	if msg := readWatchText(t, ctx, conn); !strings.HasPrefix(msg, "stamp ") {
		t.Fatalf("first %q", msg)
	}
	tmp := filepath.Join(dir, ".doc.md.tmp")
	if err := os.WriteFile(tmp, []byte("# Doc\n\nreplaced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, md); err != nil {
		t.Fatal(err)
	}
	msg := readWatchText(t, ctx, conn)
	info, err := os.Stat(md)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "stamp "+sourceStamp(info) {
		t.Fatalf("message %q, want stamp %s", msg, sourceStamp(info))
	}
}

func TestWatch_MissingParentThenFile(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "a", "b", "doc.md")
	s := &Server{CacheDir: filepath.Join(dir, "cache"), WatchHeartbeat: time.Hour}
	ts := startTestServer(t, s)
	conn, ctx, _ := dialWatch(t, ts.URL, md)
	got := make(chan string, 1)
	errc := make(chan error, 1)
	go func() {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			errc <- err
			return
		}
		if typ != websocket.MessageText {
			errc <- fmt.Errorf("message type %v", typ)
			return
		}
		got <- strings.TrimSpace(string(data))
	}()
	select {
	case msg := <-got:
		t.Fatalf("unexpected message before the file exists: %q", msg)
	case err := <-errc:
		t.Fatal(err)
	case <-time.After(80 * time.Millisecond):
	}
	if err := os.MkdirAll(filepath.Dir(md), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(md, []byte("# Nested\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var msg string
	select {
	case msg = <-got:
	case err := <-errc:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for nested file")
	}
	info, err := os.Stat(md)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "stamp "+sourceStamp(info) {
		t.Fatalf("stamp %q, want %q", msg, sourceStamp(info))
	}
}

func TestWatch_SharedMachine(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache"), WatchHeartbeat: time.Hour}
	ts := startTestServer(t, s)
	a, ctxA, _ := dialWatch(t, ts.URL, md)
	b, ctxB, _ := dialWatch(t, ts.URL, md)
	if msg := readWatchText(t, ctxA, a); !strings.HasPrefix(msg, "stamp ") {
		t.Fatalf("a first %q", msg)
	}
	if msg := readWatchText(t, ctxB, b); !strings.HasPrefix(msg, "stamp ") {
		t.Fatalf("b first %q", msg)
	}
	if err := os.WriteFile(md, []byte("# Doc\n\nboth\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(md)
	if err != nil {
		t.Fatal(err)
	}
	want := "stamp " + sourceStamp(info)
	msgA, ok := readWatchWithin(t, ctxA, a, 2*time.Second)
	if !ok || msgA != want {
		t.Fatalf("a got %q ok=%v, want %q", msgA, ok, want)
	}
	msgB, ok := readWatchWithin(t, ctxB, b, 2*time.Second)
	if !ok || msgB != want {
		t.Fatalf("b got %q ok=%v, want %q", msgB, ok, want)
	}
}

func TestServe_MissingMarkdownWatches(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "missing.md")
	s := &Server{CacheDir: filepath.Join(dir, "cache")}
	ts := startTestServer(t, s)
	resp, err := http.Get(ViewURL(ts.URL, md))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Cache-Control"), "no-store") {
		t.Fatalf("Cache-Control %q", resp.Header.Get("Cache-Control"))
	}
	if !strings.Contains(page, `data-waiting="1"`) {
		t.Fatalf("missing waiting marker:\n%s", page)
	}
	if !strings.Contains(page, "not here yet") || !strings.Contains(page, "startWatch()") {
		t.Fatalf("waiting page missing copy or watch:\n%s", page)
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
