// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/vellum/internal/fileurl"
	"github.com/marcelocantos/vellum/internal/pandoc"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

// A CLI or MCP conversion embeds files from the document's own folder
// and nowhere else (render-boundaries report, D1 and D2). The fixture
// is the report's: a folder beside the document's holds a secret text
// file and a 3×3 canary image; the document names both every way the
// engines and pandoc accept, and also names an image and an attachment
// of its own.

const confineSecret = "SECRET-TOKEN-1234 private key material"

type confineFixture struct {
	docDir    string
	secretDir string
	secretTxt string
	canaryPNG string
	insidePNG []byte
}

func newConfineFixture(t *testing.T) *confineFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &confineFixture{
		docDir:    filepath.Join(root, "doc"),
		secretDir: filepath.Join(root, "secret"),
	}
	f.secretTxt = filepath.Join(f.secretDir, "secret.txt")
	f.canaryPNG = filepath.Join(f.secretDir, "canary.png")
	f.insidePNG = solidPNG(t, 2, 2, color.RGBA{R: 255, A: 255})
	for _, d := range []string{f.docDir, f.secretDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p string, b []byte) {
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(f.secretTxt, []byte(confineSecret+"\n"))
	write(f.canaryPNG, solidPNG(t, 3, 3, color.RGBA{B: 255, A: 255}))
	write(filepath.Join(f.docDir, "inside.png"), f.insidePNG)
	write(filepath.Join(f.docDir, "notes.txt"), []byte("inside attachment\n"))
	// A link inside the folder to a file outside it.
	if err := os.Symlink(f.canaryPNG, filepath.Join(f.docDir, "link.png")); err != nil {
		t.Fatal(err)
	}
	return f
}

// pdfDocument names the secret as an attachment and the canary as an
// image, by file: URL, absolute path and ../, and its own image and
// attachment by relative name.
func (f *confineFixture) pdfDocument(rel string) string {
	return "# Report\n\nInside words.\n\n" +
		`<a rel="attachment" href="` + fileurl.FromPath(f.secretTxt) + `">notes</a>` + "\n\n" +
		`<a rel="attachment" href="` + f.secretTxt + `">notes2</a>` + "\n\n" +
		`<link rel="attachment" href="` + rel + `secret/secret.txt">` + "\n\n" +
		`<a rel="attachment" href="notes.txt">own notes</a>` + "\n\n" +
		"![logo](" + f.canaryPNG + ")\n\n" +
		`<img src="` + fileurl.FromPath(f.canaryPNG) + `">` + "\n\n" +
		"![up](" + rel + "secret/canary.png)\n\n" +
		"![sym](link.png)\n\n" +
		"![own](inside.png)\n"
}

// pdfEmbeddedFiles returns pdfdetach's count of embedded files and the
// bytes of each.
func pdfEmbeddedFiles(t *testing.T, pdf string) []string {
	t.Helper()
	out, err := exec.Command("pdfdetach", "-list", pdf).CombinedOutput()
	if err != nil {
		t.Fatalf("pdfdetach -list: %v\n%s", err, out)
	}
	var n int
	if _, err := fmt.Sscanf(string(out), "%d embedded files", &n); err != nil {
		t.Fatalf("pdfdetach -list: %q", out)
	}
	var files []string
	for i := 1; i <= n; i++ {
		dst := filepath.Join(t.TempDir(), "att")
		if b, err := exec.Command("pdfdetach", "-save", fmt.Sprint(i), "-o", dst, pdf).CombinedOutput(); err != nil {
			t.Fatalf("pdfdetach -save %d: %v\n%s", i, err, b)
		}
		b, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, string(b))
	}
	return files
}

// pdfImageSizes returns "WxH" for every image pdfimages lists.
func pdfImageSizes(t *testing.T, pdf string) []string {
	t.Helper()
	out, err := exec.Command("pdfimages", "-list", pdf).CombinedOutput()
	if err != nil {
		t.Fatalf("pdfimages -list: %v\n%s", err, out)
	}
	var sizes []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[2] == "image" {
			sizes = append(sizes, fields[3]+"x"+fields[4])
		}
	}
	return sizes
}

func checkConfinedPDF(t *testing.T, pdf string, wantOwnAttachment bool) {
	t.Helper()
	for _, att := range pdfEmbeddedFiles(t, pdf) {
		if strings.Contains(att, "SECRET-TOKEN") {
			t.Errorf("PDF carries the outside file as an attachment: %q", att)
		}
	}
	if wantOwnAttachment {
		found := false
		for _, att := range pdfEmbeddedFiles(t, pdf) {
			found = found || strings.Contains(att, "inside attachment")
		}
		if !found {
			t.Errorf("PDF lost the document's own attachment")
		}
	}
	sizes := pdfImageSizes(t, pdf)
	own := 0
	for _, s := range sizes {
		switch s {
		case "3x3":
			t.Errorf("PDF draws the outside canary image: %v", sizes)
		case "2x2":
			own++
		}
	}
	if own == 0 {
		t.Errorf("PDF lost the document's own image: %v", sizes)
	}
}

