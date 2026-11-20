// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"image/color"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/testdeps"
)

// TestPDF_EngineFetchCannotOpenAFolder covers the PDF engine as a client
// of the viewer itself. The engine fetches a document's http images with
// no fetch metadata, which is what the viewer takes for the user opening
// a document. A document that names the viewer's own origin must not get
// a folder served that way, during the render or afterwards.
func TestPDF_EngineFetchCannotOpenAFolder(t *testing.T) {
	testdeps.Need(t, "weasyprint", "pdftotext", "pdfimages")

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

	s := &Server{CacheDir: filepath.Join(root, "cache"), Roots: []string{docs}}
	ts := startTestServer(t, s)
	// The first image asks for a Markdown path in the outside folder (the
	// file need not exist); the second is the file wanted from it.
	doc := "# Note\n\nInside words.\n\n" +
		"![open](" + ts.URL + filepath.Join(outside, "nothing.md") + ")\n\n" +
		"![take](" + ts.URL + secretPNG + ")\n"
	if err := os.WriteFile(note, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(ts.URL + ChromePDFPath + "?path=" + url.QueryEscape(note))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pdf status %d: %s", resp.StatusCode, body)
	}
	if err := s.checkServed(secretPNG); err == nil {
		t.Errorf("rendering the PDF made the viewer serve %s", outside)
	}
	pdf := filepath.Join(root, "out.pdf")
	if err := os.WriteFile(pdf, body, 0o644); err != nil {
		t.Fatal(err)
	}
	list := pdfImageList(t, pdf)
	for _, line := range strings.Split(list, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[2] == "image" && fields[3] == "7" && fields[4] == "5" {
			t.Errorf("pdf embedded the outside image fetched from the viewer itself:\n%s", list)
		}
	}
	// A page request (same-origin) for the outside file is still refused.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+secretPNG, nil)
	req.Header.Set(secFetchSiteHeader, secFetchSiteSameOrigin)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("page request for the outside file after the PDF: status %d, want 403", resp.StatusCode)
	}
}
