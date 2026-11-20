// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/fileurl"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

// TestPDF_MermaidCannotOpenAFolderOrReadOutside covers the other program
// the viewer PDF runs on document content: mmdc, whose headless Chromium
// draws a diagram's image shapes. Chromium sends a page image's fetch
// metadata, so the viewer refuses its requests and never grants, and it
// does not read file: images. The diagram names the viewer's own origin
// for an outside folder and image, and the outside image by file: URL;
// the secret image is pure blue, a colour Mermaid's theme never draws.
func TestPDF_MermaidCannotOpenAFolderOrReadOutside(t *testing.T) {
	testdeps.Need(t, "weasyprint", "mmdc", "pdfimages")

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
	if err := os.WriteFile(secretPNG, pngBytes(t, 40, 40, color.RGBA{0, 0, 255, 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(root, "cache"), Roots: []string{docs}}
	var viewerLog requestLog
	vh := s.Handler()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		viewerLog.add(r)
		vh.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	shape := func(id, img string) string {
		return "  " + id + `@{ img: "` + img + `", label: "` + id + `", pos: "t", w: 60, h: 60, constraint: "on" }` + "\n"
	}
	// Both diagrams name an image. The renderer answers every request
	// except its own page with an empty image, so nothing reaches the
	// viewer and neither image is read. The diagrams still draw.
	doc := "# Note\n\nInside words.\n\n```mermaid\nflowchart TD\n" +
		shape("A", ts.URL+filepath.Join(outside, "nothing.md")) +
		shape("B", ts.URL+secretPNG) +
		"  A --> B\n```\n\n```mermaid\nflowchart TD\n" +
		shape("C", fileurl.FromPath(secretPNG)) +
		"  C --> D\n```\n"
	note := filepath.Join(docs, "note.md")
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
		t.Errorf("rendering the diagram made the viewer serve %s", outside)
	}
	reached := 0
	for _, r := range viewerLog.all() {
		if r.URL.Path == ChromePDFPath {
			continue
		}
		reached++
		if r.Header.Get(secFetchSiteHeader) == "" {
			t.Errorf("viewer got %s without fetch metadata (%s)", r.URL.Path, r.UserAgent())
		}
	}
	if reached != 0 {
		t.Errorf("mmdc made %d request(s) to the viewer; the renderer should have answered them itself", reached)
	}
	pdf := filepath.Join(root, "out.pdf")
	if err := os.WriteFile(pdf, body, 0o644); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(root, "img")
	if out, err := exec.Command("pdfimages", "-png", pdf, prefix).CombinedOutput(); err != nil {
		t.Fatalf("pdfimages: %v\n%s", err, out)
	}
	pngs, _ := filepath.Glob(prefix + "*.png")
	if len(pngs) == 0 {
		t.Fatal("pdf has no diagram image")
	}
	for _, p := range pngs {
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if n := pureBlue(img); n > 0 {
			t.Errorf("%s has %d pure blue pixels: the diagram drew the outside image", filepath.Base(p), n)
		}
	}
	if b, err := os.ReadFile(filepath.Join(root, "cache", rootsFileName)); err == nil && strings.Contains(string(b), outside) {
		t.Errorf("rendering the diagram persisted a grant for %s", outside)
	}
}

func pureBlue(img image.Image) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r == 0 && g == 0 && bl == 0xffff {
				n++
			}
		}
	}
	return n
}
