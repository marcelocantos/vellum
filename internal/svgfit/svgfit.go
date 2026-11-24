// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package svgfit decides how wide an SVG should be drawn on a page of
// text. An SVG has a natural size, but a page often has more room than
// that, and a drawing squeezed into a narrow column loses its labels.
// The measure that keeps a small drawing from ballooning is its own
// text: growing stops once that text is as large as the page's.
//
// Measure reads the text sizes without a browser. It follows the cascade
// far enough for what drawing tools write: font-size as a presentation
// attribute, in an inline style, and in <style> rules whose selectors
// are made of tags, ids and classes; inherited sizes; em and percentage
// sizes; the scale of transform attributes and of the viewBox. Anything
// it cannot read (a rule with a pseudo-class, a size from calc()) is
// skipped, so its answer is an estimate, good to the precision a layout
// needs.
package svgfit

import (
	"bytes"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	// MaxBytes is the largest SVG Measure reads. A larger one is left at
	// its natural size.
	MaxBytes = 8 << 20

	// SmallPrintEm and TypicalEm are where growing stops, in ems of the
	// page's text: when the drawing's small print reaches SmallPrintEm or
	// its typical text reaches TypicalEm, whichever comes first. The first
	// is the smallest size that reads comfortably; the second keeps a
	// drawing's labels from outgrowing the prose around it.
	SmallPrintEm = 0.75
	TypicalEm    = 1.0

	// smallPrintShare is the share of a drawing's characters that may be
	// smaller than its small print. A lone watermark or footnote does not
	// set the size of everything else.
	smallPrintShare = 0.10
	typicalShare    = 0.50

	// rootFontPx is the font size an SVG starts from: the browser default,
	// and the size of the viewer's text.
	rootFontPx = 16.0

	cssPxPerInch = 96.0
	ptPerInch    = 72.0
	pxPerPica    = 16.0
	cmPerInch    = 2.54
	mmPerInch    = 25.4
	// exPerEm approximates an x-height, as browsers do without font data.
	exPerEm = 0.5
)

// Fit is what a page needs to size one SVG.
type Fit struct {
	// Width and Height are the natural size in CSS pixels.
	Width, Height float64
	// Small and Typical are text sizes in CSS pixels at the natural size:
	// the small print (all but a tenth of the characters are at least this
	// large) and the median character. Both are 0 for a drawing with no
	// text.
	Small, Typical float64
}

// GrowEm is the width, in ems of the page's text, up to which the SVG may
// be drawn larger than natural. It is 0 for a drawing with no text. The
// caller draws at the larger of this and the natural width, so a drawing
// whose text is already large simply stays natural.
func (f Fit) GrowEm() float64 {
	if f.Small <= 0 || f.Typical <= 0 {
		return 0
	}
	return f.Width * math.Min(SmallPrintEm/f.Small, TypicalEm/f.Typical)
}

// Measure reads svg's natural size and text sizes. ok is false when it
// has no usable viewBox: such a drawing does not scale with its box.
func Measure(svg []byte) (fit Fit, ok bool) {
	if len(svg) > MaxBytes {
		return Fit{}, false
	}
	body := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(bytes.NewReader(svg), body)
	if err != nil {
		return Fit{}, false
	}
	var root *html.Node
	for _, n := range nodes {
		if root = findElement(n, "svg"); root != nil {
			break
		}
	}
	if root == nil {
		return Fit{}, false
	}
	box, ok := viewBox(attr(root, "viewbox"))
	if !ok {
		return Fit{}, false
	}
	fit.Width, fit.Height = naturalSize(root, box)
	if !(fit.Width > 0) || !(fit.Height > 0) {
		return Fit{}, false
	}

	w := walker{rules: fontRules(root)}
	// The viewBox is fitted inside the natural box (preserveAspectRatio's
	// default), so the smaller ratio is the scale of everything in it.
	unit := math.Min(fit.Width/box.w, fit.Height/box.h)
	w.walk(root, state{font: rootFontPx, scale: unit})
	fit.Small, fit.Typical = w.sizes()
	return fit, true
}

type box struct{ w, h float64 }

