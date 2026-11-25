// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A 300px drawing with 10px text: growth stops when that text reaches
// 0.75em, at 300 * 0.75/10 = 22.5em.
const (
	fitSampleSVG   = `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="100" viewBox="0 0 300 100"><text font-size="10">some label text</text></svg>`
	fitSampleStyle = `--vellum-svg-natural: 300.00px; --vellum-svg-ems: 22.50`
	// fitSampleSVGFitted is fitSampleSVG drawn inline and fitted.
	fitSampleSVGFitted = `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="100" viewBox="0 0 300 100" style="` + fitSampleStyle + `"><text font-size="10">some label text</text></svg>`
	// fitSampleDataURL and fitSampleBase64 are fitSampleSVG as data: URL payloads.
	fitSampleDataURL = `%3Csvg%20xmlns=%22http://www.w3.org/2000/svg%22%20width=%22300%22%20height=%22100%22%20viewBox=%220%200%20300%20100%22%3E%3Ctext%20font-size=%2210%22%3Esome%20label%20text%3C/text%3E%3C/svg%3E`
	fitSampleBase64  = `PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHdpZHRoPSIzMDAiIGhlaWdodD0iMTAwIiB2aWV3Qm94PSIwIDAgMzAwIDEwMCI+PHRleHQgZm9udC1zaXplPSIxMCI+c29tZSBsYWJlbCB0ZXh0PC90ZXh0Pjwvc3ZnPg==`
)

func fitFixture(t *testing.T) (s *Server, dir, md string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(base, "docs")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"small.svg":                        fitSampleSVG,
		"my drawing.svg":                   fitSampleSVG,
		"UPPER.SVG":                        fitSampleSVG,
		"fixed.svg":                        `<svg xmlns="http://www.w3.org/2000/svg" width="300" height="100"><text font-size="10">no viewBox</text></svg>`,
		"pic.png":                          "not really a png",
		filepath.Join("..", "outside.svg"): fitSampleSVG,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &Server{CacheDir: filepath.Join(base, "cache"), Roots: []string{dir}}, dir, filepath.Join(dir, "doc.md")
}

func TestFitFigures(t *testing.T) {
	s, _, md := fitFixture(t)
	fitted := func(attrs, img string) string {
		return `<p` + attrs + ` data-vellum-figure="">` + img + ` style="` + fitSampleStyle + `"></p>`
	}
	cases := []struct {
		name, in, want string
	}{
		{"a figure", `<p><img src="small.svg" alt="a"/></p>`, fitted("", `<img src="small.svg" alt="a"`)},
		{"paragraph attributes stay", `<p data-vellum-line="3"> <img src="small.svg"> </p>`, `<p data-vellum-line="3" data-vellum-figure=""> <img src="small.svg" style="` + fitSampleStyle + `"> </p>`},
		{"an escaped name", `<p><img src="my%20drawing.svg"></p>`, fitted("", `<img src="my%20drawing.svg"`)},
		{"extension in capitals", `<p><img src="UPPER.SVG"></p>`, fitted("", `<img src="UPPER.SVG"`)},
		{"an existing style is kept", `<p><img src="small.svg" style="text-align: center"></p>`,
			`<p data-vellum-figure=""><img src="small.svg" style="` + fitSampleStyle + `; text-align: center"></p>`},
		{"an image in a sentence", `<p>See <img src="small.svg"> here.</p>`, ""},
		{"two images", `<p><img src="small.svg"><img src="small.svg"></p>`, ""},
		{"a linked image", `<p><a href="x"><img src="small.svg"></a></p>`,
			`<p data-vellum-figure=""><a href="x" data-vellum-fit-wrap=""><img src="small.svg" style="` + fitSampleStyle + `"></a></p>`},
		{"in a picture in a centred div", `<div align="center"><picture><source srcset="small.svg"><img src="small.svg"></picture></div>`,
			`<div align="center" data-vellum-figure=""><picture data-vellum-fit-wrap=""><source srcset="small.svg"><img src="small.svg" style="` + fitSampleStyle + `"></picture></div>`},
		{"a figure with a caption", `<figure><img src="small.svg"><figcaption>A caption</figcaption></figure>`,
			`<figure data-vellum-figure=""><img src="small.svg" style="` + fitSampleStyle + `"><figcaption>A caption</figcaption></figure>`},
		{"a linked image beside text", `<p><a href="x"><img src="small.svg"> Home</a></p>`, ""},
		{"sized by the author", `<p><img src="small.svg" width="100"></p>`, ""},
		{"sized by a style", `<p><img src="small.svg" style="width: 50%"></p>`, ""},
		{"not an SVG", `<p><img src="pic.png"></p>`, ""},
		{"no viewBox", `<p><img src="fixed.svg"></p>`, ""},
		{"from the web, measured later", `<p><img src="https://example.com/small.svg"></p>`,
			`<p data-vellum-figure-pending=""><img src="https://example.com/small.svg" data-vellum-fit-pending=""></p>`},
		{"a raster from the web", `<p><img src="https://example.com/photo.PNG"></p>`, ""},
		{"a data URL", `<p><img src="data:image/svg+xml,` + fitSampleDataURL + `"></p>`,
			`<p data-vellum-figure=""><img src="data:image/svg+xml,` + fitSampleDataURL + `" style="` + fitSampleStyle + `"></p>`},
		{"a base64 data URL", `<p><img src="data:image/svg+xml;base64,` + fitSampleBase64 + `"></p>`,
			`<p data-vellum-figure=""><img src="data:image/svg+xml;base64,` + fitSampleBase64 + `" style="` + fitSampleStyle + `"></p>`},
		{"a data URL with no viewBox", `<p><img src="data:image/svg+xml,%3Csvg%3E"></p>`, ""},
		{"a missing file", `<p><img src="gone.svg"></p>`, ""},
		{"outside the served folders", `<p><img src="../outside.svg"></p>`, ""},

		{"an inline drawing alone in a paragraph", `<p>` + fitSampleSVG + `</p>`,
			`<p data-vellum-figure="">` + fitSampleSVGFitted + `</p>`},
		{"inline drawings among blocks", `<article><h1>T</h1>` + fitSampleSVG + "\n<p>x</p>\n" + fitSampleSVG + `</article>`,
			`<article><h1>T</h1>` + figureWrapOpen + fitSampleSVGFitted + figureWrapClose + "\n<p>x</p>\n" + figureWrapOpen + fitSampleSVGFitted + figureWrapClose + `</article>`},
		// Side by side on one line, as a browser draws them.
		{"two drawings between blocks", `<article><h1>T</h1>` + fitSampleSVG + "\n" + fitSampleSVG + `<p>x</p></article>`, ""},
		{"an inline drawing in a sentence", `<p>An icon ` + fitSampleSVG + ` in text.</p>`, ""},
		{"an inline drawing beside another", `<p>` + fitSampleSVG + fitSampleSVG + `</p>`, ""},
		{"an inline drawing sized by a style", `<p><svg style="width: 10em" viewBox="0 0 300 100"><text font-size="10">x</text></svg></p>`, ""},
		{"an inline drawing at a percentage", `<p><svg width="100%" viewBox="0 0 300 100"><text font-size="10">x</text></svg></p>`, ""},
		{"an inline drawing with no viewBox", `<p><svg width="300" height="100"><text font-size="10">x</text></svg></p>`, ""},
		{"a diagram the viewer drew", `<div class="mermaid-svg">` + fitSampleSVG + `</div>`, ""},
		{"a list item's drawing", `<ul><li>` + fitSampleSVG + `</li></ul>`,
			`<ul><li>` + figureWrapOpen + fitSampleSVGFitted + figureWrapClose + `</li></ul>`},
	}
	for _, c := range cases {
		want := c.want
		if want == "" {
			want = c.in
		}
		if got := s.fitFigures(c.in, md, false); got != want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, want)
		}
	}
}

