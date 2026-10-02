// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/vellum/convert"
)

// A document with inline and display math and two Mermaid diagrams, the
// second a scaled duplicate of the first (same fragment id).
const lazyDoc = "# Hello\n\nbefore $x^2$ and\n\n$$\na = b\n$$\n\n```mermaid\ngraph TD\n  A --> B\n```\n\n<!-- vellum:scale 0.5 -->\n```mermaid\ngraph TD\n  A --> B\n```\n\n![pic](pic.png)\n\nafter\n"

// mmdc-shaped SVG: nested <svg> icon plus a <foreignObject><div> label.
const sampleMermaidSVG = `<svg id="my-svg" width="100%" xmlns="http://www.w3.org/2000/svg" class="flowchart" style="max-width: 360px; background-color: white;" viewBox="4 4 360 418" role="graphics-document document"><g><foreignObject width="80" height="24"><div xmlns="http://www.w3.org/1999/xhtml"><span>Start</span></div></foreignObject><svg width="10" height="10"><circle r="4"/></svg></g></svg>`

// stubFragments stands in for mmdc and node: every Mermaid diagram becomes
// sampleMermaidSVG, every math expression a KaTeX-shaped span. Sources
// containing "boom" fail.
func stubFragments(calls *atomic.Int64) func(context.Context, []convert.Fragment) (map[string]convert.FragmentResult, error) {
	return func(_ context.Context, frags []convert.Fragment) (map[string]convert.FragmentResult, error) {
		out := map[string]convert.FragmentResult{}
		for _, f := range frags {
			calls.Add(1)
			if strings.Contains(f.Source, "boom") {
				out[f.ID] = convert.FragmentResult{Err: errors.New("mmdc: simulated failure")}
				continue
			}
			switch f.Kind {
			case convert.FragmentMermaid:
				out[f.ID] = convert.FragmentResult{HTML: sampleMermaidSVG}
			default:
				out[f.ID] = convert.FragmentResult{HTML: `<span class="katex">` + f.Source + `</span>`}
			}
		}
		return out, nil
	}
}

func writeDoc(t *testing.T, body string) (dir, md string) {
	t.Helper()
	dir = t.TempDir()
	md = filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, md
}

func getText(t *testing.T, u string) (int, http.Header, string) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b)
}

