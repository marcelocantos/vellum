// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"bytes"
	"compress/zlib"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/testdeps"
)

const pdfSecretMarker = "UNIQUE-SECRET-MARKER-9f3a"

// TestPDF_DoesNotReadOutsideTheServedFolder is the viewer's PDF download.
// The page refuses an image or stylesheet outside the served folder;
// the PDF renderer must not fetch them on its own either, including
// through a symlink that is planted inside the folder.
func TestPDF_DoesNotReadOutsideTheServedFolder(t *testing.T) {
	testdeps.Need(t, "weasyprint", "pdftotext", "pdfimages")

	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	docs := filepath.Join(root, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	secretPNG := filepath.Join(root, "secret.png")
	if err := os.WriteFile(secretPNG, pngBytes(t, 1, 1, color.RGBA{0, 0, 0, 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "pic.png"), pngBytes(t, 2, 2, color.RGBA{255, 0, 0, 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretPNG, filepath.Join(docs, "link.png")); err != nil {
		t.Fatal(err)
	}
	css := "body:before{content:\"" + pdfSecretMarker + " fromcss\"}\n"
	if err := os.WriteFile(filepath.Join(root, "secret.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte(pdfSecretMarker+" fromtxt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := "# Note\n\nInside words.\n\n" +
		"<link rel=\"stylesheet\" href=\"../secret.css\">\n\n" +
		"<style>@font-face{font-family:leak;src:url(\"../secret.txt\")}body{font-family:leak,sans-serif}</style>\n\n" +
		"![in](pic.png)\n\n" +
		"![out](../secret.png)\n\n" +
		"![sym](link.png)\n\n" +
		"<img src=\"" + secretPNG + "\" alt=\"abs\">\n"
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
	text := pdfText(t, pdf)
	if !strings.Contains(text, "Inside words") {
		t.Fatalf("pdf lost the document text:\n%s", text)
	}
	if strings.Contains(text, pdfSecretMarker) || bytes.Contains(pdfStreams(body), []byte(pdfSecretMarker)) {
		t.Fatalf("pdf contains a file from outside the served folder:\n%s", text)
	}
	list := pdfImageList(t, pdf)
	if !strings.Contains(list, "2") {
		t.Fatalf("inside image missing from pdf:\n%s", list)
	}
	// secret.png and the symlink to it are 1×1; the inside image is 2×2.
	if strings.Contains(list, " 1 ") && strings.Contains(list, "image") {
		for _, line := range strings.Split(list, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 5 && fields[2] == "image" && fields[3] == "1" && fields[4] == "1" {
				t.Fatalf("pdf embedded the outside image:\n%s", list)
			}
		}
	}
}

func pngBytes(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func pdfText(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("pdftotext", "-raw", path, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func pdfImageList(t *testing.T, path string) string {
	t.Helper()
	out, err := exec.Command("pdfimages", "-list", path).CombinedOutput()
	if err != nil {
		t.Fatalf("pdfimages: %v\n%s", err, out)
	}
	return string(out)
}

func pdfStreams(pdf []byte) []byte {
	var b bytes.Buffer
	rest := pdf
	for {
		i := bytes.Index(rest, []byte("stream"))
		if i < 0 {
			break
		}
		rest = rest[i+len("stream"):]
		if len(rest) > 0 && (rest[0] == '\n' || rest[0] == '\r') {
			if rest[0] == '\r' && len(rest) > 1 && rest[1] == '\n' {
				rest = rest[2:]
			} else {
				rest = rest[1:]
			}
		}
		j := bytes.Index(rest, []byte("endstream"))
		if j < 0 {
			break
		}
		chunk := bytes.TrimRight(rest[:j], "\r\n")
		rest = rest[j+len("endstream"):]
		r, err := zlib.NewReader(bytes.NewReader(chunk))
		if err != nil {
			continue
		}
		decoded, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			continue
		}
		b.Write(decoded)
	}
	return b.Bytes()
}
