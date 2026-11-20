// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"encoding/base64"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/vellum/convert"
	"github.com/marcelocantos/vellum/internal/fileurl"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

// TestPDF_EngineReachesNothingTheDocumentNames attacks the viewer PDF
// (🎯T66) with every way a document's images can name something else:
// file: URLs, data: wrapping, redirects from a permitted remote image to
// the viewer itself or to a file, CSS url() and @import, fonts, <link>,
// <use>, <image>, <feImage>, XInclude and external entities inside SVG.
// Every outside target is a 7×5 image, an http "recorder" that logs what
// reaches it, or the viewer's own origin naming an outside folder.
//
// The PDF must still show what the page shows: an image in the folder
// (9×4), a remote image (11×3), and an SVG figure that carries its own
// raster (13×2). It runs on each installed engine.
func TestPDF_EngineReachesNothingTheDocumentNames(t *testing.T) {
	testdeps.Need(t, "weasyprint", "pdftotext", "pdfimages")
	engines := []string{convert.BackendWeasyPrint}
	if _, err := exec.LookPath("prince"); err == nil {
		engines = append(engines, convert.BackendPrince)
	}
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) { attackPDF(t, engine) })
	}
}

type requestLog struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (l *requestLog) add(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reqs = append(l.reqs, r)
}

func (l *requestLog) all() []*http.Request {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]*http.Request(nil), l.reqs...)
}

