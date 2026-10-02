// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// FragmentKind names the subprocess a Fragment needs.
type FragmentKind string

const (
	// FragmentMermaid is a ```mermaid block rendered by mmdc.
	FragmentMermaid FragmentKind = "mermaid"
	// FragmentMath is a $…$ or $$…$$ expression rendered by KaTeX (node).
	FragmentMath FragmentKind = "math"
)

// Fragment is one Mermaid diagram or math expression whose subprocess
// render is split from the Markdown pass. Render with Options.Defer emits
// a placeholder per fragment instead of waiting on mmdc or node; the
// caller renders fragments later (RenderFragments) and substitutes them
// (ResolveFragments), or serves them one at a time by ID.
type Fragment struct {
	// ID is the content id: a hash of Kind, Source, and the render mode
	// (Format or Display). Equal content yields equal ids across documents,
	// so a rendered fragment is immutable and cacheable by ID alone.
	ID     string
	Kind   FragmentKind
	Source string
	// Display is set for $$ block math (KaTeX display mode).
	Display bool
	// Format is the mmdc output format for Mermaid (MermaidSVG or MermaidPNG).
	Format string
}

// FragmentResult is the rendered HTML of one Fragment, or the reason it
// failed. Rendered HTML is the inner element only: an <svg> or <img> for
// Mermaid (the .mermaid-svg wrapper stays in the page), a KaTeX span for
// math.
type FragmentResult struct {
	HTML string
	Err  error
}

// fragmentIDLen is the hex length of a fragment id (64 bits of SHA-256).
const fragmentIDLen = 16

// FragmentIDRe matches a well-formed Fragment.ID.
var FragmentIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

func fragmentID(kind FragmentKind, mode, source string) string {
	h := sha256.New()
	h.Write([]byte(string(kind)))
	h.Write([]byte{0})
	h.Write([]byte(mode))
	h.Write([]byte{0})
	h.Write([]byte(source))
	return hex.EncodeToString(h.Sum(nil))[:fragmentIDLen]
}

func mathFragment(e mathExpr) Fragment {
	mode := "inline"
	if e.DisplayMode {
		mode = "display"
	}
	return Fragment{ID: fragmentID(FragmentMath, mode, e.Expr), Kind: FragmentMath, Source: e.Expr, Display: e.DisplayMode}
}

func mermaidFragment(d mermaidDiagram, format string) Fragment {
	format = resolveMermaidFormat(format)
	return Fragment{ID: fragmentID(FragmentMermaid, format, d.source), Kind: FragmentMermaid, Source: d.source, Format: format}
}

// Placeholder markup emitted for deferred fragments. The element carries
// the fragment id; its text is the escaped source, which doubles as the
// visible loading state for math and as the failure fallback for both.
const (
	deferredClass        = "vellum-deferred"
	deferredAttr         = "data-vellum-fragment"
	deferredMermaidClass = deferredClass + " " + deferredClass + "-mermaid"
	deferredMathClass    = deferredClass + " " + deferredClass + "-math"
)

// fragmentPlaceholderRe matches one deferred placeholder. The body never
// contains a closing tag (it is escaped text or empty), so the lazy match
// stops at the placeholder's own close.
var fragmentPlaceholderRe = regexp.MustCompile(`(?s)<(?:span|div) class="` + deferredClass + ` ` + deferredClass + `-(mermaid|math)" ` + deferredAttr + `="([0-9a-f]{16})"[^>]*>(.*?)</(?:span|div)>`)

func mermaidPlaceholder(f Fragment) string {
	return `<div class="` + deferredMermaidClass + `" ` + deferredAttr + `="` + f.ID + `" role="img" aria-label="Diagram, loading"><pre>` + htmlEscapeText(f.Source) + `</pre></div>`
}

func mathPlaceholder(f Fragment) string {
	span := `<span class="` + deferredMathClass + `" ` + deferredAttr + `="` + f.ID + `">` + htmlEscapeText(f.Source) + `</span>`
	if f.Display {
		return `<div class="katex-display">` + span + `</div>`
	}
	return span
}

// ExtractFragments lists the fragments Render would defer for src, in
// source order (math first, then Mermaid), without running goldmark or
// any subprocess. The viewer uses it to find a fragment by id when the
// browser asks for one.
func ExtractFragments(src []byte, opts *Options) []Fragment {
	math := newMathPreprocessor()
	mermaidFmt := MermaidSVG
	if opts != nil && opts.MermaidFormat != "" {
		mermaidFmt = opts.MermaidFormat
	}
	mermaid := newMermaidPreprocessor(mermaidFmt)
	mermaid.Extract(math.Extract(string(src)))
	return append(math.fragments(), mermaid.fragments()...)
}

func (m *mathPreprocessor) fragments() []Fragment {
	out := make([]Fragment, 0, len(m.exprs))
	for _, e := range m.exprs {
		out = append(out, mathFragment(e))
	}
	return out
}

func (m *mermaidPreprocessor) fragments() []Fragment {
	out := make([]Fragment, 0, len(m.diagrams))
	for _, d := range m.diagrams {
		out = append(out, mermaidFragment(d, m.format))
	}
	return out
}