func fragmentURL(ts, md, id string) string {
	return ts + ChromeFragmentPath + "?path=" + url.QueryEscape(md) + "&id=" + id
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

func TestStamp_RejectsOtherFormats(t *testing.T) {
	dir, md := writeDoc(t, "x")
	cache := filepath.Join(dir, "c.html")
	if err := os.WriteFile(cache, []byte("c"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeStamp(cache, md); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(md)
	if !stampMatches(cache, info) {
		t.Fatal("fresh stamp must match")
	}
	// A v0.24 stamp (mtime and size only) belongs to a whole-page cache
	// entry with diagrams inline; it must miss so the shell is rendered.
	data, _ := os.ReadFile(stampPath(cache))
	old := strings.Join(strings.Fields(string(data))[:2], " ") + "\n"
	if err := os.WriteFile(stampPath(cache), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if stampMatches(cache, info) {
		t.Fatal("two-field stamp from an older layout must not match")
	}
}

func TestFlightGroup_CoalescesSameKey(t *testing.T) {
	var g flightGroup
	var runs atomic.Int32
	release := make(chan struct{})
	fn := func() (map[string]convert.FragmentResult, error) {
		runs.Add(1)
		<-release
		return map[string]convert.FragmentResult{"a": {HTML: "x"}}, nil
	}
	var wg sync.WaitGroup
	results := make([]map[string]convert.FragmentResult, 3)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _ = g.do("k", fn)
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(10 * time.Millisecond) // let the other two queue behind the flight
	close(release)
	wg.Wait()
	if runs.Load() != 1 {
		t.Fatalf("fn ran %d times, want 1", runs.Load())
	}
	for i, r := range results {
		if r["a"].HTML != "x" {
			t.Fatalf("waiter %d got %+v", i, r)
		}
	}
	// A later call after landing runs again.
	if _, _ = g.do("k", fn); runs.Load() != 2 {
		t.Fatalf("post-landing call did not run: %d", runs.Load())
	}
}

func TestServer_ShellFirstThenFragmentsOnDemand(t *testing.T) {
	dir, md := writeDoc(t, lazyDoc)
	var renders atomic.Int64
	s := &Server{CacheDir: filepath.Join(dir, "cache"), RenderFragments: stubFragments(&renders)}
	ts := startTestServer(t, s)

	status, _, page := getText(t, ViewURL(ts.URL, md))
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, page)
	}
	if renders.Load() != 0 {
		t.Fatalf("first paint waited on %d fragment renders", renders.Load())
	}
	frags := convert.ExtractFragments([]byte(lazyDoc), nil)
	if len(frags) != 4 {
		t.Fatalf("fragments: %+v", frags)
	}
	display, inline, diagram := frags[0], frags[1], frags[2]
	// Chrome toolbar icons are inline SVG too, so look for the diagram's.
	if strings.Contains(page, `<svg id="my-svg"`) || strings.Contains(page, `class="katex"`) {
		t.Fatalf("page must not carry rendered fragments:\n%s", page)
	}
	for _, want := range []string{
		`<p>before <span class="vellum-deferred vellum-deferred-math" data-vellum-fragment="` + inline.ID + `">x^2</span> and</p>`,
		`<div class="katex-display"><span class="vellum-deferred vellum-deferred-math" data-vellum-fragment="` + display.ID + `">a = b</span></div>`,
		`<div class="mermaid-svg"><div class="vellum-deferred vellum-deferred-mermaid" data-vellum-fragment="` + diagram.ID + `" role="img" aria-label="Diagram, loading"><pre>graph TD`,
		`<div class="mermaid-svg" style="max-width: 50%"><div class="vellum-deferred vellum-deferred-mermaid" data-vellum-fragment="` + diagram.ID + `"`,
		`<img src="pic.png" alt="pic" loading="lazy" decoding="async">`,
		`<p>after</p>`,
		`katex.min.css`,
		"hydrateDeferred()",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("missing %q in:\n%s", want, page)
		}
	}
	if strings.Contains(page, "aspect-ratio") {
		t.Fatalf("no render is cached yet, so no box can be reserved:\n%s", page)
	}

	// The shell cache holds placeholders, never a rendered fragment.
	ents, err := os.ReadDir(s.CacheDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".html") && !strings.HasPrefix(e.Name(), fragmentCachePrefix) {
			b, _ := os.ReadFile(filepath.Join(s.CacheDir, e.Name()))
			if !strings.Contains(string(b), "vellum-deferred") || strings.Contains(string(b), "<svg") {
				t.Fatalf("shell cache must hold placeholders only:\n%s", b)
			}
		}
	}

	// Mermaid: one render per id, then cache hits; immutable response.
	status, hdr, body := getText(t, fragmentURL(ts.URL, md, diagram.ID))
	if status != http.StatusOK || body != sampleMermaidSVG {
		t.Fatalf("fragment %d: %s", status, body)
	}
	if cc := hdr.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("fragment should be immutable, got %q", cc)
	}
	if renders.Load() != 1 {
		t.Fatalf("renders=%d after one diagram", renders.Load())
	}
	if _, _, again := getText(t, fragmentURL(ts.URL, md, diagram.ID)); again != sampleMermaidSVG || renders.Load() != 1 {
		t.Fatalf("second fetch must come from the fragment cache (renders=%d)", renders.Load())
	}
	if _, err := os.Stat(fragmentCachePath(s.CacheDir, diagram.ID)); err != nil {
		t.Fatalf("fragment not cached: %v", err)
	}

	// Math: the first request renders the document's whole batch (one
	// node start), so the second expression is already cached.
	status, _, body = getText(t, fragmentURL(ts.URL, md, inline.ID))
	if status != http.StatusOK || body != `<span class="katex">x^2</span>` {
		t.Fatalf("math fragment %d: %s", status, body)
	}
	if renders.Load() != 3 {
		t.Fatalf("renders=%d, want the two math expressions batched with the diagram", renders.Load())
	}
	status, _, body = getText(t, fragmentURL(ts.URL, md, display.ID))
	if status != http.StatusOK || body != `<span class="katex">a = b</span>` || renders.Load() != 3 {
		t.Fatalf("display math %d %q renders=%d", status, body, renders.Load())
	}

	// A revisit reserves the diagram's box from the cached SVG.
	_, _, page = getText(t, ViewURL(ts.URL, md))
	if !strings.Contains(page, `data-vellum-fragment="`+diagram.ID+`" role="img" aria-label="Diagram, loading" style="aspect-ratio: 360 / 418; max-width: 360px">`) {
		t.Fatalf("revisit should reserve the diagram box:\n%s", page)
	}
	if got := s.ConvertCount.Load(); got != 1 {
		t.Fatalf("shell must be served from cache, converts=%d", got)
	}

	for _, bad := range []struct {
		q, name string
		status  int
	}{
		{"?path=" + url.QueryEscape(md) + "&id=0000000000000000", "unknown id", http.StatusNotFound},
		{"?path=" + url.QueryEscape(md) + "&id=nope", "malformed id", http.StatusBadRequest},
		{"?path=" + url.QueryEscape(filepath.Join(dir, "missing.md")) + "&id=" + "0000000000000000", "missing file", http.StatusNotFound},
		{"?id=" + diagram.ID, "missing path", http.StatusBadRequest},
	} {
		status, _, _ := getText(t, ts.URL+ChromeFragmentPath+bad.q)
		if status != bad.status {
			t.Fatalf("%s: status %d, want %d", bad.name, status, bad.status)
		}
	}
	if r, err := http.Post(fragmentURL(ts.URL, md, diagram.ID), "", nil); err != nil {
		t.Fatal(err)
	} else {
		r.Body.Close()
		if r.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("POST status %d", r.StatusCode)
		}
	}
}

