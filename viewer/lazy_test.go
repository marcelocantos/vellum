// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/convert"
)

// mmdc-shaped SVG: nested <svg> icon plus a <foreignObject><div> label, so
// naive div or svg matching would cut the wrapper short.
const sampleMermaidSVG = `<svg id="my-svg" width="100%" xmlns="http://www.w3.org/2000/svg" class="flowchart" style="max-width: 360px; background-color: white;" viewBox="4 4 360 418" role="graphics-document document"><g><foreignObject width="80" height="24"><div xmlns="http://www.w3.org/1999/xhtml"><span>Start</span></div></foreignObject><svg width="10" height="10"><circle r="4"/></svg></g></svg>`

const sampleSequenceSVG = `<svg id="my-svg" width="100%" xmlns="http://www.w3.org/2000/svg" style="max-width: 450px;" viewBox="-50 -10 450 261"><g><text>Hi</text></g></svg>`

func mermaidStubHTML(_ context.Context, _ string, _ *convert.Options) (string, []string, error) {
	return `<!DOCTYPE html><html><head><title>T</title></head><body>
<h1 id="hello">Hello</h1>
<p>before</p>
<div class="mermaid-svg">` + sampleMermaidSVG + `</div>
<p>between</p>
<div class="mermaid-svg" style="max-width: 50%">` + sampleSequenceSVG + `</div>
<div class="mermaid-svg"><pre class="mermaid-error">graph TD</pre></div>
<p><img src="pic.png" alt="pic"></p>
<p>after</p>
</body></html>`, nil, nil
}

func TestLazyLoadImages(t *testing.T) {
	in := `<p><img src="a.png" alt="a"><img loading="eager" src="b.png"><IMG src="c.png" decoding="sync"></p>`
	out := lazyLoadImages(in)
	want := `<p><img src="a.png" alt="a" loading="lazy" decoding="async"><img loading="eager" src="b.png" decoding="async"><IMG src="c.png" decoding="sync" loading="lazy"></p>`
	if out != want {
		t.Fatalf("lazyLoadImages:\n got %s\nwant %s", out, want)
	}
	if again := lazyLoadImages(out); again != out {
		t.Fatalf("not idempotent:\n%s", again)
	}
}

func TestDeferMermaid_LiftsSVGsAndReservesBox(t *testing.T) {
	in, _, _ := mermaidStubHTML(context.Background(), "", nil)
	out, frags := deferMermaid(in)
	if len(frags) != 2 {
		t.Fatalf("want 2 fragments, got %d", len(frags))
	}
	if strings.Contains(out, "<svg") {
		t.Fatalf("SVG left inline:\n%s", out)
	}
	for _, want := range []string{
		`<p>before</p>`, `<p>between</p>`, `<p>after</p>`,
		`<div class="mermaid-svg"><div class="vellum-lazy-svg" data-vellum-fragment="` + fragmentID(sampleMermaidSVG) + `"`,
		`style="aspect-ratio: 360 / 418; max-width: 360px"`,
		`<div class="mermaid-svg" style="max-width: 50%"><div class="vellum-lazy-svg" data-vellum-fragment="` + fragmentID(sampleSequenceSVG) + `"`,
		`style="aspect-ratio: 450 / 261; max-width: 450px"`,
		`<div class="mermaid-svg"><pre class="mermaid-error">graph TD</pre></div>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if got := frags[fragmentID(sampleMermaidSVG)]; got != sampleMermaidSVG {
		t.Fatalf("fragment 1 mangled:\n%s", got)
	}
	if got := frags[fragmentID(sampleSequenceSVG)]; got != sampleSequenceSVG {
		t.Fatalf("fragment 2 mangled:\n%s", got)
	}
	// Exactly one `</div>` follows each placeholder: the original wrapper's.
	if n := strings.Count(out, `"></div></div>`); n != 2 {
		t.Fatalf("want 2 closed placeholders, got %d:\n%s", n, out)
	}
}

