// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/convert"
)

func stubHTML(_ context.Context, _ string, _ *convert.Options) (string, []string, error) {
	return `<!DOCTYPE html><html><head><title>T</title></head><body>
<h1 id="hello">Hello</h1>
<h2 id="world">World</h2>
<p><img src="pic.png" alt="pic"></p>
<p>cache test body</p>
</body></html>`, nil, nil
}

func TestInjectChrome_WrapsBodyAndAddsClass(t *testing.T) {
	in := `<!DOCTYPE html><html><head><title>T</title></head><body><h1 id="hello">Hello</h1></body></html>`
	out := injectChrome(in, "/docs/note.md")
	if !strings.Contains(out, `class="vellum-view"`) {
		t.Fatalf("missing body class:\n%s", out)
	}
	if !strings.Contains(out, `data-source="/docs/note.md"`) {
		t.Fatalf("missing source path:\n%s", out)
	}
	if !strings.Contains(out, `class="vellum-article"`) {
		t.Fatalf("missing article wrap:\n%s", out)
	}
	if !strings.Contains(out, `class="vellum-scroll"`) {
		t.Fatalf("missing scroll pane:\n%s", out)
	}
	if !strings.Contains(out, ".vellum-scroll") {
		t.Fatalf("missing scroll-pane CSS")
	}
	if !strings.Contains(out, `<h1 id="hello">Hello</h1>`) {
		t.Fatalf("lost body:\n%s", out)
	}
	if !strings.Contains(out, `data-vellum="pdf"`) || !strings.Contains(out, `data-vellum="clipboard"`) || !strings.Contains(out, `data-vellum="reveal"`) {
		t.Fatalf("missing toolbar actions:\n%s", out)
	}
	if !strings.Contains(out, `data-vellum="theme"`) || !strings.Contains(out, "vellum-theme") {
		t.Fatalf("missing theme toggle:\n%s", out)
	}
	if !strings.Contains(out, `data-vellum="toc-toggle"`) || !strings.Contains(out, `«`) {
		t.Fatalf("missing TOC toggle chrome:\n%s", out)
	}
	if !strings.Contains(out, `id="vellum-toc-splitter"`) {
		t.Fatalf("missing TOC splitter:\n%s", out)
	}
	if !strings.Contains(out, "vellum-toc-width") {
		t.Fatalf("missing TOC width persistence:\n%s", out)
	}
	if !strings.Contains(out, `id="vellum-lightbox"`) {
		t.Fatalf("missing lightbox:\n%s", out)
	}
	if !strings.Contains(out, ".vellum-chrome") {
		t.Fatalf("missing chrome CSS")
	}
	if !strings.Contains(out, "buildTOC()") {
		t.Fatalf("missing chrome JS")
	}
	if !strings.Contains(out, "fillTOC()") {
		t.Fatalf("missing TOC fill")
	}
	if !strings.Contains(out, `rel="icon"`) || !strings.Contains(out, ChromeFaviconPath) {
		t.Fatalf("missing favicon link:\n%s", out)
	}
}

func TestInjectChrome_KeepsInDocumentContents(t *testing.T) {
	in := `<!DOCTYPE html><html><head><title>T</title></head><body>
<div class="vellum-contents">
<h2>Contents</h2>
<ul><li><a href="#hello">Hello</a></li></ul>
</div>
<h1 id="hello">Hello</h1>
</body></html>`
	out := injectChrome(in, "/docs/note.md")
	if !strings.Contains(out, `class="vellum-contents"`) {
		t.Fatalf("in-document Contents stripped by chrome:\n%s", out)
	}
	if !strings.Contains(out, `<a href="#hello">Hello</a>`) {
		t.Fatalf("in-document Contents links lost:\n%s", out)
	}
	if !strings.Contains(out, `class="vellum-article"`) {
		t.Fatalf("missing article wrap:\n%s", out)
	}
	article := out[strings.Index(out, `class="vellum-article"`):]
	if !strings.Contains(article, `class="vellum-contents"`) {
		t.Fatalf("in-document Contents should live in the article:\n%s", out)
	}
}

func TestInjectChrome_PreservesExistingBodyClass(t *testing.T) {
	in := `<html><head></head><body class="x" data-q="1"><p>hi</p></body></html>`
	out := injectChrome(in, "/a.md")
	if !strings.Contains(out, `class="x vellum-view"`) {
		t.Fatalf("expected class merge:\n%s", out)
	}
	if !strings.Contains(out, `data-q="1"`) {
		t.Fatalf("lost body attrs:\n%s", out)
	}
}

func TestServer_ChromeInjectedOnServeNotInCache(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		CacheDir:   filepath.Join(dir, "cache"),
		RenderFile: stubHTML,
	}
	ts := startTestServer(t, s)

	resp, err := http.Get(ViewURL(ts.URL, md))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, html)
	}
	if !strings.Contains(html, "cache test body") {
		t.Fatalf("missing body:\n%s", html)
	}
	if !strings.Contains(html, `data-vellum="pdf"`) {
		t.Fatalf("response missing chrome:\n%s", html)
	}
	if !strings.Contains(html, `data-source="`+md+`"`) {
		t.Fatalf("response missing source:\n%s", html)
	}

	ents, err := os.ReadDir(s.CacheDir)
	if err != nil {
		t.Fatal(err)
	}
	var cacheHTML string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".html") {
			b, err := os.ReadFile(filepath.Join(s.CacheDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			cacheHTML = string(b)
			break
		}
	}
	if cacheHTML == "" {
		t.Fatal("expected cached HTML file")
	}
	if strings.Contains(cacheHTML, "vellum-toolbar") || strings.Contains(cacheHTML, "data-vellum=") {
		t.Fatalf("cache must stay chrome-free:\n%s", cacheHTML)
	}
	if !strings.Contains(cacheHTML, "cache test body") {
		t.Fatalf("cache missing convert body:\n%s", cacheHTML)
	}
}
