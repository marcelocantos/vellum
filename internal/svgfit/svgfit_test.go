// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package svgfit

import (
	"math"
	"strings"
	"testing"
)

const tolerance = 0.01

func near(a, b float64) bool { return math.Abs(a-b) <= tolerance }

func measure(t *testing.T, svg string) Fit {
	t.Helper()
	fit, ok := Measure([]byte(svg))
	if !ok {
		t.Fatalf("Measure refused:\n%s", svg)
	}
	return fit
}

func TestMeasure_NaturalSize(t *testing.T) {
	cases := []struct {
		name, root string
		w, h       float64
	}{
		{"width and height", `<svg width="300" height="100" viewBox="0 0 600 200">`, 300, 100},
		{"units", `<svg width="72pt" height="1in" viewBox="0 0 10 10">`, 96, 96},
		{"millimetres", `<svg width="25.4mm" height="50.8mm" viewBox="0 0 10 20">`, 96, 192},
		{"width only", `<svg width="300" viewBox="0 0 600 200">`, 300, 100},
		{"height only", `<svg height="50" viewBox="0 0 600 200">`, 150, 50},
		{"viewBox only", `<svg viewBox="0 0 640 480">`, 640, 480},
		{"comma viewBox", `<svg viewBox="0,0,640,480">`, 640, 480},
		{"mermaid", `<svg width="100%" style="max-width: 919.281px; background-color: white;" viewBox="4 4 919.28125 187">`, 919.281, 187},
	}
	for _, c := range cases {
		fit := measure(t, c.root+`</svg>`)
		if !near(fit.Width, c.w) || !near(fit.Height, c.h) {
			t.Errorf("%s: got %.3f x %.3f, want %.3f x %.3f", c.name, fit.Width, fit.Height, c.w, c.h)
		}
		if fit.Small != 0 || fit.Typical != 0 || fit.GrowEm() != 0 {
			t.Errorf("%s: a drawing with no text reported text sizes %+v", c.name, fit)
		}
	}
}

func TestMeasure_RefusesWhatDoesNotScale(t *testing.T) {
	for _, svg := range []string{
		``,
		`<p>not a drawing</p>`,
		`<svg width="300" height="100"><text>no viewBox</text></svg>`,
		`<svg viewBox="0 0 0 100"></svg>`,
		`<svg viewBox="0 0 100"></svg>`,
		`<svg viewBox="0 0 -5 100"></svg>`,
	} {
		if fit, ok := Measure([]byte(svg)); ok {
			t.Errorf("Measure accepted %q: %+v", svg, fit)
		}
	}
	if _, ok := Measure([]byte(`<svg viewBox="0 0 10 10">` + strings.Repeat(" ", MaxBytes) + `</svg>`)); ok {
		t.Error("Measure read a drawing over MaxBytes")
	}
}