// defer replaces the math comment placeholders goldmark passed through
// with deferred placeholder elements.
func (m *mathPreprocessor) deferAll(html string) string {
	for i, p := range m.placeholders {
		html = strings.Replace(html, p, mathPlaceholder(mathFragment(m.exprs[i])), 1)
	}
	return html
}

// deferAll replaces the Mermaid comment placeholders with the .mermaid-svg
// wrapper (scale hint applied) around a deferred placeholder element.
func (m *mermaidPreprocessor) deferAll(html string) string {
	for i, d := range m.diagrams {
		style := ""
		if d.scale != 1.0 {
			style = fmt.Sprintf(` style="max-width: %.0f%%"`, d.scale*100)
		}
		wrapped := `<div class="mermaid-svg"` + style + `>` + mermaidPlaceholder(mermaidFragment(d, m.format)) + `</div>`
		html = strings.Replace(html, m.placeholders[i], wrapped, 1)
	}
	return html
}

// RenderFragments renders frags, deduplicated by ID. All math goes to one
// node process; Mermaid diagrams run concurrently, bounded by CPU count.
//
// A failed Mermaid diagram is reported in its FragmentResult.Err (the
// document still renders with a source fallback). A failed KaTeX batch
// is a hard error: the returned error is set and no math result exists,
// though Mermaid results are still returned.
func RenderFragments(ctx context.Context, frags []Fragment) (map[string]FragmentResult, error) {
	results := make(map[string]FragmentResult, len(frags))
	var math, mermaid []Fragment
	for _, f := range frags {
		if _, seen := results[f.ID]; seen {
			continue
		}
		results[f.ID] = FragmentResult{}
		switch f.Kind {
		case FragmentMath:
			math = append(math, f)
		case FragmentMermaid:
			mermaid = append(mermaid, f)
		default:
			results[f.ID] = FragmentResult{Err: fmt.Errorf("unknown fragment kind %q", f.Kind)}
		}
	}

	var mathErr error
	var mathWG sync.WaitGroup
	var mathOut []string
	if len(math) > 0 {
		exprs := make([]mathExpr, len(math))
		for i, f := range math {
			exprs[i] = mathExpr{Expr: f.Source, DisplayMode: f.Display}
		}
		mathWG.Go(func() {
			mathOut, mathErr = batchKaTeX(ctx, exprs)
		})
	}

	// Each diagram costs a full mmdc process, and mmdc drives Puppeteer,
	// which launches a headless Chromium — so the cost is browser
	// startup per diagram, not markup generation. Rendering them
	// serially made document time linear in diagram count (measured
	// 2026-08-07: 8 diagrams took 3536ms serially against 854ms
	// concurrently, ~440ms apiece). Render concurrently, bounded by CPU
	// count so a diagram-heavy document cannot spawn an unbounded pile
	// of browsers.
	mermaidOut := make([]FragmentResult, len(mermaid))
	sem := make(chan struct{}, min(runtime.NumCPU(), len(mermaid)))
	var wg sync.WaitGroup
	for i, f := range mermaid {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			img, err := renderMermaidFn(ctx, f.Source, f.Format)
			mermaidOut[i] = FragmentResult{HTML: img, Err: err}
		})
	}
	wg.Wait()
	mathWG.Wait()

	for i, f := range mermaid {
		results[f.ID] = mermaidOut[i]
	}
	if mathErr != nil {
		for _, f := range math {
			delete(results, f.ID)
		}
		return results, mathErr
	}
	for i, f := range math {
		results[f.ID] = FragmentResult{HTML: mathOut[i]}
	}
	return results, nil
}

// ResolveFragments substitutes rendered fragments into html. A
// placeholder whose id is absent from results is left in place, so a
// partially rendered document can be completed later. A result with Err
// becomes the source-as-code fallback (<pre class="mermaid-error"> or
// <span class="katex-error">), is logged to stderr, and is reported in
// soft with its 1-based position among fragments of the same kind, in
// document order.
func ResolveFragments(html string, results map[string]FragmentResult) (string, []string) {
	var soft []string
	counts := map[string]int{}
	out := fragmentPlaceholderRe.ReplaceAllStringFunc(html, func(m string) string {
		sub := fragmentPlaceholderRe.FindStringSubmatch(m)
		kind, id, escapedSource := sub[1], sub[2], sub[3]
		counts[kind]++
		res, ok := results[id]
		if !ok {
			return m
		}
		if res.Err == nil {
			return res.HTML
		}
		switch kind {
		case "mermaid":
			// 1-based index for human-facing messages.
			msg := fmt.Sprintf("mermaid diagram %d: %v", counts[kind], res.Err)
			fmt.Fprintln(os.Stderr, "Error:", msg)
			soft = append(soft, msg)
			escapedSource = strings.TrimSuffix(strings.TrimPrefix(escapedSource, "<pre>"), "</pre>")
			return `<pre class="mermaid-error">` + escapedSource + `</pre>`
		default:
			msg := fmt.Sprintf("math expression %d: %v", counts[kind], res.Err)
			fmt.Fprintln(os.Stderr, "Error:", msg)
			soft = append(soft, msg)
			return `<span class="katex-error">` + escapedSource + `</span>`
		}
	})
	return out, soft
}
