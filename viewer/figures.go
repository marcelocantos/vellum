// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/marcelocantos/vellum/internal/svgfit"
)

// A figure is an SVG that stands alone on the page: a document's own
// <svg> or an SVG image, with nothing but whitespace beside it in its
// block, perhaps inside a link or <picture>. fitFigures finds them on
// the rendered page and writes the sizing properties (fit.go) on each,
// marking the block that holds it so chrome.css can let it leave the
// reading column. An SVG in a sentence keeps its size, as does one its
// author sized.
const (
	// figurePendingAttr marks a figure whose remote image is not yet
	// measured. chrome.js asks ChromeSVGFitPath for it once the page is
	// up and makes it a figure when the viewer could measure it.
	figurePendingAttr = "data-vellum-figure-pending"
	// fitPendingAttr marks that image.
	fitPendingAttr = "data-vellum-fit-pending"
	// fitWrapAttr marks an inline element between a figure's block and
	// its SVG, such as the link around an image. chrome.css takes its box
	// away so the SVG sizes against the block.
	fitWrapAttr = "data-vellum-fit-wrap"
	// figureWrapOpen and figureWrapClose surround an SVG that stands
	// alone among blocks, where no block of its own can be marked.
	figureWrapOpen  = `<div ` + figureAttr + `="">`
	figureWrapClose = `</div>`

	svgDataPrefix = "data:image/svg+xml"
)

// figureBlocks are the elements that start a block of their own. Every
// other element is inline: its content belongs to the block around it.
var figureBlocks = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true,
	"body": true, "caption": true, "center": true, "dd": true,
	"details": true, "dialog": true, "div": true, "dl": true, "dt": true,
	"fieldset": true, "figcaption": true, "figure": true, "footer": true,
	"form": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "header": true, "hgroup": true, "hr": true, "html": true,
	"li": true, "main": true, "nav": true, "ol": true, "p": true, "pre": true,
	"section": true, "summary": true, "table": true, "tbody": true,
	"td": true, "tfoot": true, "th": true, "thead": true, "tr": true,
	"ul": true,
}

// figureContainers hold a page's flow: a figure among their blocks is
// wrapped in a block of its own rather than marking them.
var figureContainers = map[string]bool{
	"html": true, "body": true, "article": true, "main": true,
	"section": true, "blockquote": true, "li": true, "details": true,
	"dd": true, "td": true, "th": true, "aside": true, "header": true,
	"footer": true, "nav": true, "dialog": true, "form": true,
	"fieldset": true,
}

// figureWrappers are the inline elements a figure's SVG may sit in.
var figureWrappers = map[string]bool{"a": true, "picture": true, "span": true}

// unseen are elements whose content is not drawn where it is written,
// and voids that draw nothing.
var (
	unseenContent = map[string]bool{
		"script": true, "style": true, "template": true, "noscript": true,
		"title": true, "head": true,
	}
	unseenVoids = map[string]bool{
		"br": true, "wbr": true, "source": true, "track": true,
		"meta": true, "link": true, "base": true,
	}
	// rasterExts are image names that are certainly not SVG, so a remote
	// one is not worth a fetch.
	rasterExts = map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".webp": true, ".avif": true, ".bmp": true, ".ico": true,
	}
)

// skipClasses mark viewer-made content (diagrams, math, placeholders)
// that is sized elsewhere or not at all.
var skipClasses = []string{"mermaid", "katex", "vellum-"}

// figure is one SVG that may stand alone.
type figure struct {
	start, tagEnd, end int // the image tag, or the <svg> element
	fit                svgfit.Fit
	pending            bool // a remote image, measured later
	// wraps are the start tags of the inline elements around it, and
	// outer/outerEnd the span of the outermost.
	wraps           [][2]int
	outer, outerEnd int
}

// openElement is an element the pass is inside.
type openElement struct {
	tag           string
	start, tagEnd int
	skip          bool
	// items counts what is drawn in this element's inline content: text,
	// images, drawings. fig is the figure when it is the only one.
	items  int
	fig    *figure
	blocks int // child blocks so far, other than a <figcaption>
}

type pageEdit struct {
	at, end int
	text    string
}

