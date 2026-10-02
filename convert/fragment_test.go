// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

const fragmentDoc = "# T\n\nInline $x^2$ then\n\n$$\na = b\n$$\n\n```mermaid\ngraph TD\n  A --> B\n```\n\n<!-- vellum:scale 0.5 -->\n```mermaid\ngraph TD\n  A --> B\n```\n\ntail\n"

func TestRender_DeferEmitsPlaceholdersWithoutSubprocess(t *testing.T) {
	prev := renderMermaidFn
	t.Cleanup(func() { renderMermaidFn = prev })
	var calls atomic.Int32
	renderMermaidFn = func(context.Context, string, string) (string, error) {
		calls.Add(1)
		return "<svg></svg>", nil
	}

	html, soft, err := Render(context.Background(), []byte(fragmentDoc), &Options{Defer: true})
	if err != nil {
		t.Fatal(err)
	}
	if soft != nil {
		t.Fatalf("deferred render must not report soft errors: %v", soft)
	}
	if calls.Load() != 0 {
		t.Fatalf("deferred render ran mmdc %d times", calls.Load())
	}
	frags := ExtractFragments([]byte(fragmentDoc), nil)
	if len(frags) != 4 {
		t.Fatalf("want 4 fragments, got %d: %+v", len(frags), frags)
	}
	// Block math is extracted before inline math, so display comes first.
	display, inline, m1, m2 := frags[0], frags[1], frags[2], frags[3]
	if inline.Kind != FragmentMath || inline.Display || inline.Source != "x^2" {
		t.Fatalf("inline fragment: %+v", inline)
	}
	if display.Kind != FragmentMath || !display.Display || display.Source != "a = b" {
		t.Fatalf("display fragment: %+v", display)
	}
	if m1.Kind != FragmentMermaid || m1.Format != MermaidSVG || m1.Source != "graph TD\n  A --> B" {
		t.Fatalf("mermaid fragment: %+v", m1)
	}
	if m1.ID != m2.ID {
		t.Fatalf("same diagram source must share an id (scale is in the wrapper): %s vs %s", m1.ID, m2.ID)
	}
	for _, f := range frags {
		if !FragmentIDRe.MatchString(f.ID) {
			t.Fatalf("malformed id %q", f.ID)
		}
	}
	for _, want := range []string{
		`<span class="vellum-deferred vellum-deferred-math" data-vellum-fragment="` + inline.ID + `">x^2</span>`,
		`<div class="katex-display"><span class="vellum-deferred vellum-deferred-math" data-vellum-fragment="` + display.ID + `">a = b</span></div>`,
		`<div class="mermaid-svg"><div class="vellum-deferred vellum-deferred-mermaid" data-vellum-fragment="` + m1.ID + `" role="img" aria-label="Diagram, loading"><pre>graph TD
  A --&gt; B</pre></div></div>`,
		`<div class="mermaid-svg" style="max-width: 50%"><div class="vellum-deferred vellum-deferred-mermaid" data-vellum-fragment="` + m2.ID + `"`,
		`katex.min.css`,
		`<p>tail</p>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<!--MATH:") || strings.Contains(html, "<!--MERMAID:") {
		t.Fatalf("comment placeholders leaked:\n%s", html)
	}
	if n := len(fragmentPlaceholderRe.FindAllString(html, -1)); n != 4 {
		t.Fatalf("placeholder regex finds %d, want 4", n)
	}
}

func TestFragmentID_DependsOnKindModeAndSource(t *testing.T) {
	a := mathFragment(mathExpr{Expr: "x"})
	b := mathFragment(mathExpr{Expr: "x", DisplayMode: true})
	c := mermaidFragment(mermaidDiagram{source: "x"}, MermaidSVG)
	d := mermaidFragment(mermaidDiagram{source: "x"}, MermaidPNG)
	e := mermaidFragment(mermaidDiagram{source: "x", scale: 2}, MermaidSVG)
	ids := map[string]bool{a.ID: true, b.ID: true, c.ID: true, d.ID: true}
	if len(ids) != 4 {
		t.Fatalf("ids collide: %v", ids)
	}
	if e.ID != c.ID {
		t.Fatalf("scale must not change the id")
	}
}

func TestRenderFragments_DedupesAndBatchesMath(t *testing.T) {
	prev := renderMermaidFn
	t.Cleanup(func() { renderMermaidFn = prev })
	var mermaidCalls atomic.Int32
	renderMermaidFn = func(_ context.Context, src, format string) (string, error) {
		mermaidCalls.Add(1)
		if strings.Contains(src, "bad") {
			return "", errors.New("mmdc: nope")
		}
		return "<svg>" + format + "</svg>", nil
	}
	ok := mermaidFragment(mermaidDiagram{source: "graph TD"}, MermaidSVG)
	bad := mermaidFragment(mermaidDiagram{source: "bad"}, MermaidSVG)
	results, err := RenderFragments(context.Background(), []Fragment{ok, bad, ok, ok})
	if err != nil {
		t.Fatal(err)
	}
	if mermaidCalls.Load() != 2 {
		t.Fatalf("want 2 mmdc runs for 2 distinct diagrams, got %d", mermaidCalls.Load())
	}
	if r := results[ok.ID]; r.Err != nil || r.HTML != "<svg>svg</svg>" {
		t.Fatalf("ok result: %+v", r)
	}
	if r := results[bad.ID]; r.Err == nil || r.HTML != "" {
		t.Fatalf("bad result: %+v", r)
	}
	if _, err := RenderFragments(context.Background(), []Fragment{{ID: "0000000000000000", Kind: "nope"}}); err != nil {
		t.Fatalf("unknown kind must be a per-fragment error, got %v", err)
	}
	if r := results[ok.ID]; r.Err != nil {
		t.Fatal(r.Err)
	}
}

func TestResolveFragments_PartialAndFallbacks(t *testing.T) {
	html, _, err := Render(context.Background(), []byte(fragmentDoc), &Options{Defer: true})
	if err != nil {
		t.Fatal(err)
	}
	frags := ExtractFragments([]byte(fragmentDoc), nil)
	display, inline, mermaid := frags[0], frags[1], frags[2]

	// Only the mermaid fragment resolved: math placeholders stay put.
	partial, soft := ResolveFragments(html, map[string]FragmentResult{mermaid.ID: {HTML: "<svg>ok</svg>"}})
	if soft != nil {
		t.Fatalf("soft=%v", soft)
	}
	if n := strings.Count(partial, `<div class="mermaid-svg"><svg>ok</svg></div>`); n != 1 {
		t.Fatalf("want the unscaled wrapper filled once, got %d:\n%s", n, partial)
	}
	if !strings.Contains(partial, `<div class="mermaid-svg" style="max-width: 50%"><svg>ok</svg></div>`) {
		t.Fatalf("scaled duplicate not filled:\n%s", partial)
	}
	if n := len(fragmentPlaceholderRe.FindAllString(partial, -1)); n != 2 {
		t.Fatalf("want 2 math placeholders left, got %d", n)
	}

	// Failures become source fallbacks with 1-based per-kind numbering.
	failed, soft := ResolveFragments(partial, map[string]FragmentResult{
		inline.ID:  {HTML: `<span class="katex">x</span>`},
		display.ID: {Err: errors.New("node: boom")},
	})
	// Numbering follows document order, where the display block is second.
	if len(soft) != 1 || !strings.Contains(soft[0], "math expression 2: node: boom") {
		t.Fatalf("soft=%v", soft)
	}
	if !strings.Contains(failed, `<div class="katex-display"><span class="katex-error">a = b</span></div>`) {
		t.Fatalf("display fallback:\n%s", failed)
	}
	if !strings.Contains(failed, `<p>Inline <span class="katex">x</span> then</p>`) {
		t.Fatalf("inline math:\n%s", failed)
	}
	if len(fragmentPlaceholderRe.FindAllString(failed, -1)) != 0 {
		t.Fatalf("placeholders remain:\n%s", failed)
	}

	// Mermaid failure keeps the escaped source, without the loading <pre>.
	mf, soft := ResolveFragments(html, map[string]FragmentResult{mermaid.ID: {Err: fmt.Errorf("mmdc: exit 1")}})
	if len(soft) != 2 || !strings.HasPrefix(soft[0], "mermaid diagram 1: mmdc") || !strings.HasPrefix(soft[1], "mermaid diagram 2: mmdc") {
		t.Fatalf("soft=%v", soft)
	}
	if n := strings.Count(mf, `<pre class="mermaid-error">graph TD
  A --&gt; B</pre>`); n != 2 {
		t.Fatalf("want 2 mermaid fallbacks, got %d:\n%s", n, mf)
	}
}

func TestRender_DeferThenResolveMatchesDirectRender(t *testing.T) {
	prev := renderMermaidFn
	t.Cleanup(func() { renderMermaidFn = prev })
	renderMermaidFn = func(_ context.Context, src, _ string) (string, error) {
		return "<svg>" + htmlEscapeText(src) + "</svg>", nil
	}
	// No math: the direct path would need node. Two diagrams, one scaled.
	doc := "a\n\n```mermaid\ngraph TD\n```\n\n<!-- vellum:scale 0.5 -->\n```mermaid\ngraph LR\n```\n"
	direct, soft, err := Render(context.Background(), []byte(doc), &Options{})
	if err != nil || soft != nil {
		t.Fatalf("direct: %v %v", err, soft)
	}
	shell, _, err := Render(context.Background(), []byte(doc), &Options{Defer: true})
	if err != nil {
		t.Fatal(err)
	}
	results, err := RenderFragments(context.Background(), ExtractFragments([]byte(doc), nil))
	if err != nil {
		t.Fatal(err)
	}
	resolved, soft := ResolveFragments(shell, results)
	if soft != nil {
		t.Fatalf("soft=%v", soft)
	}
	if resolved != direct {
		t.Fatalf("two-stage render differs from direct:\n--- direct\n%s\n--- resolved\n%s", direct, resolved)
	}
}