func viewBox(v string) (box, bool) {
	f := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	if len(f) != 4 {
		return box{}, false
	}
	w, errW := strconv.ParseFloat(f[2], 64)
	h, errH := strconv.ParseFloat(f[3], 64)
	if errW != nil || errH != nil || !(w > 0) || !(h > 0) || math.IsInf(w, 0) || math.IsInf(h, 0) {
		return box{}, false
	}
	return box{w, h}, true
}

// naturalSize is the size the SVG asks for: its width and height where
// they are lengths, else a max-width in its style (Mermaid writes
// width="100%" and the real width there), else the viewBox itself.
func naturalSize(root *html.Node, vb box) (w, h float64) {
	w, okW := absoluteLength(attr(root, "width"))
	h, okH := absoluteLength(attr(root, "height"))
	if !okW {
		w, okW = absoluteLength(declaration(attr(root, "style"), "max-width"))
	}
	switch {
	case okW && okH:
	case okW:
		h = w * vb.h / vb.w
	case okH:
		w = h * vb.w / vb.h
	default:
		w, h = vb.w, vb.h
	}
	return w, h
}

// state is what an element hands down to its children.
type state struct {
	font   float64 // computed font-size, in the element's own pixels
	scale  float64 // CSS pixels per one of those pixels
	inText bool    // inside <text> or <foreignObject>: character data is drawn
	chain  []elem  // the element's ancestors and itself, outermost first
}

type elem struct {
	tag, id string
	classes []string
}

type sample struct {
	size   float64
	weight int
}

type walker struct {
	rules   []fontRule
	samples []sample
}

// notDrawn are elements whose text content is never drawn where it is
// written: metadata, style sheets, and definitions used from elsewhere.
var notDrawn = map[string]bool{
	"style": true, "script": true, "title": true, "desc": true,
	"metadata": true, "defs": true, "symbol": true,
}