// A file's measurement is remembered while the file is unchanged and
// taken again when it changes.
func TestFitFigures_FollowsTheFile(t *testing.T) {
	s, dir, md := fitFixture(t)
	const page = `<p><img src="small.svg"></p>`
	if got := s.fitFigures(page, md, false); !strings.Contains(got, fitSampleStyle) {
		t.Fatalf("first read: %s", got)
	}
	wider := strings.Replace(fitSampleSVG, `width="300" height="100"`, `width="600" height="200"`, 1)
	path := filepath.Join(dir, "small.svg")
	if err := os.WriteFile(path, []byte(wider), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	// Twice the width, and the same text drawn at twice the size (20px),
	// so growth stops at the same 22.5em.
	if got := s.fitFigures(page, md, false); !strings.Contains(got, `--vellum-svg-natural: 600.00px; --vellum-svg-ems: 22.50`) {
		t.Fatalf("after the file changed: %s", got)
	}
}

func TestFitDiagram(t *testing.T) {
	s := &Server{}
	cases := []struct {
		name, in, want string
	}{
		{"mermaid's root", sampleMermaidSVG, strings.Replace(sampleMermaidSVG, `style="`, `style="`+sampleMermaidFit+`; `, 1)},
		{"a root with no style", `<svg viewBox="0 0 300 100" width="300"><text font-size="10">label</text></svg>`,
			`<svg viewBox="0 0 300 100" width="300" style="` + fitSampleStyle + `"><text font-size="10">label</text></svg>`},
		{"math", `<span class="katex"><svg viewBox="0 0 10 10"></svg></span>`, ""},
		{"no viewBox", `<svg width="300" height="100"><text>label</text></svg>`, ""},
		{"an error fallback", `<pre class="mermaid-error">graph TD</pre>`, ""},
	}
	for i, c := range cases {
		want := c.want
		if want == "" {
			want = c.in
		}
		if got := s.fitDiagram(string(rune('a'+i)), c.in); got != want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, want)
		}
	}
}

// The page a browser gets marks the figure, in the viewer and inside
// T3 Code, where the image's URL has been rewritten by then.
func TestServer_PageFitsSVGFigures(t *testing.T) {
	s, dir, md := fitFixture(t)
	if err := os.WriteFile(md, []byte("# Doc\n\n![drawing](small.svg)\n\nText with ![inline](small.svg) inside.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := startTestServer(t, s)
	status, _, page := getText(t, ViewURL(ts.URL, md))
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if n := strings.Count(page, `data-vellum-figure=""`); n != 1 {
		t.Fatalf("%d figures marked, want only the image that stands alone:\n%s", n, page)
	}
	if n := strings.Count(page, `style="`+fitSampleStyle+`"`); n != 1 {
		t.Fatalf("%d images sized, want 1", n)
	}

	embedded := httptest.NewServer(s.T3Handler())
	t.Cleanup(embedded.Close)
	status, _, page = getText(t, embedded.URL+t3DocPrefix+t3Token(md))
	if status != http.StatusOK {
		t.Fatalf("embedded status %d", status)
	}
	want := `src="../f/` + t3Token(filepath.Join(dir, "small.svg")) + `" alt="drawing" style="` + fitSampleStyle + `"`
	if !strings.Contains(page, want) {
		t.Fatalf("embedded page lacks the sized figure %s:\n%s", want, page)
	}
}
