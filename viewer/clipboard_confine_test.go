// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/pandoc"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

// TestClipboard_ConverterCannotReadOutsideOrOpenAFolder covers the
// clipboard route's converter as a reader of the document's images. The
// pasteboard is faked; the fake runs the conversion clipboard.Write runs
// for its RTF when AppKit is unavailable (clipboard/pandoc.go), which is
// the converter that fetches. AppKit's importer fetches nothing
// (clipboard.TestAppKitRichTextFetchesNothing).
//
// The document names an outside image by absolute path and the viewer's
// own origin twice: once for a Markdown path in an outside folder (which
// a request without fetch metadata would grant), once for the image
// wanted from it.
func TestClipboard_ConverterCannotReadOutsideOrOpenAFolder(t *testing.T) {
	testdeps.Need(t, pandoc.Binary)

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	docs := filepath.Join(root, "docs")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{docs, outside} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	secretPNG := filepath.Join(outside, "secret.png")
	if err := os.WriteFile(secretPNG, pngBytes(t, 7, 5, color.RGBA{0, 0, 255, 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(docs, "note.md")

	var rtf []byte
	s := &Server{
		CacheDir: filepath.Join(root, "cache"),
		Roots:    []string{docs},
		WriteClipboard: func(html string) error {
			// The same call clipboard/pandoc.go makes.
			out, _, err := pandoc.HTMLToRTFSandboxed(context.Background(), html)
			rtf = out
			return err
		},
	}
	ts := startTestServer(t, s)
	doc := "# Note\n\nInside words.\n\n" +
		"![abs](" + secretPNG + ")\n\n" +
		"![open](" + ts.URL + pathURL(filepath.Join(outside, "nothing.md")) + ")\n\n" +
		"![take](" + ts.URL + pathURL(secretPNG) + ")\n"
	if err := os.WriteFile(note, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+ChromeClipboardPath+"?path="+url.QueryEscape(note), nil)
	req.Header.Set(secFetchSiteHeader, secFetchSiteSameOrigin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clipboard status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(rtf), "Inside words") {
		t.Fatalf("rtf lost the document text:\n%s", rtf)
	}
	if strings.Contains(string(rtf), `\pict`) {
		t.Errorf("clipboard rtf embedded an outside image:\n%.400s", rtf)
	}
	if err := s.checkServed(secretPNG); err == nil {
		t.Errorf("copying to the clipboard made the viewer serve %s", outside)
	}
	if b, err := os.ReadFile(filepath.Join(root, "cache", rootsFileName)); err == nil && strings.Contains(string(b), outside) {
		t.Errorf("copying to the clipboard persisted a grant for %s:\n%s", outside, b)
	}
}