func TestDeferMermaid_NoDiagramsIsIdentity(t *testing.T) {
	in := `<html><body><p>plain</p><svg viewBox="0 0 1 1"></svg><div class="mermaid-svg"><pre class="mermaid-error">x</pre></div></body></html>`
	out, frags := deferMermaid(in)
	if out != in || frags != nil {
		t.Fatalf("expected identity, got %v:\n%s", frags, out)
	}
	unterminated := `<div class="mermaid-svg"><svg><g></g>`
	if out, frags := deferMermaid(unterminated); out != unterminated || frags != nil {
		t.Fatalf("unterminated svg must be left alone, got %v:\n%s", frags, out)
	}
}

func TestInlineSVGLength(t *testing.T) {
	s := `<svg><svg></svg><foreignObject><div></div></foreignObject></svg></div>tail`
	n, ok := inlineSVGLength(s)
	if !ok || s[:n] != `<svg><svg></svg><foreignObject><div></div></foreignObject></svg>` {
		t.Fatalf("got ok=%v n=%d %q", ok, n, s[:n])
	}
	if _, ok := inlineSVGLength(`<p></p>`); ok {
		t.Fatal("non-svg prefix must not match")
	}
	if _, ok := inlineSVGLength(`<svg><svg></svg>`); ok {
		t.Fatal("unbalanced svg must not match")
	}
}

func TestServer_DefersMermaidAndServesFragments(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache"), RenderFile: mermaidStubHTML}
	ts := startTestServer(t, s)

	resp, err := http.Get(ViewURL(ts.URL, md))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, page)
	}
	if strings.Contains(page, "<svg id=") {
		t.Fatalf("page must not inline Mermaid SVG:\n%s", page)
	}
	id := fragmentID(sampleMermaidSVG)
	if !strings.Contains(page, `data-vellum-fragment="`+id+`"`) {
		t.Fatalf("page missing fragment placeholder:\n%s", page)
	}
	if !strings.Contains(page, `<img src="pic.png" alt="pic" loading="lazy" decoding="async">`) {
		t.Fatalf("page missing lazy image:\n%s", page)
	}
	if !strings.Contains(page, "hydrateLazyFragments()") || !strings.Contains(page, ".vellum-lazy-svg") {
		t.Fatalf("chrome missing lazy fragment loader")
	}

	// The convert cache (clipboard / PDF source) keeps the SVG inline.
	ents, err := os.ReadDir(s.CacheDir)
	if err != nil {
		t.Fatal(err)
	}
	cached := ""
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".html") {
			b, err := os.ReadFile(filepath.Join(s.CacheDir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			cached = string(b)
		}
	}
	if !strings.Contains(cached, sampleMermaidSVG) || strings.Contains(cached, "vellum-lazy-svg") {
		t.Fatalf("cache must stay whole and placeholder-free:\n%s", cached)
	}

	fragURL := ts.URL + ChromeFragmentPath + "?path=" + url.QueryEscape(md) + "&id=" + id
	fr, err := http.Get(fragURL)
	if err != nil {
		t.Fatal(err)
	}
	defer fr.Body.Close()
	fb, _ := io.ReadAll(fr.Body)
	if fr.StatusCode != http.StatusOK {
		t.Fatalf("fragment status %d: %s", fr.StatusCode, fb)
	}
	if string(fb) != sampleMermaidSVG {
		t.Fatalf("fragment body mismatch:\n%s", fb)
	}
	if cc := fr.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("fragment should be immutable, got %q", cc)
	}
	if got := s.ConvertCount.Load(); got != 1 {
		t.Fatalf("fragment fetch must hit the cache, converts=%d", got)
	}

	for _, bad := range []struct{ q, name string }{
		{"?path=" + url.QueryEscape(md) + "&id=0000000000000000", "unknown id"},
		{"?path=" + url.QueryEscape(md) + "&id=nope", "malformed id"},
		{"?path=" + url.QueryEscape(filepath.Join(dir, "missing.md")) + "&id=" + id, "missing file"},
		{"?id=" + id, "missing path"},
	} {
		r, err := http.Get(ts.URL + ChromeFragmentPath + bad.q)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode == http.StatusOK {
			t.Fatalf("%s: expected error status, got 200", bad.name)
		}
	}
	if r, err := http.Post(ts.URL+ChromeFragmentPath+"?path="+url.QueryEscape(md)+"&id="+id, "", nil); err != nil {
		t.Fatal(err)
	} else {
		r.Body.Close()
		if r.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("POST status %d", r.StatusCode)
		}
	}
}