// TestPDF_CLIEmbedsNoFileOutsideTheDocumentFolder is D1 through the CLI
// route (Run, file to file), for each engine installed.
func TestPDF_CLIEmbedsNoFileOutsideTheDocumentFolder(t *testing.T) {
	testdeps.Need(t, "weasyprint", "pdfdetach", "pdfimages")
	for _, backend := range []string{BackendWeasyPrint, BackendPrince} {
		if _, err := exec.LookPath(backend); err != nil && backend != BackendWeasyPrint {
			continue // Prince is opt-in; the gate needs only WeasyPrint.
		}
		t.Run(backend, func(t *testing.T) {
			f := newConfineFixture(t)
			in := filepath.Join(f.docDir, "report.md")
			if err := os.WriteFile(in, []byte(f.pdfDocument("../")), 0o644); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(f.docDir, "report.pdf")
			_, err := Run(context.Background(), &Request{
				From:    Endpoint{Media: MediaFile, Path: in},
				To:      Endpoint{Media: MediaFile, Path: out},
				Backend: backend,
			})
			if err != nil {
				t.Fatal(err)
			}
			// Prince does not honour rel=attachment at all.
			checkConfinedPDF(t, out, backend == BackendWeasyPrint)
		})
	}
}

// TestPDF_ConvertEmbedsNoFileOutsideTheDocumentFolder is D1 through the
// Convert entry point (the viewer's cached-PDF route uses it).
func TestPDF_ConvertEmbedsNoFileOutsideTheDocumentFolder(t *testing.T) {
	testdeps.Need(t, "weasyprint", "pdfdetach", "pdfimages")
	f := newConfineFixture(t)
	in := filepath.Join(f.docDir, "report.md")
	if err := os.WriteFile(in, []byte(f.pdfDocument("../")), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(f.docDir, "report.pdf")
	if err := Convert(context.Background(), in, out, nil); err != nil {
		t.Fatal(err)
	}
	checkConfinedPDF(t, out, true)
}

// TestPDF_ContentEmbedsNoFileOutsideTheOutputFolder is D1 through MCP's
// from: content, which has no folder of its own: relative names resolve
// against the output file's folder, and that folder is the limit.
func TestPDF_ContentEmbedsNoFileOutsideTheOutputFolder(t *testing.T) {
	testdeps.Need(t, "weasyprint", "pdfdetach", "pdfimages")
	f := newConfineFixture(t)
	out := filepath.Join(f.docDir, "mcp.pdf")
	_, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaContent, Content: f.pdfDocument("../")},
		To:   Endpoint{Media: MediaFile, Path: out},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkConfinedPDF(t, out, true)
}

