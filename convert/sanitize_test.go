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
