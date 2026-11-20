// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"encoding/base64"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/fileurl"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

// TestPDF_SVGImageCannotPullInAnOutsideFile covers the PDF engine's own
// fetches. An SVG shown through <img> is inert in a browser, but the PDF
// engine draws it itself and loads the <image> elements inside it. The
// SVG names the outside file by absolute file URL, so no base URL
// confines it; only an engine that fetches nothing does.
func TestPDF_SVGImageCannotPullInAnOutsideFile(t *testing.T) {
	testdeps.Need(t, "weasyprint", "pdftotext", "pdfimages")

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	docs := filepath.Join(root, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	// The outside image is 7×5; nothing inside the folder has that size.
	const secretW, secretH = "7", "5"
	secretPNG := filepath.Join(root, "secret.png")
	if err := os.WriteFile(secretPNG, pngBytes(t, 7, 5, color.RGBA{0, 0, 255, 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	svg := `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="70" height="50">` +
		`<image href="` + fileurl.FromPath(secretPNG) + `" width="70" height="50"/>` +
		`<image xlink:href="` + fileurl.FromPath(secretPNG) + `" width="70" height="50"/>` +
		`</svg>`
	if err := os.WriteFile(filepath.Join(docs, "inside.svg"), []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := "# Note\n\nInside words.\n\n" +
		"![file](inside.svg)\n\n" +
		"![data](data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg)) + ")\n"
	note := filepath.Join(docs, "note.md")
	if err := os.WriteFile(note, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Server{CacheDir: filepath.Join(root, "cache"), Roots: []string{docs}}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + ChromePDFPath + "?path=" + url.QueryEscape(note))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pdf status %d: %s", resp.StatusCode, body)
	}
	pdf := filepath.Join(root, "out.pdf")
	if err := os.WriteFile(pdf, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if text := pdfText(t, pdf); !strings.Contains(text, "Inside words") {
		t.Fatalf("pdf lost the document text:\n%s", text)
	}
	list := pdfImageList(t, pdf)
	for _, line := range strings.Split(list, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[2] == "image" && fields[3] == secretW && fields[4] == secretH {
			t.Fatalf("pdf embedded the outside image through an SVG:\n%s", list)
		}
	}
}