func TestMeasure_FontSizeSources(t *testing.T) {
	const open = `<svg viewBox="0 0 100 100" width="100" height="100">`
	cases := []struct {
		name, body string
		want       float64
	}{
		{"browser default", `<text>abc</text>`, 16},
		{"attribute", `<text font-size="10">abc</text>`, 10},
		{"attribute with unit", `<text font-size="9pt">abc</text>`, 12},
		{"inline style", `<text style="fill:red; font-size: 11px">abc</text>`, 11},
		{"style beats attribute", `<text font-size="10" style="font-size:14px">abc</text>`, 14},
		{"inherited", `<g font-size="20"><g><text>abc</text></g></g>`, 20},
		{"em of parent", `<g font-size="20"><text font-size="0.5em">abc</text></g>`, 10},
		{"percent of parent", `<g font-size="20"><text style="font-size:150%">abc</text></g>`, 30},
		{"keyword", `<text font-size="small">abc</text>`, 13},
		{"tspan", `<text font-size="10"><tspan font-size="30">abcdefgh</tspan></text>`, 30},
		{"font shorthand", `<text style="font: bold 12px/1.4 Arial">abc</text>`, 12},
		{"shorthand weight is not a size", `<text style="font: 400 13px Arial">abc</text>`, 13},
		{"unreadable value falls through", `<text font-size="10" style="font-size:calc(1px + 2vw)">abc</text>`, 10},
		{"rule by id", `<style>#a{font-size:7px}</style><text id="a">abc</text>`, 7},
		{"rule by class", `<style>.big{font-size:40px}</style><text class="small big">abc</text>`, 40},
		{"rule by tag", `<style>text{font-size:9px}</style><text>abc</text>`, 9},
		{"rule beats attribute", `<style>text{font-size:9px}</style><text font-size="30">abc</text>`, 9},
		{"style beats rule", `<style>text{font-size:9px}</style><text style="font-size:21px">abc</text>`, 21},
		{"important beats style", `<style>text{font-size:9px !important}</style><text style="font-size:21px">abc</text>`, 9},
		{"specificity", `<style>.c{font-size:8px} text{font-size:30px}</style><text class="c">abc</text>`, 8},
		{"later rule wins a tie", `<style>.c{font-size:8px} .d{font-size:25px}</style><text class="c d">abc</text>`, 25},
		{"descendant", `<style>.box text{font-size:6px}</style><g class="box"><g><text>abc</text></g></g>`, 6},
		{"child", `<style>.box > text{font-size:6px}</style><g class="box"><text>abc</text></g>`, 6},
		{"child does not skip a level", `<style>.box > text{font-size:6px}</style><g class="box"><g><text>abc</text></g></g>`, 16},
		{"selector list", `<style>.x, #a .y text{font-size:5px}</style><g id="a"><g class="y"><text>abc</text></g></g>`, 5},
		{"unsupported selector is skipped", `<style>text:hover{font-size:99px} text[x]{font-size:98px}</style><text>abc</text>`, 16},
		{"at-rules are skipped", `<style>@media print{text{font-size:99px}} @font-face{font-family:x;src:url(data:font/woff2;base64,AA==)} text{font-size:12px}</style><text>abc</text>`, 12},
		{"comment and CDATA", `<style><![CDATA[ /* text{font-size:99px} */ text{font-size:12px} ]]></style><text>abc</text>`, 12},
		{"foreignObject html", `<style>#m{font-size:14px}</style><g id="m"><foreignObject width="50" height="20"><div><span class="nodeLabel"><p>abc</p></span></div></foreignObject></g>`, 14},
		{"foreignObject inline style", `<foreignObject width="50" height="20"><div style="font-size: 12px">abc</div></foreignObject>`, 12},
	}
	for _, c := range cases {
		fit := measure(t, open+c.body+`</svg>`)
		if !near(fit.Typical, c.want) || !near(fit.Small, c.want) {
			t.Errorf("%s: got small %.2f typical %.2f, want %.2f", c.name, fit.Small, fit.Typical, c.want)
		}
	}
}

func TestMeasure_ScaleOfTheDrawing(t *testing.T) {
	cases := []struct {
		name, svg string
		want      float64
	}{
		{"viewBox drawn at half size", `<svg width="50" height="50" viewBox="0 0 100 100"><text font-size="20">abc</text></svg>`, 10},
		{"millimetre units", `<svg width="210mm" height="297mm" viewBox="0 0 210 297"><text font-size="4">abc</text></svg>`, 4 * 96 / 25.4},
		{"points, as Graphviz writes", `<svg width="206pt" height="260pt" viewBox="0 0 206 260"><text font-size="14">abc</text></svg>`, 14 * 96.0 / 72},
		{"the smaller ratio", `<svg width="200" height="50" viewBox="0 0 100 100"><text font-size="20">abc</text></svg>`, 10},
		{"scale", `<svg viewBox="0 0 100 100"><g transform="translate(5 5) scale(2)"><text font-size="10">abc</text></g></svg>`, 20},
		{"uneven scale", `<svg viewBox="0 0 100 100"><g transform="scale(4, 1)"><text font-size="10">abc</text></g></svg>`, 20},
		{"matrix", `<svg viewBox="0 0 100 100"><g transform="matrix(0 3 -3 0 10 10)"><text font-size="10">abc</text></g></svg>`, 30},
		{"nested transforms", `<svg viewBox="0 0 100 100"><g transform="scale(2)"><g transform="rotate(45) scale(0.25)"><text font-size="10">abc</text></g></g></svg>`, 5},
		{"transform on the text", `<svg viewBox="0 0 100 100"><text font-size="10" transform="scale(3)">abc</text></svg>`, 30},
		{"nested svg", `<svg viewBox="0 0 100 100"><svg width="50" height="50" viewBox="0 0 200 200"><text font-size="40">abc</text></svg></svg>`, 10},
	}
	for _, c := range cases {
		fit := measure(t, c.svg)
		if !near(fit.Typical, c.want) {
			t.Errorf("%s: typical %.3f, want %.3f", c.name, fit.Typical, c.want)
		}
	}
}

