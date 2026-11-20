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
	"strconv"
	"strings"
	"testing"
)

func TestAction_PDFDownload(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "report.md")
	if err := os.WriteFile(md, []byte("# Report\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var converted []string
	s := &Server{
		CacheDir: filepath.Join(dir, "cache"), Roots: []string{dir},
		ConvertPDF: func(ctx context.Context, in, out string) error {
			converted = append(converted, in)
			return os.WriteFile(out, []byte("%PDF-1.4 fake"), 0o644)
		},
	}
	ts := startTestServer(t, s)

	u := ts.URL + ChromePDFPath + "?path=" + url.QueryEscape(md)
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/pdf") {
		t.Errorf("Content-Type %q", got)
	}
	if disp := resp.Header.Get("Content-Disposition"); !strings.Contains(disp, "report.pdf") {
		t.Errorf("Content-Disposition %q", disp)
	}
	if !strings.HasPrefix(string(body), "%PDF-1.4") {
		t.Errorf("body %q", body)
	}
	if len(converted) != 1 || converted[0] != md {
		t.Errorf("ConvertPDF got %v", converted)
	}

	// Second GET is a cache hit.
	resp2, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second status %d", resp2.StatusCode)
	}
	if len(converted) != 1 {
		t.Fatalf("expected cache hit, ConvertPDF calls=%d", len(converted))
	}
}

// TestAction_PDFFromAnEarlierViewerIsRerendered: every viewer up to
// 🎯T68 stamped its cache "v5", and those viewers let a Mermaid diagram
// draw an image file from anywhere on disk into the PDF (and, before
// 🎯T66, let the engine read what the HTML named). A PDF they cached is
// keyed by the document's mtime and size only, so after an upgrade the
// same unchanged document would keep getting the old PDF for up to
// CacheMaxAge. It must be rendered again.
func TestAction_PDFFromAnEarlierViewerIsRerendered(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "report.md")
	if err := os.WriteFile(md, []byte("# Report\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(md)
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cached := filepath.Join(cacheDir, cacheName(md, ".pdf"))
	if err := os.WriteFile(cached, []byte("%PDF-1.4 drawn by an earlier viewer"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := strconv.FormatInt(info.ModTime().UnixNano(), 10) + " " + strconv.FormatInt(info.Size(), 10) + " v5\n"
	if err := os.WriteFile(stampPath(cached), []byte(stamp), 0o644); err != nil {
		t.Fatal(err)
	}
	converted := 0
	s := &Server{
		CacheDir: cacheDir, Roots: []string{dir},
		ConvertPDF: func(ctx context.Context, in, out string) error {
			converted++
			return os.WriteFile(out, []byte("%PDF-1.4 fresh"), 0o644)
		},
	}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + ChromePDFPath + "?path=" + url.QueryEscape(md))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if converted != 1 || string(body) != "%PDF-1.4 fresh" {
		t.Fatalf("served %q with %d render(s); want the earlier viewer's PDF rendered again", body, converted)
	}
}

func TestAction_ClipboardPOST(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got string
	s := &Server{
		CacheDir: filepath.Join(dir, "cache"), Roots: []string{dir},
		RenderFile: stubHTML,
		WriteClipboard: func(html string) error {
			got = html
			return nil
		},
	}
	ts := startTestServer(t, s)

	resp, err := http.Post(ts.URL+ChromeClipboardPath+"?path="+url.QueryEscape(md), "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(got, "cache test body") {
		t.Fatalf("clipboard HTML missing convert body:\n%s", got)
	}
	if strings.Contains(got, "vellum-toolbar") || strings.Contains(got, "data-vellum=") {
		t.Fatalf("clipboard must be chrome-free:\n%s", got)
	}
}

func TestAction_ClipboardRejectsGET(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + ChromeClipboardPath + "?path=/tmp/x.md")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", resp.StatusCode)
	}
}

func TestAction_RevealPOST(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var revealed string
	s := &Server{
		CacheDir: filepath.Join(dir, "cache"), Roots: []string{dir},
		Reveal: func(p string) error {
			revealed = p
			return nil
		},
	}
	ts := startTestServer(t, s)

	resp, err := http.Post(ts.URL+ChromeRevealPath+"?path="+url.QueryEscape(md), "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if revealed != md {
		t.Fatalf("revealed %q, want %q", revealed, md)
	}
}

func TestAction_RevealRejectsGET(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + ChromeRevealPath + "?path=/tmp/x.md")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", resp.StatusCode)
	}
}

func TestAction_MissingPath(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + ChromePDFPath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
}

func TestAction_Favicon(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	for _, path := range []string{ChromeFaviconPath, FaviconICOPath} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status %d", path, resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "image/svg+xml") {
			t.Fatalf("%s Content-Type %q", path, got)
		}
		if !strings.Contains(string(body), `viewBox="0 0 32 32"`) || !strings.Contains(string(body), "#f3e6c8") {
			t.Fatalf("%s missing mark:\n%s", path, body)
		}
	}
}

func TestAction_ReservedPrefixNotFilesystem(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + "/_vellum/nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
}

func TestDownloadName(t *testing.T) {
	if got := downloadName("/Users/me/My Report.md", ".pdf"); got != "My Report.pdf" {
		t.Errorf("got %q", got)
	}
	if got := downloadName(`/tmp/weird"name.md`, ".pdf"); strings.Contains(got, `"`) {
		t.Errorf("quote survived: %q", got)
	}
}
