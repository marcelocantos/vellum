// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/vellum/internal/testdeps"
)

// TestPDF_MermaidCannotDrawAnOutsideImageByPath is the reproduction for
// 🎯T68. The viewer PDF draws each diagram with mmdc, whose headless
// Chromium runs Mermaid on a file: page. A path with no scheme resolves
// against that page, so Chromium reads the file from disk and the
// screenshot that becomes the diagram's PNG carries its pixels: an image
// outside every served folder ends up in the PDF.
// TestPDF_MermaidCannotOpenAFolderOrReadOutside names the outside image
// by file: URL only, which Mermaid does not draw.
//
// Each diagram names the same outside image (pure blue, a colour
// Mermaid's theme never draws) a different way.
func TestPDF_MermaidCannotDrawAnOutsideImageByPath(t *testing.T) {
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
	shape := func(img string) string {
		return "```mermaid\nflowchart TD\n" +
			`  A@{ img: "` + img + `", label: "a", pos: "t", w: 60, h: 60, constraint: "on" }` + "\n  A --> B\n```\n\n"
	}
	cases := []struct{ name, diagram string }{
		{"image shape, absolute path", shape(secretPNG)},
		{"image shape, path climbing out of mmdc's own folder", shape("../../../../../../../../../../../../../../.." + secretPNG)},
		{"image shape, //localhost path", shape("//localhost" + secretPNG)},
		{"HTML label, absolute path", "```mermaid\nflowchart TD\n  A[\"<img src='" + secretPNG + "' width=60 height=60>\"] --> B\n```\n\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{CacheDir: filepath.Join(t.TempDir(), "cache"), Roots: []string{docs}}
			ts := startTestServer(t, s)
			note := filepath.Join(docs, "note.md")
			if err := os.WriteFile(note, []byte("# Note\n\nInside words.\n\n"+tc.diagram), 0o644); err != nil {
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
			out := t.TempDir()
			pdf := filepath.Join(out, "out.pdf")
			if err := os.WriteFile(pdf, body, 0o644); err != nil {
				t.Fatal(err)
			}
			prefix := filepath.Join(out, "img")
			if o, err := exec.Command("pdfimages", "-png", pdf, prefix).CombinedOutput(); err != nil {
				t.Fatalf("pdfimages: %v\n%s", err, o)
			}
			pngs, _ := filepath.Glob(prefix + "*.png")
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
					t.Errorf("the diagram drew the outside image: %d pure blue pixels in the PDF", n)
				}
			}
		})
	}
}