func TestServer_FragmentRenderFailureIsNotCached(t *testing.T) {
	dir, md := writeDoc(t, "```mermaid\ngraph TD\n  boom\n```\n")
	var renders atomic.Int64
	s := &Server{CacheDir: filepath.Join(dir, "cache"), RenderFragments: stubFragments(&renders)}
	ts := startTestServer(t, s)
	id := convert.ExtractFragments([]byte("```mermaid\ngraph TD\n  boom\n```\n"), nil)[0].ID

	for attempt := 1; attempt <= 2; attempt++ {
		status, hdr, body := getText(t, fragmentURL(ts.URL, md, id))
		if status != http.StatusInternalServerError || !strings.Contains(body, "simulated failure") {
			t.Fatalf("attempt %d: %d %s", attempt, status, body)
		}
		if cc := hdr.Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("failure must not be cacheable, got %q", cc)
		}
		if renders.Load() != int64(attempt) {
			t.Fatalf("attempt %d: renders=%d (failure cached?)", attempt, renders.Load())
		}
	}
	if _, err := os.Stat(fragmentCachePath(s.CacheDir, id)); !os.IsNotExist(err) {
		t.Fatalf("failed fragment must not reach the cache: %v", err)
	}
}

func TestServer_ClipboardResolvesEveryFragment(t *testing.T) {
	dir, md := writeDoc(t, lazyDoc)
	var renders atomic.Int64
	var got string
	s := &Server{
		CacheDir:        filepath.Join(dir, "cache"),
		RenderFragments: stubFragments(&renders),
		WriteClipboard:  func(html string) error { got = html; return nil },
	}
	ts := startTestServer(t, s)

	resp, err := http.Post(ts.URL+ChromeClipboardPath+"?path="+url.QueryEscape(md), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clipboard status %d", resp.StatusCode)
	}
	if strings.Contains(got, "vellum-deferred") {
		t.Fatalf("clipboard HTML still has placeholders:\n%s", got)
	}
	for _, want := range []string{
		`<p>before <span class="katex">x^2</span> and</p>`,
		`<div class="katex-display"><span class="katex">a = b</span></div>`,
		`<div class="mermaid-svg">` + sampleMermaidSVG + `</div>`,
		`<div class="mermaid-svg" style="max-width: 50%">` + sampleMermaidSVG + `</div>`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("clipboard missing %q:\n%s", want, got)
		}
	}
	// Three distinct fragments, rendered once; the view page then serves
	// them from the fragment cache.
	if renders.Load() != 3 {
		t.Fatalf("renders=%d", renders.Load())
	}
	diagram := convert.ExtractFragments([]byte(lazyDoc), nil)[2]
	if _, _, body := getText(t, fragmentURL(ts.URL, md, diagram.ID)); body != sampleMermaidSVG || renders.Load() != 3 {
		t.Fatalf("fragment after clipboard: %q renders=%d", body, renders.Load())
	}
	// The shell cache is untouched by the full render.
	shell, err := s.cachedHTML(context.Background(), md)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(shell), "vellum-deferred") {
		t.Fatalf("shell cache lost its placeholders:\n%s", shell)
	}
}

func TestServer_FragmentNotFoundWhenSourceChanged(t *testing.T) {
	dir, md := writeDoc(t, lazyDoc)
	var renders atomic.Int64
	s := &Server{CacheDir: filepath.Join(dir, "cache"), RenderFragments: stubFragments(&renders)}
	ts := startTestServer(t, s)
	diagram := convert.ExtractFragments([]byte(lazyDoc), nil)[2]

	if err := os.WriteFile(md, []byte("# no diagrams\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, _, _ := getText(t, fragmentURL(ts.URL, md, diagram.ID))
	if status != http.StatusNotFound || renders.Load() != 0 {
		t.Fatalf("stale id: status %d renders=%d", status, renders.Load())
	}
}
