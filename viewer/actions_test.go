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
)

func TestAction_PDFDownload(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "report.md")
	if err := os.WriteFile(md, []byte("# Report\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var converted []string
	s := &Server{
		CacheDir: filepath.Join(dir, "cache"),
		ConvertPDF: func(ctx context.Context, in, out string) error {
			converted = append(converted, in)
			return os.WriteFile(out, []byte("%PDF-1.4 fake"), 0o644)
		},
	}
	ts := startTestServer(t, s)
	viewFile(t, ts.URL, md)

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

func TestAction_ClipboardPOST(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got string
	s := &Server{
		CacheDir:   filepath.Join(dir, "cache"),
		RenderFile: stubHTML,
		WriteClipboard: func(html string) error {
			got = html
			return nil
		},
	}
	ts := startTestServer(t, s)
	viewFile(t, ts.URL, md)

	resp := chromePOST(t, ts.URL, ChromeClipboardPath, md, ts.URL)
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
		CacheDir: filepath.Join(dir, "cache"),
		Reveal: func(p string) error {
			revealed = p
			return nil
		},
	}
	ts := startTestServer(t, s)
	viewFile(t, ts.URL, md)

	resp := chromePOST(t, ts.URL, ChromeRevealPath, md, ts.URL)
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

func chromePOST(t *testing.T, tsURL, actionPath, file, origin string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, tsURL+actionPath+"?path="+url.QueryEscape(file), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func viewFile(t *testing.T, tsURL, path string) {
	t.Helper()
	resp, err := http.Get(ViewURL(tsURL, path))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("view %s: status %d: %s", path, resp.StatusCode, body)
	}
}

func TestAction_ChromePathConfinedToViewedMarkdown(t *testing.T) {
	dir := t.TempDir()
	viewed := filepath.Join(dir, "doc.md")
	secret := filepath.Join(dir, "secret.md")
	if err := os.WriteFile(viewed, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("# Secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var converted, revealed []string
	var clipped int
	s := &Server{
		CacheDir: filepath.Join(dir, "cache"),
		ConvertPDF: func(ctx context.Context, in, out string) error {
			converted = append(converted, in)
			return os.WriteFile(out, []byte("%PDF-1.4 fake"), 0o644)
		},
		WriteClipboard: func(html string) error {
			clipped++
			return nil
		},
		Reveal: func(p string) error {
			revealed = append(revealed, p)
			return nil
		},
	}
	ts := startTestServer(t, s)

	pdfSecret, err := http.Get(ts.URL + ChromePDFPath + "?path=" + url.QueryEscape(secret))
	if err != nil {
		t.Fatal(err)
	}
	pdfSecret.Body.Close()
	if pdfSecret.StatusCode != http.StatusNotFound {
		t.Fatalf("PDF of never-viewed file: status %d, want 404", pdfSecret.StatusCode)
	}

	clipSecret := chromePOST(t, ts.URL, ChromeClipboardPath, secret, ts.URL)
	clipSecret.Body.Close()
	if clipSecret.StatusCode != http.StatusNotFound {
		t.Fatalf("clipboard of never-viewed file: status %d, want 404", clipSecret.StatusCode)
	}

	revSecret := chromePOST(t, ts.URL, ChromeRevealPath, secret, ts.URL)
	revSecret.Body.Close()
	if revSecret.StatusCode != http.StatusNotFound {
		t.Fatalf("reveal of never-viewed file: status %d, want 404", revSecret.StatusCode)
	}
	if clipped != 0 || len(revealed) != 0 || len(converted) != 0 {
		t.Fatalf("chrome acted before view: clip=%d reveal=%v convert=%v", clipped, revealed, converted)
	}

	viewFile(t, ts.URL, viewed)

	pdfOK, err := http.Get(ts.URL + ChromePDFPath + "?path=" + url.QueryEscape(viewed))
	if err != nil {
		t.Fatal(err)
	}
	defer pdfOK.Body.Close()
	if pdfOK.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(pdfOK.Body)
		t.Fatalf("PDF of viewed file: status %d: %s", pdfOK.StatusCode, body)
	}

	clipOK := chromePOST(t, ts.URL, ChromeClipboardPath, viewed, ts.URL)
	clipOK.Body.Close()
	if clipOK.StatusCode != http.StatusOK {
		t.Fatalf("clipboard of viewed file: status %d, want 200", clipOK.StatusCode)
	}

	revOK := chromePOST(t, ts.URL, ChromeRevealPath, viewed, ts.URL)
	revOK.Body.Close()
	if revOK.StatusCode != http.StatusOK {
		t.Fatalf("reveal of viewed file: status %d, want 200", revOK.StatusCode)
	}

	pdfSecret2, err := http.Get(ts.URL + ChromePDFPath + "?path=" + url.QueryEscape(secret))
	if err != nil {
		t.Fatal(err)
	}
	pdfSecret2.Body.Close()
	if pdfSecret2.StatusCode != http.StatusNotFound {
		t.Fatalf("PDF of sibling never-viewed file after view: status %d, want 404", pdfSecret2.StatusCode)
	}
	clipSecret2 := chromePOST(t, ts.URL, ChromeClipboardPath, secret, ts.URL)
	clipSecret2.Body.Close()
	if clipSecret2.StatusCode != http.StatusNotFound {
		t.Fatalf("clipboard of sibling never-viewed file: status %d, want 404", clipSecret2.StatusCode)
	}
}

func TestAction_ChromePOSTRequiresOrigin(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var clipped, revealed int
	s := &Server{
		CacheDir: filepath.Join(dir, "cache"),
		WriteClipboard: func(html string) error {
			clipped++
			return nil
		},
		Reveal: func(p string) error {
			revealed++
			return nil
		},
	}
	ts := startTestServer(t, s)
	viewFile(t, ts.URL, md)

	for _, action := range []string{ChromeClipboardPath, ChromeRevealPath} {
		missing := chromePOST(t, ts.URL, action, md, "")
		body, _ := io.ReadAll(missing.Body)
		missing.Body.Close()
		if missing.StatusCode != http.StatusForbidden {
			t.Fatalf("%s without Origin: status %d body %q, want 403", action, missing.StatusCode, body)
		}

		evil := chromePOST(t, ts.URL, action, md, "https://evil.example")
		body, _ = io.ReadAll(evil.Body)
		evil.Body.Close()
		if evil.StatusCode != http.StatusForbidden {
			t.Fatalf("%s with foreign Origin: status %d body %q, want 403", action, evil.StatusCode, body)
		}
	}
	if clipped != 0 || revealed != 0 {
		t.Fatalf("untrusted POST mutated state: clip=%d reveal=%d", clipped, revealed)
	}

	clipOK := chromePOST(t, ts.URL, ChromeClipboardPath, md, ts.URL)
	clipOK.Body.Close()
	if clipOK.StatusCode != http.StatusOK {
		t.Fatalf("clipboard with view Origin: status %d, want 200", clipOK.StatusCode)
	}
	revOK := chromePOST(t, ts.URL, ChromeRevealPath, md, ts.URL)
	revOK.Body.Close()
	if revOK.StatusCode != http.StatusOK {
		t.Fatalf("reveal with view Origin: status %d, want 200", revOK.StatusCode)
	}
	if clipped != 1 || revealed != 1 {
		t.Fatalf("trusted POST: clip=%d reveal=%d, want 1/1", clipped, revealed)
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