// remoteRecorder serves one PNG at every path and records what asked.
type remoteRecorder struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (rr *remoteRecorder) serve(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rr.mu.Lock()
		rr.reqs = append(rr.reqs, r.Clone(context.Background()))
		rr.mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		w.Write(body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (rr *remoteRecorder) all() []*http.Request {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return append([]*http.Request(nil), rr.reqs...)
}

// TestPDF_CLIStillShowsRemoteImages: confining local files leaves the
// engine's network reach as it was (README badges keep working).
func TestPDF_CLIStillShowsRemoteImages(t *testing.T) {
	testdeps.Need(t, "weasyprint", "pdfimages")
	var rec remoteRecorder
	ts := rec.serve(t, solidPNG(t, 5, 5, color.RGBA{G: 255, A: 255}))
	dir := t.TempDir()
	in := filepath.Join(dir, "badge.md")
	if err := os.WriteFile(in, []byte("# Badge\n\n![b]("+ts.URL+"/badge.png)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "badge.pdf")
	if _, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaFile, Path: in},
		To:   Endpoint{Media: MediaFile, Path: out},
	}); err != nil {
		t.Fatal(err)
	}
	if sizes := pdfImageSizes(t, out); len(sizes) != 1 || sizes[0] != "5x5" {
		t.Fatalf("remote image not drawn: %v", sizes)
	}
	reqs := rec.all()
	if len(reqs) == 0 {
		t.Fatal("remote image never requested")
	}
	// The engine used to fetch this itself, with no fetch metadata, which
	// the viewer reads as the user opening a folder (D3). vellum fetches
	// it and says so.
	checkRemoteSaysImage(t, reqs)
}

// TestDocFiles_SwappedDirectoryIsNotRead: the folder is pinned when the
// conversion starts. Replacing it with a symlink to a secret folder
// before a file is read must not return the secret.
func TestDocFiles_SwappedDirectoryIsNotRead(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(root, "doc")
	secret := filepath.Join(root, "secret")
	if err := os.Mkdir(doc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "secret.txt"), []byte(confineSecret+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(doc, "inside.txt"), []byte("inside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := newDocFiles(doc)
	if b, err := files.read(filepath.Join(doc, "inside.txt")); err != nil || string(b) != "inside\n" {
		t.Fatalf("inside file before the swap: %q %v", b, err)
	}
	if err := os.RemoveAll(doc); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, doc); err != nil {
		t.Fatal(err)
	}
	b, err := files.read(filepath.Join(doc, "secret.txt"))
	if err == nil && strings.Contains(string(b), "SECRET-TOKEN") {
		t.Fatalf("read the secret after the document folder was replaced by a symlink: %q", b)
	}
}

// deckDocument names the secret and the canary as images in every
// spelling pandoc's Markdown reader accepts, and its own image and a
// non-image file of its own.
func (f *confineFixture) deckDocument(remote string) string {
	return "# Deck\n\n" +
		"![logo](" + f.canaryPNG + ")\n\n" +
		"![key](" + f.secretTxt + ")\n\n" +
		"![url](" + fileurl.FromPath(f.secretTxt) + ")\n\n" +
		"![up](../secret/canary.png)\n\n" +
		"![sym](link.png)\n\n" +
		"![ref][r]\n\n" +
		`<img src="` + f.canaryPNG + `">` + "\n\n" +
		"![own text](notes.txt)\n\n" +
		"![own](inside.png)\n\n" +
		"![remote](" + remote + "/deck.png)\n\n" +
		"[r]: " + f.secretTxt + "\n"
}

func zipMedia(t *testing.T, path string) map[string][]byte {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	media := map[string][]byte{}
	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, "ppt/media/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		media[f.Name] = b
	}
	return media
}

// TestPPTX_EmbedsOnlyImagesFromTheDocumentFolder is D2 for pptx.
func TestPPTX_EmbedsOnlyImagesFromTheDocumentFolder(t *testing.T) {
	testdeps.Need(t, pandoc.Binary)
	f := newConfineFixture(t)
	var rec remoteRecorder
	remotePNG := solidPNG(t, 5, 5, color.RGBA{G: 255, A: 255})
	ts := rec.serve(t, remotePNG)
	in := filepath.Join(f.docDir, "deck.md")
	if err := os.WriteFile(in, []byte(f.deckDocument(ts.URL)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(f.docDir, "deck.pptx")
	_, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaFile, Path: in},
		To:   Endpoint{Media: MediaFile, Path: out},
	})
	var se *SoftError
	if err != nil && !errors.As(err, &se) {
		t.Fatal(err)
	}
	canary, err := os.ReadFile(f.canaryPNG)
	if err != nil {
		t.Fatal(err)
	}
	var own, remote bool
	for name, b := range zipMedia(t, out) {
		switch {
		case bytes.Contains(b, []byte("SECRET-TOKEN")):
			t.Errorf("deck carries the outside file as %s", name)
		case bytes.Contains(b, []byte("inside attachment")):
			t.Errorf("deck carries a non-image file as %s", name)
		case bytes.Equal(b, canary):
			t.Errorf("deck carries the outside canary as %s", name)
		case bytes.Equal(b, f.insidePNG):
			own = true
		case bytes.Equal(b, remotePNG):
			remote = true
		}
	}
	if !own {
		t.Error("deck lost the document's own image")
	}
	if !remote {
		t.Error("deck lost the remote image")
	}
	checkRemoteSaysImage(t, rec.all())
}

// checkRemoteSaysImage: a remote image is fetched by vellum, and the
// request says it is a cross-site image, so a viewer it reaches never
// takes it for the user opening a document (🎯T67's grant rule).
func checkRemoteSaysImage(t *testing.T, reqs []*http.Request) {
	t.Helper()
	if len(reqs) == 0 {
		t.Fatal("remote image never requested")
	}
	for _, r := range reqs {
		if r.Header.Get("Sec-Fetch-Site") != "cross-site" || r.Header.Get("Sec-Fetch-Dest") != "image" {
			t.Errorf("remote request %s carries no image fetch metadata: %v", r.URL, r.Header)
		}
	}
}

var rtfPictureRe = regexp.MustCompile(`\\(png|jpeg)blip`)

// TestRTF_EmbedsOnlyImagesFromTheDocumentFolder is D2 for RTF.
func TestRTF_EmbedsOnlyImagesFromTheDocumentFolder(t *testing.T) {
	testdeps.Need(t, pandoc.Binary)
	f := newConfineFixture(t)
	var rec remoteRecorder
	ts := rec.serve(t, solidPNG(t, 5, 5, color.RGBA{G: 255, A: 255}))
	in := filepath.Join(f.docDir, "note.md")
	doc := "# Note\n\n" +
		"![logo](" + f.canaryPNG + ")\n\n" +
		`<img src="` + fileurl.FromPath(f.canaryPNG) + `">` + "\n\n" +
		"![up](../secret/canary.png)\n\n" +
		"![sym](link.png)\n\n" +
		"![own](inside.png)\n\n" +
		"![remote](" + ts.URL + "/note.png)\n"
	if err := os.WriteFile(in, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(f.docDir, "note.rtf")
	_, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaFile, Path: in},
		To:   Endpoint{Media: MediaFile, Path: out},
	})
	var se *SoftError
	if err != nil && !errors.As(err, &se) {
		t.Fatal(err)
	}
	rtf, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// The document's own image and the remote one; none of the canaries.
	if n := len(rtfPictureRe.FindAll(rtf, -1)); n != 2 {
		t.Errorf("RTF holds %d pictures, want 2 (own and remote)", n)
	}
	checkRemoteSaysImage(t, rec.all())
}
