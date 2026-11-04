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
	"github.com/marcelocantos/vellum/embed"
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
	if !strings.Contains(out, "vellum-pin-title") || !strings.Contains(out, "pinLeadingTitle") {
		t.Fatalf("missing leading-title pin")
	}
	if !strings.Contains(out, `<h1 id="hello">Hello</h1>`) {
		t.Fatalf("lost body:\n%s", out)
	}
	if !strings.Contains(out, `data-vellum="download"`) || !strings.Contains(out, `data-vellum="pdf"`) || !strings.Contains(out, `data-vellum="source"`) {
		t.Fatalf("missing download menu:\n%s", out)
	}
	trigger := out[strings.Index(out, `data-vellum="download"`):]
	trigger = trigger[:strings.Index(trigger, `</button>`)+len(`</button>`)]
	if strings.Contains(trigger, "PDF") {
		t.Fatalf("download trigger should be icon-only, got:\n%s", trigger)
	}
	if !strings.Contains(out, `data-vellum="clipboard"`) || !strings.Contains(out, `data-vellum="reveal"`) {
		t.Fatalf("missing toolbar actions:\n%s", out)
	}
	if !strings.Contains(out, "vellum-download-menu") || !strings.Contains(out, `toggleDownloadMenu`) {
		t.Fatalf("missing download menu chrome:\n%s", out)
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
	if !strings.Contains(out, `id="vellum-gone"`) || !strings.Contains(out, "This file was deleted.") {
		t.Fatalf("missing deletion dialog:\n%s", out)
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
	if !strings.Contains(html, `data-vellum="download"`) || !strings.Contains(html, `data-vellum="source"`) {
		t.Fatalf("response missing download chrome:\n%s", html)
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

// mediaBlock returns the body of the first "@media <query> {" block in css
// that contains marker, with braces balanced.
func mediaBlock(t *testing.T, css, query, marker string) string {
	t.Helper()
	head := "@media " + query + " {"
	for rest := css; ; {
		i := strings.Index(rest, head)
		if i < 0 {
			t.Fatalf("no %q block containing %q", head, marker)
		}
		body := rest[i+len(head):]
		depth := 1
		for j, c := range body {
			switch c {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth == 0 {
				body = body[:j]
				break
			}
		}
		if strings.Contains(body, marker) {
			return body
		}
		rest = rest[i+len(head):]
	}
}

// Viewer tables leave the 980px reading column and size their columns to
// content, on screen only. Prose keeps the reading width, and print keeps
// github.css's fixed, page-wide tables, which the PDF needs.
func TestChromeCSS_TablesBreakOutAndAutosize(t *testing.T) {
	if !strings.Contains(chromeCSS, ".vellum-article {\n  max-width: 980px;\n  margin: 0 auto;\n  padding: 32px;\n}") {
		t.Fatal("prose reading column changed: .vellum-article must stay max-width 980px, margin 0 auto, padding 32px")
	}
	screen := mediaBlock(t, chromeCSS, "screen", ".vellum-article table")
	for _, want := range []string{
		"container-type: inline-size",
		"width: max-content",
		"max-width: 100%",
		"overflow-x: auto",
		"table-layout: auto",
		"overflow-wrap: break-word",
		"word-break: normal",
		"max-width: calc(100cqw - 64px)",
		"translateX(round(min(0px, 490px - 50%), 1px))",
		"clip-path: inset(0 -100vmax)",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen table rules missing %q:\n%s", want, screen)
		}
	}
	if strings.Contains(chromeCSS, "table-layout: fixed") {
		t.Error("viewer chrome must not force fixed (even-width) table columns")
	}
	if strings.Contains(chromeCSS, "width: 100%;\n    table-layout") {
		t.Error("viewer tables must not be forced to the full width")
	}
	if printCSS := mediaBlock(t, chromeCSS, "print", ".vellum-article"); strings.Contains(printCSS, "table") {
		t.Errorf("print chrome must leave tables to github.css:\n%s", printCSS)
	}

	gh := embed.GitHubCSS
	if !strings.Contains(gh, "  width: 100%;\n  max-width: 100%;\n  /* fixed:") ||
		!strings.Contains(gh, "  table-layout: fixed;\n}") {
		t.Error("embed/github.css table rules (PDF fit, v0.34.0) must stay as they are")
	}
}
