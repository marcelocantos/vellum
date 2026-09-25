// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/testdeps"
)

func TestApplyTOC_StyleInjectsAtFront(t *testing.T) {
	on := true
	body := `<h1 id="alpha">Alpha</h1>
<h2 id="beta">Beta</h2>
<p>Hello.</p>
`
	got, injected := applyTOC(body, &Style{TOC: &on})
	if !injected {
		t.Fatal("want injected")
	}
	if !strings.HasPrefix(got, `<div class="vellum-contents">`) {
		t.Fatalf("TOC should lead the body:\n%s", got)
	}
	assertTOCShape(t, got)
	if strings.Count(got, `class="vellum-contents"`) != 1 {
		t.Fatalf("want one Contents block:\n%s", got)
	}
	if !strings.Contains(got, `<a href="#alpha">Alpha</a>`) {
		t.Fatalf("missing h1 link:\n%s", got)
	}
	if !strings.Contains(got, `<a href="#beta">Beta</a>`) {
		t.Fatalf("missing h2 link:\n%s", got)
	}
	// Nested: beta under alpha.
	alpha := strings.Index(got, `href="#alpha"`)
	beta := strings.Index(got, `href="#beta"`)
	if alpha < 0 || beta < 0 || beta < alpha {
		t.Fatalf("beta should follow alpha in the list:\n%s", got)
	}
	inner := got[alpha:beta]
	if !strings.Contains(inner, "<ul>") {
		t.Fatalf("beta should nest under alpha:\n%s", got)
	}
}

func TestApplyTOC_HintReplacesPlaceholder(t *testing.T) {
	body := `<p>Intro.</p>
<!-- vellum:toc -->
<h1 id="one">One</h1>
<h2 id="two">Two</h2>
`
	got, injected := applyTOC(body, nil)
	if !injected {
		t.Fatal("want injected")
	}
	if strings.Contains(got, "vellum:toc") {
		t.Fatalf("hint should be consumed:\n%s", got)
	}
	intro := strings.Index(got, "<p>Intro.</p>")
	toc := strings.Index(got, `class="vellum-contents"`)
	one := strings.Index(got, `<h1 id="one">`)
	if intro < 0 || toc < 0 || one < 0 || !(intro < toc && toc < one) {
		t.Fatalf("TOC should sit between intro and h1:\n%s", got)
	}
	assertTOCShape(t, got)
}

func TestApplyTOC_HintWinsOverStyle(t *testing.T) {
	on := true
	body := `<p>Intro.</p>
<!-- vellum:toc -->
<h1 id="one">One</h1>
`
	got, injected := applyTOC(body, &Style{TOC: &on})
	if !injected {
		t.Fatal("want injected")
	}
	if strings.Count(got, `class="vellum-contents"`) != 1 {
		t.Fatalf("want exactly one Contents block:\n%s", got)
	}
	if strings.HasPrefix(strings.TrimSpace(got), `<div class="vellum-contents">`) {
		t.Fatalf("style.toc must not also prepend when a hint is present:\n%s", got)
	}
}

func TestApplyTOC_NoHeadingsOmitsAndStripsHint(t *testing.T) {
	on := true
	body := `<p>Just a paragraph.</p>`
	got, injected := applyTOC(body, &Style{TOC: &on})
	if injected {
		t.Fatal("no headings: should not inject")
	}
	if got != body {
		t.Fatalf("body mutated without TOC:\n%s", got)
	}

	hinted := `<!-- vellum:toc -->
<p>Just a paragraph.</p>`
	got, injected = applyTOC(hinted, nil)
	if injected {
		t.Fatal("no headings: hint should not inject")
	}
	if strings.Contains(got, "vellum:toc") {
		t.Fatalf("hint should be stripped:\n%s", got)
	}
	if strings.Contains(got, "vellum-contents") {
		t.Fatalf("no Contents block:\n%s", got)
	}
}