func attackPDF(t *testing.T, engine string) {
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
	blue := color.RGBA{0, 0, 255, 255}
	secret := pngBytes(t, 7, 5, blue)
	secretPNG := filepath.Join(outside, "secret.png")
	if err := os.WriteFile(secretPNG, secret, 0o644); err != nil {
		t.Fatal(err)
	}
	secretFile := fileurl.FromPath(secretPNG)
	if err := os.WriteFile(filepath.Join(docs, "pic.png"), pngBytes(t, 9, 4, blue), 0o644); err != nil {
		t.Fatal(err)
	}
	// Symlinks out of the folder, as a file and as a directory.
	if err := os.Symlink(secretPNG, filepath.Join(docs, "link.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(docs, "linkdir")); err != nil {
		t.Fatal(err)
	}

	s := &Server{CacheDir: filepath.Join(root, "cache"), Roots: []string{docs}, Backend: engine}
	var viewerLog requestLog
	vh := s.Handler()
	viewer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		viewerLog.add(r)
		vh.ServeHTTP(w, r)
	}))
	t.Cleanup(viewer.Close)

	var recLog requestLog
	recorder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recLog.add(r)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(secret)
	}))
	t.Cleanup(recorder.Close)

	// The permitted remote host: one real image, and redirects out.
	var remoteLog requestLog
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteLog.add(r)
		switch r.URL.Path {
		case "/ok.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes(t, 11, 3, blue))
		case "/to-viewer-md":
			http.Redirect(w, r, viewer.URL+filepath.Join(outside, "nothing.md"), http.StatusFound)
		case "/to-viewer-png":
			http.Redirect(w, r, viewer.URL+secretPNG, http.StatusFound)
		case "/to-file":
			http.Redirect(w, r, secretFile, http.StatusFound)
		case "/svg":
			// A remote SVG is inlined; what it names is refused.
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = io.WriteString(w, `<svg xmlns="http://www.w3.org/2000/svg" width="70" height="50">`+
				`<image href="`+secretFile+`" width="70" height="50"/>`+
				`<image href="`+recorder.URL+`/remote-svg" width="70" height="50"/></svg>`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(remote.Close)

	selfContained := `<svg xmlns="http://www.w3.org/2000/svg" width="26" height="4">` +
		`<image href="data:image/png;base64,` + base64.StdEncoding.EncodeToString(pngBytes(t, 13, 2, blue)) + `" width="26" height="4"/></svg>`
	inner := `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="70" height="50">` +
		`<image href="` + secretFile + `" width="70" height="50"/>` +
		`<image xlink:href="` + recorder.URL + `/nested" width="70" height="50"/></svg>`
	hostile := `<?xml version="1.0"?>` +
		`<!DOCTYPE svg [<!ENTITY ext SYSTEM "` + recorder.URL + `/xxe"><!ENTITY extfile SYSTEM "` + secretFile + `">]>` +
		`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:xi="http://www.w3.org/2001/XInclude" width="70" height="50">` +
		`<style>@import url("` + recorder.URL + `/import.css"); @import url("` + fileurl.FromPath(filepath.Join(outside, "x.css")) + `");` +
		`@font-face{font-family:x;src:url("` + recorder.URL + `/font.woff")}` +
		`rect{fill:url("` + recorder.URL + `/paint.svg#p")} text{font-family:x}</style>` +
		`<link xmlns="http://www.w3.org/1999/xhtml" rel="stylesheet" href="` + recorder.URL + `/link.css"/>` +
		`<defs><filter id="f"><feImage href="` + recorder.URL + `/feimage"/><feImage href="` + secretFile + `"/></filter></defs>` +
		`<rect width="10" height="10" filter="url(#f)"/>` +
		`<use href="` + recorder.URL + `/use.svg#a"/><use xlink:href="` + fileurl.FromPath(filepath.Join(outside, "u.svg")) + `#a"/>` +
		`<image href="` + secretFile + `" width="70" height="50"/>` +
		`<image href="` + recorder.URL + `/image" width="70" height="50"/>` +
		`<image href="` + viewer.URL + filepath.Join(outside, "nothing.md") + `" width="70" height="50"/>` +
		`<image href="` + viewer.URL + secretPNG + `" width="70" height="50"/>` +
		`<image href="data:image/svg+xml;base64,` + base64.StdEncoding.EncodeToString([]byte(inner)) + `" width="70" height="50"/>` +
		`<xi:include href="` + recorder.URL + `/xinclude"/><xi:include href="` + secretFile + `" parse="text"/>` +
		`<text x="0" y="40">&ext;&extfile;</text></svg>`
	if err := os.WriteFile(filepath.Join(docs, "hostile.svg"), []byte(hostile), 0o644); err != nil {
		t.Fatal(err)
	}
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

	doc := "# Note\n\nInside words.\n\n" +
		"![inside](pic.png)\n\n" +
		"![remote](" + remote.URL + "/ok.png)\n\n" +
		"![figure](data:image/svg+xml;base64," + b64(selfContained) + ")\n\n" +
		"![served svg](hostile.svg)\n\n" +
		"![data svg](data:image/svg+xml;base64," + b64(hostile) + ")\n\n" +
		"![wrapped](data:image/svg+xml;base64," + b64(`<svg xmlns="http://www.w3.org/2000/svg" width="70" height="50"><image href="data:image/svg+xml;base64,`+b64(inner)+`" width="70" height="50"/></svg>`) + ")\n\n" +
		"![remote svg](" + remote.URL + "/svg)\n\n" +
		"![redir md](" + remote.URL + "/to-viewer-md)\n\n" +
		"![redir png](" + remote.URL + "/to-viewer-png)\n\n" +
		"![redir file](" + remote.URL + "/to-file)\n\n" +
		"![self md](" + viewer.URL + filepath.Join(outside, "nothing.md") + ")\n\n" +
		"![self png](" + viewer.URL + secretPNG + ")\n\n" +
		"![abs](" + secretPNG + ")\n\n" +
		"![rel](../outside/secret.png)\n\n" +
		"![link](link.png)\n\n" +
		"![linkdir](linkdir/secret.png)\n\n" +
		`<img src="` + secretFile + `"><object data="` + recorder.URL + `/object"></object>` +
		`<link rel="stylesheet" href="` + recorder.URL + `/head.css"><style>@import "` + recorder.URL + `/style.css";</style>` + "\n"
	note := filepath.Join(docs, "note.md")
	if err := os.WriteFile(note, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(viewer.URL + ChromePDFPath + "?path=" + url.QueryEscape(note))
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
	sizes := map[string]int{}
	list := pdfImageList(t, pdf)
	for _, line := range strings.Split(list, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[2] == "image" {
			sizes[fields[3]+"x"+fields[4]]++
		}
	}
	if sizes["7x5"] != 0 {
		t.Errorf("pdf embedded an outside image %d times:\n%s", sizes["7x5"], list)
	}
	for _, want := range []string{"9x4", "11x3", "13x2"} {
		if sizes[want] == 0 {
			t.Errorf("pdf lost the permitted %s image:\n%s", want, list)
		}
	}

	// Nothing reaches the recorder: every reference to it is inside an
	// SVG, in markup the sanitiser removes, or in CSS.
	for _, r := range recLog.all() {
		t.Errorf("recorder got %s (%s)", r.URL.Path, r.UserAgent())
	}
	// Every request to the viewer but the PDF download is vellum's own
	// image fetch, and says so.
	for _, r := range viewerLog.all() {
		if r.URL.Path == ChromePDFPath {
			continue
		}
		if r.Header.Get(secFetchSiteHeader) != secFetchSiteCrossSite || r.Header.Get(secFetchDestHeader) != secFetchDestImage {
			t.Errorf("viewer got %s without a page image's fetch metadata (%v)", r.URL.Path, r.Header)
		}
	}
	for _, r := range remoteLog.all() {
		if r.Header.Get(secFetchSiteHeader) != secFetchSiteCrossSite {
			t.Errorf("remote got %s without fetch metadata (%s)", r.URL.Path, r.UserAgent())
		}
	}
	if err := s.checkServed(secretPNG); err == nil {
		t.Errorf("rendering the PDF made the viewer serve %s", outside)
	}
	if b, err := os.ReadFile(filepath.Join(root, "cache", rootsFileName)); err == nil && strings.Contains(string(b), outside) {
		t.Errorf("rendering the PDF persisted a grant for %s:\n%s", outside, b)
	}
}

// TestPDF_MathKeepsKaTeXFonts is the positive control for the engine
// that fetches nothing: KaTeX's fonts reach the viewer PDF inlined
// (convert.Options.KaTeXInline), on each installed engine.
func TestPDF_MathKeepsKaTeXFonts(t *testing.T) {
	testdeps.Need(t, "weasyprint", "node", "pdffonts")
	engines := []string{convert.BackendWeasyPrint}
	if _, err := exec.LookPath("prince"); err == nil {
		engines = append(engines, convert.BackendPrince)
	}
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			note := filepath.Join(dir, "math.md")
			if err := os.WriteFile(note, []byte("# Math\n\n$$\\mathbb{R} \\to \\mathfrak{g}$$\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			s := &Server{CacheDir: filepath.Join(dir, "cache"), Roots: []string{dir}, Backend: engine}
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
			pdf := filepath.Join(dir, "out.pdf")
			if err := os.WriteFile(pdf, body, 0o644); err != nil {
				t.Fatal(err)
			}
			fonts, err := exec.Command("pdffonts", pdf).CombinedOutput()
			if err != nil {
				t.Fatalf("pdffonts: %v: %s", err, fonts)
			}
			for _, want := range []string{"KaTeX_AMS", "KaTeX_Fraktur"} {
				if !strings.Contains(string(fonts), want) {
					t.Errorf("pdf lacks %s:\n%s", want, fonts)
				}
			}
		})
	}
}