func TestMeasure_OnlyDrawnTextCounts(t *testing.T) {
	fit := measure(t, `<svg viewBox="0 0 100 100">
		<title>a long title that is never drawn</title>
		<desc>a description</desc>
		<metadata>metadata words</metadata>
		<style>text{font-size:10px}</style>
		<defs><text font-size="90">defined, not drawn</text></defs>
		<g display="none"><text font-size="80">hidden</text></g>
		<g style="visibility: hidden"><text font-size="70">hidden too</text></g>
		loose characters outside any text element
		<text>drawn</text>
	</svg>`)
	if !near(fit.Small, 10) || !near(fit.Typical, 10) {
		t.Errorf("got %+v, want only the 10px text counted", fit)
	}
}

// The small print is the size a tenth of the characters fall below, so a
// stray label does not stand for the drawing, and a real body of small
// text does.
func TestMeasure_SmallPrintAndTypical(t *testing.T) {
	text := func(size string, chars int) string {
		return `<text font-size="` + size + `">` + strings.Repeat("x", chars) + `</text>`
	}
	const open = `<svg viewBox="0 0 100 100">`
	cases := []struct {
		name, body     string
		small, typical float64
	}{
		{"uniform", text("12", 50), 12, 12},
		{"a stray watermark is ignored", text("16", 95) + text("4", 5), 16, 16},
		{"a body of small text counts", text("16", 80) + text("8", 20), 8, 16},
		{"mostly small", text("20", 30) + text("9", 70), 9, 9},
		{"three sizes", text("20", 10) + text("14", 60) + text("10", 30), 10, 14},
		{"spaces carry no weight", text("16", 80) + `<text font-size="4">` + strings.Repeat(" ", 500) + `xx</text>`, 16, 16},
	}
	for _, c := range cases {
		fit := measure(t, open+c.body+`</svg>`)
		if !near(fit.Small, c.small) || !near(fit.Typical, c.typical) {
			t.Errorf("%s: got small %.2f typical %.2f, want %.2f and %.2f", c.name, fit.Small, fit.Typical, c.small, c.typical)
		}
	}
}

// GrowEm is the policy: growth stops when the small print reaches
// SmallPrintEm or the typical text reaches TypicalEm, whichever is first.
// The caller draws at the larger of that and the natural width, so a
// value under the natural width (in ems of 16px text here) means "stay".
func TestGrowEm(t *testing.T) {
	const pagePx = 16.0
	cases := []struct {
		name           string
		small, typical float64
		wantScale      float64 // grown width over natural width, before the caller's floor of 1
	}{
		{"text already at reading size", 16, 16, 0.75},
		{"mermaid's 14px", 14, 14, 12.0 / 14},
		{"uniform 8px grows to 12px, not to 16px", 8, 8, 1.5},
		{"typical text stops it first", 10, 14, 16.0 / 14},
		{"small print stops it first", 6, 7, 2},
		{"tiny notes under large labels do not inflate the labels", 6, 16, 1},
	}
	for _, c := range cases {
		fit := Fit{Width: 400, Height: 300, Small: c.small, Typical: c.typical}
		got := fit.GrowEm() * pagePx / fit.Width
		if !near(got, c.wantScale) {
			t.Errorf("%s: scale %.3f, want %.3f", c.name, got, c.wantScale)
		}
	}
	if got := (Fit{Width: 400, Height: 300}).GrowEm(); got != 0 {
		t.Errorf("a drawing with no text grows to %.2fem, want 0", got)
	}
}

func TestMeasure_StandaloneFile(t *testing.T) {
	fit := measure(t, `<?xml version="1.0" encoding="UTF-8" standalone="no"?>
<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd">
<!-- Generated by a drawing tool -->
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="400px" height="200px" viewBox="0 0 800 400" version="1.1">
  <g id="graph0" class="graph" transform="scale(1 1) rotate(0) translate(4 396)">
    <text text-anchor="middle" x="100" y="-50" font-family="Times,serif" font-size="28.00">node</text>
  </g>
</svg>`)
	if !near(fit.Width, 400) || !near(fit.Height, 200) || !near(fit.Typical, 14) {
		t.Errorf("got %+v, want 400x200 with 14px text", fit)
	}
}
