// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"strings"
	"testing"
)

// Sanitize (the view server's mode) cuts the document's HTML to the
// allow-list; vellum's own placeholders, added after it, survive.
func TestRender_SanitizeRemovesDocumentScript(t *testing.T) {
	src := "<script>alert(1)</script>\n\n[go](javascript:alert(1)) <img src=x onerror=alert(1)>\n\n$x^2$\n\n```mermaid\ngraph TD\n  A --> B\n```\n"
	html, _, err := Render(context.Background(), []byte(src), &Options{Defer: true, Sanitize: true})
	if err != nil {
		t.Fatal(err)
	}
	body := html[strings.Index(html, "<body"):]
	for _, bad := range []string{"<script", "javascript:", "onerror"} {
		if strings.Contains(strings.ToLower(body), bad) {
			t.Fatalf("%s survived Sanitize:\n%s", bad, body)
		}
	}
	for _, want := range []string{`class="vellum-deferred vellum-deferred-math"`, `class="vellum-deferred vellum-deferred-mermaid"`, `>go</a>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("lost %s:\n%s", want, body)
		}
	}
	// Without Sanitize the document's HTML is kept as written: CLI and
	// MCP outputs are documents the user asked for by name.
	raw, _, err := Render(context.Background(), []byte(src), &Options{Defer: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "<script>alert(1)</script>") {
		t.Fatal("Render without Sanitize changed the document's raw HTML")
	}
}

func TestScrubFragments(t *testing.T) {
	in := map[string]FragmentResult{
		"a": {HTML: `<span class="katex" onclick="alert(1)">x</span>`},
		"b": {HTML: `<svg><script>alert(1)</script><a href="javascript:alert(1)"><text>t</text></a></svg>`},
		"c": {Err: errBoom},
	}
	out := ScrubFragments(in)
	for id, r := range out {
		for _, bad := range []string{"onclick", "<script", "javascript:"} {
			if strings.Contains(r.HTML, bad) {
				t.Fatalf("%s: %s survived: %s", id, bad, r.HTML)
			}
		}
	}
	if out["c"].Err != errBoom || !strings.Contains(out["a"].HTML, `class="katex"`) {
		t.Fatalf("scrub lost content or errors: %+v", out)
	}
}

var errBoom = &SoftError{Messages: []string{"boom"}}

// inlineSVGDoc is a Markdown document with a drawing written inline, as
// a block and inside a sentence. Every element and attribute in it is
// one the viewer must keep (🎯T83).
const inlineSVGDoc = "# Drawing\n\n" +
	`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 200 80" width="400" id="arch">` +
	`<style>.box { fill: #def; stroke: #036 } .lbl { font: 12px sans-serif }</style>` +
	`<defs><linearGradient id="grad"><stop offset="0" stop-color="#fff"/><stop offset="1" stop-color="#9cf"/></linearGradient>` +
	`<marker id="arrow" markerWidth="6" markerHeight="6" refX="5" refY="3" orient="auto"><path d="M0,0 L6,3 L0,6 z"/></marker></defs>` +
	`<rect class="box" x="5" y="5" width="80" height="30" rx="4" fill="url(#grad)" style="stroke-width:2"/>` +
	`<line x1="85" y1="20" x2="120" y2="20" stroke="#036" marker-end="url(#arrow)"/>` +
	`<a xlink:href="https://example.com/service"><text class="lbl" x="10" y="25">Service <tspan font-weight="bold">A</tspan></text></a>` +
	`<a href="other.md"><text class="lbl" x="125" y="25">Doc</text></a>` +
	`<use href="#arrow" x="150" y="50"/>` +
	`<circle cx="180" cy="60" r="5"><animate attributeName="r" values="3;6;3" dur="2s" repeatCount="indefinite"/></circle>` +
	`<foreignObject x="5" y="45" width="100" height="30"><div xmlns="http://www.w3.org/1999/xhtml" style="font-size:10px"><span class="note">wrapped <b>label</b></span></div></foreignObject>` +
	`</svg>` + "\n\n" +
	`An inline icon <svg width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="5" fill="green"/></svg> in a sentence.` + "\n"

// svgParts are the markers of inlineSVGDoc that must reach the page.
var svgParts = []string{
	`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 200 80" width="400" id="arch">`,
	`<style>.box { fill: #def; stroke: #036 } .lbl { font: 12px sans-serif }</style>`,
	`<linearGradient id="grad"><stop offset="0" stop-color="#fff"></stop><stop offset="1" stop-color="#9cf"></stop></linearGradient>`,
	`<marker id="arrow" markerWidth="6" markerHeight="6" refX="5" refY="3" orient="auto"><path d="M0,0 L6,3 L0,6 z"></path></marker>`,
	`<rect class="box" x="5" y="5" width="80" height="30" rx="4" fill="url(#grad)" style="stroke-width:2"></rect>`,
	`marker-end="url(#arrow)"`,
	`<a xlink:href="https://example.com/service"><text class="lbl" x="10" y="25">Service <tspan font-weight="bold">A</tspan></text></a>`,
	`<text class="lbl" x="125" y="25">Doc</text>`,
	`<use href="#arrow" x="150" y="50"></use>`,
	`<animate attributeName="r" values="3;6;3" dur="2s" repeatCount="indefinite"></animate>`,
	`<foreignObject x="5" y="45" width="100" height="30"><div xmlns="http://www.w3.org/1999/xhtml" style="font-size:10px"><span class="note">wrapped <b>label</b></span></div></foreignObject>`,
	`<svg width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="5" fill="green"></circle></svg>`,
}

// The viewer's sanitised render keeps a document's inline SVG as SVG,
// with all of its content. v0.43.0 to v0.45.0 dropped it entirely.
func TestRender_SanitizeKeepsInlineSVG(t *testing.T) {
	html, _, err := Render(context.Background(), []byte(inlineSVGDoc), &Options{Defer: true, Sanitize: true})
	if err != nil {
		t.Fatal(err)
	}
	body := html[strings.Index(html, "<body"):]
	for _, want := range svgParts {
		if !strings.Contains(body, want) {
			t.Errorf("lost from the page: %s", want)
		}
	}
	if t.Failed() {
		t.Logf("page body:\n%s", body)
	}
}