func (w *walker) walk(n *html.Node, st state) {
	tag := strings.ToLower(n.Data)
	if notDrawn[tag] || hidden(n) {
		return
	}
	e := elem{tag: tag, id: attr(n, "id"), classes: strings.Fields(attr(n, "class"))}
	st.chain = append(st.chain[:len(st.chain):len(st.chain)], e)
	st.font = w.fontSize(n, st)
	st.scale *= transformScale(attr(n, "transform"))
	switch tag {
	case "text", "foreignobject":
		st.inText = true
	case "svg":
		// A nested drawing scales its own viewBox into its own box.
		if len(st.chain) > 1 {
			if vb, ok := viewBox(attr(n, "viewbox")); ok {
				if width, ok := absoluteLength(attr(n, "width")); ok {
					st.scale *= width / vb.w
				}
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.ElementNode:
			w.walk(c, st)
		case html.TextNode:
			if !st.inText {
				continue
			}
			chars := 0
			for _, r := range c.Data {
				if !unicode.IsSpace(r) {
					chars++
				}
			}
			size := st.font * st.scale
			if chars > 0 && size > 0 && !math.IsInf(size, 0) {
				w.samples = append(w.samples, sample{size, chars})
			}
		}
	}
}

// fontSize is n's computed font-size: its own declaration if it has one
// (an !important rule, then the inline style, then the best matching
// rule, then the presentation attribute), else its parent's.
func (w *walker) fontSize(n *html.Node, st state) float64 {
	var rule *fontRule
	for i := range w.rules {
		r := &w.rules[i]
		if !r.sel.matches(st.chain) {
			continue
		}
		if rule == nil || r.outranks(rule) {
			rule = r
		}
	}
	inline := fontDeclaration(attr(n, "style"))
	candidates := make([]string, 0, 5)
	if inline.important {
		candidates = append(candidates, inline.value)
	}
	if rule != nil && rule.important {
		candidates = append(candidates, rule.value)
	}
	candidates = append(candidates, inline.value)
	if rule != nil {
		candidates = append(candidates, rule.value)
	}
	candidates = append(candidates, attr(n, "font-size"))
	for _, v := range candidates {
		if size, ok := fontLength(v, st.font); ok {
			return size
		}
	}
	return st.font
}

// sizes returns the small print and the typical size: the sizes below
// which smallPrintShare and typicalShare of the characters fall.
func (w *walker) sizes() (small, typical float64) {
	if len(w.samples) == 0 {
		return 0, 0
	}
	sort.Slice(w.samples, func(i, j int) bool { return w.samples[i].size < w.samples[j].size })
	total := 0
	for _, s := range w.samples {
		total += s.weight
	}
	at := func(share float64) float64 {
		seen := 0
		for _, s := range w.samples {
			seen += s.weight
			if float64(seen) > share*float64(total) {
				return s.size
			}
		}
		return w.samples[len(w.samples)-1].size
	}
	return at(smallPrintShare), at(typicalShare)
}

func hidden(n *html.Node) bool {
	style := attr(n, "style")
	return attr(n, "display") == "none" || declaration(style, "display") == "none" ||
		attr(n, "visibility") == "hidden" || declaration(style, "visibility") == "hidden"
}

func findElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && strings.EqualFold(n.Data, tag) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

// attr returns n's attribute key (compared without case: the HTML parser
// restores the case of SVG's camelCase attributes) with space trimmed.
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

// transformScale is the linear scale of a transform attribute: the square
// root of its matrix's area factor, so a drawing scaled 2 by 0.5 counts
// as unscaled. Translations, rotations and skews do not change area.
func transformScale(v string) float64 {
	scale := 1.0
	for v != "" {
		open := strings.IndexByte(v, '(')
		if open < 0 {
			break
		}
		shut := strings.IndexByte(v[open:], ')')
		if shut < 0 {
			break
		}
		name := strings.ToLower(strings.Trim(v[:open], ", \t\r\n"))
		args := numbers(v[open+1 : open+shut])
		v = v[open+shut+1:]
		area := 1.0
		switch {
		case name == "scale" && len(args) == 1:
			area = args[0] * args[0]
		case name == "scale" && len(args) == 2:
			area = args[0] * args[1]
		case name == "matrix" && len(args) == 6:
			area = args[0]*args[3] - args[1]*args[2]
		}
		if area = math.Abs(area); area > 0 && !math.IsInf(area, 0) {
			scale *= math.Sqrt(area)
		}
	}
	return scale
}

func numbers(v string) []float64 {
	fields := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	out := make([]float64, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// splitLength splits a CSS length into its number and lower-case unit.
func splitLength(v string) (n float64, unit string, ok bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	end := len(v)
	for end > 0 && (v[end-1] == '%' || (v[end-1] >= 'a' && v[end-1] <= 'z')) {
		end--
	}
	// An exponent's e belongs to the number: "1e2" has no unit.
	n, err := strconv.ParseFloat(v[:end], 64)
	if err != nil {
		if n, err = strconv.ParseFloat(v, 64); err != nil {
			return 0, "", false
		}
		end = len(v)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, "", false
	}
	return n, v[end:], true
}

// absoluteUnits are CSS pixels per unit, for units that need no context.
var absoluteUnits = map[string]float64{
	"":   1,
	"px": 1,
	"pt": cssPxPerInch / ptPerInch,
	"pc": pxPerPica,
	"in": cssPxPerInch,
	"cm": cssPxPerInch / cmPerInch,
	"mm": cssPxPerInch / mmPerInch,
}

func absoluteLength(v string) (float64, bool) {
	n, unit, ok := splitLength(v)
	if !ok || !(n > 0) {
		return 0, false
	}
	per, ok := absoluteUnits[unit]
	return n * per, ok
}

// fontKeywords are the absolute font-size keywords, in pixels at the
// default text size.
var fontKeywords = map[string]float64{
	"xx-small": 9, "x-small": 10, "small": 13, "medium": 16,
	"large": 18, "x-large": 24, "xx-large": 32,
}

const (
	smallerFactor = 1 / 1.2
	largerFactor  = 1.2
)

// fontLength resolves a font-size value against the parent's size.
func fontLength(v string, parent float64) (float64, bool) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "":
		return 0, false
	case "smaller":
		return parent * smallerFactor, true
	case "larger":
		return parent * largerFactor, true
	}
	if px, ok := fontKeywords[v]; ok {
		return px, true
	}
	n, unit, ok := splitLength(v)
	if !ok || !(n > 0) {
		return 0, false
	}
	switch unit {
	case "em":
		return n * parent, true
	case "ex":
		return n * parent * exPerEm, true
	case "rem":
		return n * rootFontPx, true
	case "%":
		return n * parent / 100, true
	}
	per, ok := absoluteUnits[unit]
	return n * per, ok
}