// fitFigures sizes each SVG that stands alone on the page rendered from
// sourcePath. It runs before any pass that rewrites image URLs, as it
// reads them as written. embedded pages (T3 Code) get no pending remote
// figures: nothing there asks for them.
func (s *Server) fitFigures(page, sourcePath string, embedded bool) string {
	dir := filepath.Dir(sourcePath)
	z := html.NewTokenizer(strings.NewReader(page))
	var stack []*openElement
	var edits []pageEdit
	pos := 0
	top := func() *openElement {
		if len(stack) == 0 {
			return nil
		}
		return stack[len(stack)-1]
	}
	skipping := func() bool { t := top(); return t != nil && t.skip }
	addItem := func(f *figure) {
		t := top()
		if t == nil {
			return
		}
		t.items++
		if t.items == 1 {
			t.fig = f
		}
	}
	mark := func(f *figure, block *openElement) {
		if f.pending {
			edits = append(edits, pageEdit{f.start, f.tagEnd, addAttr(page[f.start:f.tagEnd], fitPendingAttr)})
		} else {
			edits = append(edits, pageEdit{f.start, f.tagEnd, addStyle(page[f.start:f.tagEnd], fitStyle(f.fit))})
		}
		for _, w := range f.wraps {
			edits = append(edits, pageEdit{w[0], w[1], addAttr(page[w[0]:w[1]], fitWrapAttr)})
		}
		attr := figureAttr
		if f.pending {
			attr = figurePendingAttr
		}
		if block != nil {
			edits = append(edits, pageEdit{block.start, block.tagEnd, addAttr(page[block.start:block.tagEnd], attr)})
			return
		}
		open := figureWrapOpen
		if f.pending {
			open = `<div ` + figurePendingAttr + `="">`
		}
		edits = append(edits, pageEdit{f.outer, f.outer, open}, pageEdit{f.outerEnd, f.outerEnd, figureWrapClose})
	}
	// finishRun ends the inline content an element has so far: a figure
	// alone in it stands among blocks, in a block of its own.
	finishRun := func(el *openElement) {
		if el.items == 1 && el.fig != nil && !el.skip {
			mark(el.fig, nil)
		}
		el.items, el.fig = 0, nil
	}
	// startBlock is called as a block opens inside the current element.
	startBlock := func(tag string) {
		if t := top(); t != nil && tag != "figcaption" {
			finishRun(t)
			t.blocks++
		}
	}
	closeElement := func(end int) {
		el := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !figureBlocks[el.tag] {
			// Inline: its content is its parent's.
			if top() == nil {
				return
			}
			if el.items == 1 && el.fig != nil && figureWrappers[el.tag] {
				el.fig.wraps = append(el.fig.wraps, [2]int{el.start, el.tagEnd})
				el.fig.outer, el.fig.outerEnd = el.start, end
				addItem(el.fig)
				return
			}
			for i := 0; i < el.items; i++ {
				addItem(nil)
			}
			return
		}
		if el.blocks > 0 || figureContainers[el.tag] {
			finishRun(el)
			return
		}
		if el.items == 1 && el.fig != nil && !el.skip {
			mark(el.fig, el)
		}
	}

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		raw := z.Raw()
		start := pos
		pos += len(raw)
		switch tt {
		case html.TextToken:
			if t := top(); t != nil && !unseenContent[t.tag] && strings.TrimSpace(string(raw)) != "" {
				addItem(nil)
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			attrs := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				attrs[string(k)] = string(v)
			}
			skip := skipping() || hasSkipClass(attrs["class"])
			switch {
			case tag == "svg" || tag == "math":
				end := pos
				if tt == html.StartTagToken {
					end = skipElement(z, tag, &pos)
				}
				var f *figure
				if tag == "svg" && !skip && tt == html.StartTagToken {
					f = s.inlineFigure(page[start:end], attrs)
				}
				if f != nil {
					f.start, f.tagEnd, f.end = start, start+len(raw), end
					f.outer, f.outerEnd = start, end
				}
				addItem(f)
			case tag == "img":
				var f *figure
				if !skip {
					f = s.imageFigure(attrs, dir, embedded)
				}
				if f != nil {
					f.start, f.tagEnd, f.end = start, pos, pos
					f.outer, f.outerEnd = start, pos
				}
				addItem(f)
			case unseenVoids[tag]:
			case tt == html.SelfClosingTagToken || isVoid(tag):
				if figureBlocks[tag] {
					startBlock(tag)
				} else {
					addItem(nil)
				}
			default:
				if figureBlocks[tag] {
					startBlock(tag)
				}
				stack = append(stack, &openElement{tag: tag, start: start, tagEnd: pos, skip: skip})
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			// Close up to the matching element; a stray end tag closes
			// nothing.
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].tag == tag {
					for len(stack) > i {
						closeElement(pos)
					}
					break
				}
			}
		}
	}
	if len(edits) == 0 {
		return page
	}
	// From the end, so earlier offsets hold; at one offset, the tag
	// before the wrapper opened in front of it.
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].at != edits[j].at {
			return edits[i].at > edits[j].at
		}
		return edits[i].end > edits[j].end
	})
	var b strings.Builder
	b.Grow(len(page) + 64*len(edits))
	tail := page
	var parts []string
	for _, e := range edits {
		parts = append(parts, tail[e.end:], e.text)
		tail = tail[:e.at]
	}
	b.WriteString(tail)
	for i := len(parts) - 1; i >= 0; i-- {
		b.WriteString(parts[i])
	}
	return b.String()
}

// skipElement reads up to the end of the tag element just opened and
// returns the offset after it, advancing *pos past what it read.
func skipElement(z *html.Tokenizer, tag string, pos *int) int {
	depth := 1
	for depth > 0 {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		*pos += len(z.Raw())
		if tt != html.StartTagToken && tt != html.EndTagToken {
			continue
		}
		name, _ := z.TagName()
		if string(name) != tag {
			continue
		}
		if tt == html.StartTagToken {
			depth++
		} else {
			depth--
		}
	}
	return *pos
}

