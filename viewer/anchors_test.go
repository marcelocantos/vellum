// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/convert"
)

func TestRewriteFragmentHrefs(t *testing.T) {
	doc := "/n/sub dir/a&b.md"
	docPath := pathURL(doc) // "/n/sub%20dir/a&b.md"
	cases := []struct{ in, want string }{
		{`<a href="#options">o</a>`, `<a href="/n/sub%20dir/a&amp;b.md#options">o</a>`},
		{`<a class="x" href='#end-matter'>e</a>`, `<a class="x" href='/n/sub%20dir/a&amp;b.md#end-matter'>e</a>`},
		{`<a href=#options>u</a>`, `<a href="/n/sub%20dir/a&amp;b.md#options">u</a>`},
		{`<A HREF="#fn:1" class="footnote-ref">1</A>`, `<A HREF="/n/sub%20dir/a&amp;b.md#fn:1" class="footnote-ref">1</A>`},
		{`<a href="#">top</a>`, `<a href="/n/sub%20dir/a&amp;b.md#">top</a>`},
		{`<area shape="rect" href="#map">`, `<area shape="rect" href="/n/sub%20dir/a&amp;b.md#map">`},
		{`<svg><a xlink:href="#node">n</a></svg>`, `<svg><a xlink:href="/n/sub%20dir/a&amp;b.md#node">n</a></svg>`},
		// Same-document by definition, or not links at all: untouched.
		{`<svg><use href="#glyph"/><use xlink:href="#g2"/><path marker-end="url(#arrow)"/></svg>`, ``},
		{`<link rel="x" href="#y">`, ``},
		{`<a data-href="#x" href="other.html">r</a>`, ``},
		{`<a href="https://e.com/#x">e</a> <a href="pic.png#x">p</a> <a href="">empty</a>`, ``},
		{`<p>href="#notatag"</p>`, ``},
	}
	for _, c := range cases {
		want := c.want
		if want == "" {
			want = c.in
		}
		if got := rewriteFragmentHrefs(c.in, docPath); got != want {
			t.Errorf("rewriteFragmentHrefs(%s)\n got %s\nwant %s", c.in, got, want)
		}
	}
}

var (
	pageBaseRe = regexp.MustCompile(`<base href="([^"]*)">`)
	pageHrefRe = regexp.MustCompile(`<a\b[^>]*\shref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
)

// browserResolve resolves every <a href> of page the way a browser does:
// against the first <base href>, after HTML-unescaping the attribute.
func browserResolve(t *testing.T, page string) []*url.URL {
	t.Helper()
	m := pageBaseRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no <base href> in page")
	}
	base, err := url.Parse(html.UnescapeString(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	var out []*url.URL
	for _, sm := range pageHrefRe.FindAllStringSubmatch(page, -1) {
		raw := sm[1] + sm[2] + sm[3]
		ref, err := url.Parse(html.UnescapeString(raw))
		if err != nil {
			t.Fatalf("href %q: %v", raw, err)
		}
		out = append(out, base.ResolveReference(ref))
	}
	return out
}

// 🎯T49: [x](#id) in the view page must land on this document's heading,
// not on the source directory that <base href> points at.
func TestServer_FragmentLinksResolveToDocument(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(dir, "note & co.md")
	src := "# Note\n\nSee [Options](#options), <a href=#end-matter>raw</a> and a footnote[^1].\n\n" +
		"![pic](pic.png) [sibling](other.md)\n\n## Options\n\nx\n\n## End Matter\n\ny\n\n[^1]: fn.\n"
	if err := os.WriteFile(md, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(t.TempDir(), "cache")}
	ts := startTestServer(t, s)
	pageURL := ViewURL(ts.URL, md)
	doc, err := url.Parse(pageURL)
	if err != nil {
		t.Fatal(err)
	}

	// The second GET is a cache hit: the cached shell keeps the raw
	// fragment hrefs (rewritten per response), so entries written by an
	// older vellum need no invalidation.
	for i, label := range []string{"fresh render", "cache hit"} {
		status, _, page := getText(t, pageURL)
		if status != http.StatusOK {
			t.Fatalf("%s: status %d: %s", label, status, page)
		}
		if int(s.ConvertCount.Load()) != 1 {
			t.Fatalf("%s: ConvertCount=%d", label, s.ConvertCount.Load())
		}
		for _, id := range []string{"options", "end-matter"} {
			if !strings.Contains(page, `id="`+id+`"`) {
				t.Fatalf("%s: heading id %q missing", label, id)
			}
		}
		got := map[string]bool{}
		for _, u := range browserResolve(t, page) {
			if u.Fragment == "" {
				continue
			}
			if u.Scheme != doc.Scheme || u.Host != doc.Host || u.EscapedPath() != doc.EscapedPath() {
				t.Errorf("%s: fragment link #%s resolves to %s, want the document %s", label, u.Fragment, u, doc)
			}
			got[u.Fragment] = true
		}
		for _, want := range []string{"options", "end-matter", "fn:1", "fnref:1"} {
			if !got[want] {
				t.Errorf("%s (%d): no link to #%s; got %v", label, i, want, got)
			}
		}
		// Relative non-fragment references still resolve against <base>.
		if !strings.Contains(page, `src="pic.png"`) {
			t.Errorf("%s: relative image rewritten", label)
		}
	}
	cached, err := os.ReadFile(filepath.Join(s.CacheDir, cacheName(md, ".html")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cached), `href="#options"`) {
		t.Errorf("cache entry should hold the raw fragment href (rewrite is per response)")
	}
}

// Deferred fragments are served separately and spliced into the page, so
// a diagram's own same-page links get the same treatment.
func TestServer_DeferredFragmentLinksResolveToDocument(t *testing.T) {
	body := "# D\n\n```mermaid\ngraph TD\n  A --> B\n  click A \"#options\"\n```\n\n## Options\n"
	dir, md := writeDoc(t, body)
	s := &Server{
		CacheDir: filepath.Join(dir, "cache"),
		RenderFragments: func(_ context.Context, frags []convert.Fragment) (map[string]convert.FragmentResult, error) {
			out := map[string]convert.FragmentResult{}
			for _, f := range frags {
				out[f.ID] = convert.FragmentResult{HTML: `<svg id="d"><a xlink:href="#options"><text>A</text></a><use href="#m"/></svg>`}
			}
			return out, nil
		},
	}
	ts := startTestServer(t, s)
	frags := convert.ExtractFragments([]byte(body), nil)
	if len(frags) != 1 {
		t.Fatalf("fragments: %+v", frags)
	}
	for _, label := range []string{"render", "fragment cache hit"} {
		status, _, got := getText(t, fragmentURL(ts.URL, md, frags[0].ID))
		if status != http.StatusOK {
			t.Fatalf("%s: status %d: %s", label, status, got)
		}
		want := `<svg id="d"><a xlink:href="` + pathURL(md) + `#options"><text>A</text></a><use href="#m"/></svg>`
		if got != want {
			t.Fatalf("%s:\n got %s\nwant %s", label, got, want)
		}
	}
}