func TestApplyTOC_DefaultOff(t *testing.T) {
	body := `<h1 id="alpha">Alpha</h1>`
	got, injected := applyTOC(body, nil)
	if injected {
		t.Fatal("default: no TOC")
	}
	if got != body {
		t.Fatalf("body mutated:\n%s", got)
	}
	off := false
	got, injected = applyTOC(body, &Style{TOC: &off})
	if injected {
		t.Fatal("explicit off: no TOC")
	}
	if got != body {
		t.Fatalf("body mutated:\n%s", got)
	}
}

func TestApplyTOC_DepthThreeOmitsH4(t *testing.T) {
	on := true
	body := `<h1 id="a">A</h1>
<h2 id="b">B</h2>
<h3 id="c">C</h3>
<h4 id="d">D</h4>
`
	got, injected := applyTOC(body, &Style{TOC: &on})
	if !injected {
		t.Fatal("want injected")
	}
	if strings.Contains(got, `href="#d"`) {
		t.Fatalf("h4 should be omitted:\n%s", got)
	}
	if !strings.Contains(got, `href="#c"`) {
		t.Fatalf("h3 should be included:\n%s", got)
	}
}

func TestApplyTOC_ContentsHeadingNotListed(t *testing.T) {
	on := true
	body := `<h1 id="alpha">Alpha</h1>`
	got, _ := applyTOC(body, &Style{TOC: &on})
	if strings.Contains(got, `>Contents</a>`) {
		t.Fatalf("Contents heading must not appear as a TOC entry:\n%s", got)
	}
}

func TestApplyTOC_StripsMarkupInHeadingText(t *testing.T) {
	on := true
	body := `<h1 id="hello-world">Hello <strong>world</strong></h1>`
	got, injected := applyTOC(body, &Style{TOC: &on})
	if !injected {
		t.Fatal("want injected")
	}
	if !strings.Contains(got, `<a href="#hello-world">Hello world</a>`) {
		t.Fatalf("want flattened heading text:\n%s", got)
	}
}

func TestApplyTOC_SecondHintStripped(t *testing.T) {
	body := `<!-- vellum:toc -->
<h1 id="a">A</h1>
<!-- vellum:toc -->
`
	got, injected := applyTOC(body, nil)
	if !injected {
		t.Fatal("want injected")
	}
	if strings.Count(got, `class="vellum-contents"`) != 1 {
		t.Fatalf("want one Contents block:\n%s", got)
	}
	if strings.Contains(got, "vellum:toc") {
		t.Fatalf("leftover hint:\n%s", got)
	}
}

func TestRender_TOCFromStyle(t *testing.T) {
	on := true
	html, _, err := Render(context.Background(), []byte("# Alpha\n\n## Beta\n\nHello.\n"), &Options{Style: &Style{TOC: &on}})
	if err != nil {
		t.Fatal(err)
	}
	assertTOCShape(t, html)
	if strings.Contains(strings.ToLower(html), "<script") {
		t.Fatalf("convert HTML must not contain script:\n%s", html)
	}
	if strings.Contains(html, "javascript:") {
		t.Fatal("javascript: URL in convert HTML")
	}
	if !strings.Contains(html, "leader(dotted)") || !strings.Contains(html, "target-counter(attr(href), page)") {
		t.Fatalf("paged-media TOC CSS missing:\n%s", html)
	}
	// Contents is in the body, not a TOC entry.
	if strings.Contains(html, `href="#contents"`) || strings.Contains(html, `>Contents</a>`) {
		t.Fatalf("Contents listed in TOC:\n%s", html)
	}
}