func hasSkipClass(class string) bool {
	for _, c := range strings.Fields(class) {
		for _, p := range skipClasses {
			if strings.HasPrefix(c, p) {
				return true
			}
		}
	}
	return false
}

func isVoid(tag string) bool {
	switch tag {
	case "area", "embed", "hr", "input", "keygen", "param", "col":
		return true
	}
	return false
}

// authorSized reports whether a style gives the element a size.
//
// A declaration that only keeps the drawing inside its container does not
// count: max-width of 100% or more (or none), and width or height auto.
// "max-width: 100%; height: auto" is the usual responsive-image idiom, and
// a page exported with it still wants the drawing grown to reading size.
// Anything marked !important counts, since the viewer cannot override it.
func authorSized(style string) bool {
	for _, d := range strings.Split(style, ";") {
		k, v, ok := strings.Cut(d, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.ToLower(strings.TrimSpace(v))
		switch k {
		case "width", "height":
			if v != "auto" {
				return true
			}
		case "max-width":
			if !containerBound(v) {
				return true
			}
		}
	}
	return false
}

// containerBound reports whether a max-width value at most holds a drawing
// to its container: none, or a percentage of 100 or more.
func containerBound(v string) bool {
	if v == "none" {
		return true
	}
	pct, ok := strings.CutSuffix(v, "%")
	if !ok {
		return false
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(pct), 64)
	return err == nil && n >= 100
}

// inlineFigure measures a document's own <svg> element.
func (s *Server) inlineFigure(markup string, attrs map[string]string) *figure {
	if authorSized(attrs["style"]) || strings.HasSuffix(strings.TrimSpace(attrs["width"]), "%") {
		return nil
	}
	fit, ok := s.contentFit("inline", []byte(markup))
	if !ok {
		return nil
	}
	return &figure{fit: fit}
}

// imageFigure measures an SVG image: a file in a served folder, a data:
// URL, or a remote image the viewer has measured. A remote image it has
// not is pending; one that is certainly not an SVG is nil.
func (s *Server) imageFigure(attrs map[string]string, dir string, embedded bool) *figure {
	src := strings.TrimSpace(attrs["src"])
	_, hasW := attrs["width"]
	_, hasH := attrs["height"]
	if src == "" || hasW || hasH || authorSized(attrs["style"]) {
		return nil
	}
	if strings.HasPrefix(strings.ToLower(src), svgDataPrefix) {
		data, ok := decodeSVGDataURL(src)
		if !ok {
			return nil
		}
		if fit, ok := s.contentFit("data", data); ok {
			return &figure{fit: fit}
		}
		return nil
	}
	if remote, ok := remoteImageURL(src); ok {
		if fit, ok, known := s.knownRemoteFit(remote); known {
			if ok {
				return &figure{fit: fit}
			}
			return nil
		}
		if embedded {
			return nil
		}
		return &figure{pending: true}
	}
	abs, _, ok := localRef(src, dir)
	if !ok || !strings.EqualFold(filepath.Ext(abs), fitSVGExt) {
		return nil
	}
	if fit, ok := s.fileFit(abs); ok {
		return &figure{fit: fit}
	}
	return nil
}

// contentFit measures SVG markup, remembered by its content.
func (s *Server) contentFit(kind string, data []byte) (svgfit.Fit, bool) {
	sum := sha256.Sum256(data)
	key := kind + "\x00" + hex.EncodeToString(sum[:])
	if m, ok := s.fits.Load(key); ok {
		memo := m.(fitMemo)
		return memo.fit, memo.ok
	}
	fit, ok := svgfit.Measure(data)
	s.fits.Store(key, fitMemo{fit: fit, ok: ok})
	return fit, ok
}

// decodeSVGDataURL returns the SVG a data:image/svg+xml URL carries.
func decodeSVGDataURL(src string) ([]byte, bool) {
	meta, payload, ok := strings.Cut(src[len(svgDataPrefix):], ",")
	if !ok {
		return nil, false
	}
	if len(payload) > base64.StdEncoding.EncodedLen(svgfit.MaxBytes) {
		return nil, false
	}
	for _, p := range strings.Split(meta, ";") {
		if strings.EqualFold(strings.TrimSpace(p), "base64") {
			data, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, strings.NewReader(strings.TrimSpace(payload))))
			return data, err == nil
		}
	}
	data, err := url.PathUnescape(payload)
	return []byte(data), err == nil
}

// remoteImageURL is src when it names an http or https image that may
// be an SVG.
func remoteImageURL(src string) (string, bool) {
	u, err := url.Parse(src)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	if rasterExts[strings.ToLower(filepath.Ext(u.Path))] {
		return "", false
	}
	return u.String(), true
}

// addAttr adds an empty attribute to a start tag.
func addAttr(tag, name string) string {
	end := strings.TrimSuffix(tag, ">")
	slash := strings.HasSuffix(end, "/")
	end = strings.TrimRight(strings.TrimSuffix(end, "/"), " ")
	out := end + " " + name + `=""`
	if slash {
		return out + "/>"
	}
	return out + ">"
}