func TestRender_TOCFromHint(t *testing.T) {
	src := "# Title\n\n<!-- vellum:toc -->\n\n## Section\n\nBody.\n"
	html, _, err := Render(context.Background(), []byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertTOCShape(t, html)
	if strings.Contains(html, "vellum:toc") {
		t.Fatalf("hint leaked into HTML:\n%s", html)
	}
	title := strings.Index(html, `<h1 id="title">`)
	toc := strings.Index(html, `class="vellum-contents"`)
	section := strings.Index(html, `<h2 id="section">`)
	if title < 0 || toc < 0 || section < 0 || !(title < toc && toc < section) {
		t.Fatalf("TOC should sit between title and section:\n%s", html)
	}
	if strings.Contains(strings.ToLower(html), "<script") {
		t.Fatalf("convert HTML must not contain script:\n%s", html)
	}
}

func TestRender_TOCHintInsideFenceUntouched(t *testing.T) {
	src := "# Title\n\n```\n<!-- vellum:toc -->\n```\n\n## Section\n"
	html, _, err := Render(context.Background(), []byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, `class="vellum-contents"`) {
		t.Fatalf("fenced hint must not inject TOC:\n%s", html)
	}
	if !strings.Contains(html, "vellum:toc") {
		t.Fatalf("fenced hint should remain visible as text:\n%s", html)
	}
}

func TestRender_NoTOCByDefault(t *testing.T) {
	html, _, err := Render(context.Background(), []byte("# Title\n\nHello.\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "vellum-contents") {
		t.Fatalf("default render grew a TOC:\n%s", html)
	}
}

func TestRender_NoMathOmitsKaTeXCDN(t *testing.T) {
	html, _, err := Render(context.Background(), []byte("# Tasks\n\n- [ ] one\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "cdn.jsdelivr.net") {
		t.Fatalf("no-math HTML still fetches KaTeX CSS:\n%s", html)
	}
}

func TestConvert_TOCAppearsInPDF(t *testing.T) {
	testdeps.Need(t, "pdftotext")
	on := true
	src := "# Alpha\n\n## Beta\n\nHello from the TOC PDF test.\n"
	for _, backend := range backends(t) {
		t.Run(backend, func(t *testing.T) {
			dir := t.TempDir()
			in := filepath.Join(dir, "doc.md")
			out := filepath.Join(dir, "doc.pdf")
			if err := os.WriteFile(in, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			opts := &Options{Backend: backend, Style: &Style{TOC: &on}}
			if err := Convert(context.Background(), in, out, opts); err != nil {
				t.Fatalf("Convert: %v", err)
			}
			txt := filepath.Join(dir, "doc.txt")
			cmd := exec.Command("pdftotext", out, txt)
			if o, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("pdftotext: %v: %s", err, string(o))
			}
			body, err := os.ReadFile(txt)
			if err != nil {
				t.Fatal(err)
			}
			text := string(body)
			for _, want := range []string{"Contents", "Alpha", "Beta", "Hello from the TOC PDF test"} {
				if !strings.Contains(text, want) {
					t.Errorf("extracted text missing %q; got:\n%s", want, text)
				}
			}
		})
	}
}

func TestRun_HTMLContentIncludesTOC(t *testing.T) {
	on := true
	dir := t.TempDir()
	in := filepath.Join(dir, "in.md")
	if err := os.WriteFile(in, []byte("# Hi\n\n## There\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), &Request{
		From:  Endpoint{Media: MediaFile, Path: in},
		To:    Endpoint{Media: MediaContent, Format: FormatHTML},
		Style: &Style{TOC: &on},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTOCShape(t, res.Content)
	if strings.Contains(strings.ToLower(res.Content), "<script") {
		t.Fatalf("HTML content sink must not contain script:\n%s", res.Content)
	}
}

func assertTOCShape(t *testing.T, html string) {
	t.Helper()
	if !strings.Contains(html, `<div class="vellum-contents">`) {
		t.Fatalf("missing Contents wrapper:\n%s", html)
	}
	if !strings.Contains(html, "<h2>Contents</h2>") {
		t.Fatalf("missing Contents heading:\n%s", html)
	}
	if !strings.Contains(html, "<ul>") || !strings.Contains(html, "<li>") {
		t.Fatalf("Contents is not a nested list:\n%s", html)
	}
}
