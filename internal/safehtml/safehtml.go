// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package safehtml removes active content from HTML that a document
// brought with it, so a page that shows the document in a browser cannot
// run anything the document's author wrote.
//
// Document is an allow-list: it parses the HTML the way a browser does,
// keeps only the elements, attributes, URL schemes and style properties
// listed here, and serialises the tree again. Everything else is dropped
// (elements that only make sense with their content, such as script,
// style and iframe) or unwrapped (any other unknown element: its children
// stay). The list follows what GitHub shows for a README: formatting,
// tables, lists, images, links, details/summary.
//
// Rendered is for markup that vellum's own renderers produce from
// document text (KaTeX HTML, Mermaid SVG). Those renderers escape their
// input, so it only strips what must never run: script elements, event
// handler attributes, and javascript:-style URLs.
package safehtml

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// dropWithContent are elements removed together with everything inside
// them: their content is code, a separate document, or form state, never
// prose a reader should still see.
var dropWithContent = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Template: true,
	atom.Iframe: true, atom.Frame: true, atom.Frameset: true,
	atom.Object: true, atom.Embed: true, atom.Applet: true, atom.Param: true,
	atom.Noscript: true, atom.Noembed: true, atom.Noframes: true,
	atom.Svg: true, atom.Math: true,
	atom.Head: true, atom.Title: true, atom.Meta: true, atom.Link: true, atom.Base: true,
	atom.Textarea: true, atom.Select: true, atom.Option: true, atom.Optgroup: true,
	atom.Datalist: true, atom.Xmp: true, atom.Plaintext: true,
	atom.Audio: true, atom.Video: true, atom.Source: true, atom.Track: true,
	atom.Canvas: true, atom.Picture: true, atom.Dialog: true,
}

// allowedElements may stay. Any element in neither map is unwrapped.
var allowedElements = map[atom.Atom]bool{
	atom.A: true, atom.Abbr: true, atom.B: true, atom.Bdi: true, atom.Bdo: true,
	atom.Blockquote: true, atom.Br: true, atom.Caption: true, atom.Cite: true,
	atom.Code: true, atom.Col: true, atom.Colgroup: true, atom.Dd: true,
	atom.Del: true, atom.Details: true, atom.Dfn: true, atom.Div: true,
	atom.Dl: true, atom.Dt: true, atom.Em: true, atom.Figcaption: true,
	atom.Figure: true, atom.H1: true, atom.H2: true, atom.H3: true,
	atom.H4: true, atom.H5: true, atom.H6: true, atom.Hr: true, atom.I: true,
	atom.Img: true, atom.Input: true, atom.Ins: true, atom.Kbd: true,
	atom.Li: true, atom.Mark: true, atom.Ol: true, atom.P: true, atom.Pre: true,
	atom.Q: true, atom.Rp: true, atom.Rt: true, atom.Ruby: true, atom.S: true,
	atom.Samp: true, atom.Small: true, atom.Span: true, atom.Strike: true,
	atom.Strong: true, atom.Sub: true, atom.Summary: true, atom.Sup: true,
	atom.Table: true, atom.Tbody: true, atom.Td: true, atom.Tfoot: true,
	atom.Th: true, atom.Thead: true, atom.Time: true, atom.Tr: true,
	atom.Tt: true, atom.U: true, atom.Ul: true, atom.Var: true, atom.Wbr: true,
}

// globalAttrs may appear on any allowed element. The data- attributes are
// the ones vellum's renderer emits for the viewer (source lines, task
// identity); a document that forges them can only point the viewer at
// its own lines, which every write re-checks against the file.
var globalAttrs = map[string]bool{
	"id": true, "class": true, "title": true, "lang": true, "dir": true,
	"align": true, "role": true, "style": true,
	"aria-label": true, "aria-hidden": true, "aria-describedby": true, "aria-labelledby": true,
	"data-source-line": true, "data-source-end": true,
	"data-task-index": true, "data-task-line": true,
}

var elementAttrs = map[atom.Atom]map[string]bool{
	atom.A:          {"href": true, "name": true, "hreflang": true},
	atom.Img:        {"src": true, "alt": true, "width": true, "height": true},
	atom.Ol:         {"start": true, "type": true, "reversed": true},
	atom.Ul:         {"type": true},
	atom.Li:         {"value": true},
	atom.Td:         {"colspan": true, "rowspan": true, "valign": true, "headers": true, "abbr": true},
	atom.Th:         {"colspan": true, "rowspan": true, "valign": true, "headers": true, "abbr": true, "scope": true},
	atom.Col:        {"span": true, "width": true},
	atom.Colgroup:   {"span": true, "width": true},
	atom.Table:      {"summary": true, "width": true, "border": true, "cellpadding": true, "cellspacing": true},
	atom.Details:    {"open": true},
	atom.Time:       {"datetime": true},
	atom.Blockquote: {"cite": true},
	atom.Q:          {"cite": true},
	atom.Del:        {"cite": true, "datetime": true},
	atom.Ins:        {"cite": true, "datetime": true},
	atom.Input:      {"type": true, "checked": true, "disabled": true},
}

// Schemes a URL attribute may name. A URL with no scheme (relative, or
// only a #fragment) is always allowed.
var (
	linkSchemes  = map[string]bool{"http": true, "https": true, "mailto": true, "tel": true, "file": true}
	imageSchemes = map[string]bool{"http": true, "https": true}
	citeSchemes  = map[string]bool{"http": true, "https": true}
)

// dataImageTypes are the image types a data: URL may carry in <img src>.
// An image is never a document: SVG loaded through <img> does not run
// script.
var dataImageTypes = map[string]bool{
	"image/png": true, "image/gif": true, "image/jpeg": true, "image/jpg": true,
	"image/webp": true, "image/avif": true, "image/bmp": true, "image/svg+xml": true,
}

// reservedClassPrefix marks classes the viewer's own chrome and
// placeholders use. A document may not claim them: they would let it
// dress its content up as the viewer's controls.
const reservedClassPrefix = "vellum-"

var bodyContext = &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}

// Document returns body (the HTML rendered for one Markdown document)
// with everything not on the allow-list removed.
func Document(body string) string {
	nodes, err := html.ParseFragment(strings.NewReader(body), bodyContext)
	if err != nil {
		// The reader is a string; parsing does not fail. Should it ever,
		// show the source as text rather than risk passing markup through.
		return html.EscapeString(body)
	}
	var b bytes.Buffer
	for _, n := range nodes {
		for _, kept := range cleanDocumentNode(n) {
			_ = html.Render(&b, kept)
		}
	}
	return b.String()
}

// cleanDocumentNode returns what replaces n in its parent: n itself
// (cleaned), n's cleaned children (unwrapped), or nothing (dropped). n is
// detached from its parent first.
func cleanDocumentNode(n *html.Node) []*html.Node {
	if n.Parent != nil {
		n.Parent.RemoveChild(n)
	}
	switch n.Type {
	case html.TextNode:
		return []*html.Node{n}
	case html.CommentNode:
		// Comments carry vellum's own placeholders through to the steps
		// after this one; the serialiser closes them, so they stay inert.
		return []*html.Node{n}
	case html.ElementNode:
	default:
		return nil
	}
	// Foreign content (SVG, MathML) parses by different rules, which is
	// where sanitiser and browser most often disagree. None is allowed.
	if n.Namespace != "" || dropWithContent[n.DataAtom] {
		return nil
	}
	children := cleanChildren(n)
	if !allowedElements[n.DataAtom] {
		return children
	}
	if n.DataAtom == atom.Input && !isCheckbox(n) {
		return nil
	}
	n.Attr = cleanAttrs(n.DataAtom, n.Attr)
	for _, c := range children {
		n.AppendChild(c)
	}
	return []*html.Node{n}
}

func cleanChildren(n *html.Node) []*html.Node {
	var kids []*html.Node
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		kids = append(kids, cleanDocumentNode(c)...)
		c = next
	}
	return kids
}

func isCheckbox(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == "type" {
			return strings.EqualFold(strings.TrimSpace(a.Val), "checkbox")
		}
	}
	return false
}

func cleanAttrs(el atom.Atom, attrs []html.Attribute) []html.Attribute {
	out := attrs[:0]
	for _, a := range attrs {
		if a.Namespace != "" {
			continue
		}
		key := strings.ToLower(a.Key)
		if !globalAttrs[key] && !elementAttrs[el][key] {
			continue
		}
		switch key {
		case "href":
			if !safeURL(a.Val, linkSchemes, false) {
				continue
			}
		case "src":
			if !safeURL(a.Val, imageSchemes, true) {
				continue
			}
		case "cite":
			if !safeURL(a.Val, citeSchemes, false) {
				continue
			}
		case "style":
			a.Val = cleanStyle(a.Val)
			if a.Val == "" {
				continue
			}
		case "class":
			a.Val = cleanClass(a.Val)
			if a.Val == "" {
				continue
			}
		case "type":
			if el == atom.Input {
				a.Val = "checkbox"
			}
		}
		a.Key = key
		out = append(out, a)
	}
	return out
}

// safeURL reports whether raw, read the way a browser reads a URL, has no
// scheme or one in allowed. dataImages also admits data: URLs of the
// image types in dataImageTypes.
func safeURL(raw string, allowed map[string]bool, dataImages bool) bool {
	// The URL parser strips leading and trailing C0 controls and spaces,
	// and removes tab and newline anywhere: "java\tscript:" is a scheme.
	s := strings.TrimFunc(raw, func(r rune) bool { return r <= ' ' })
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
	end := strings.IndexAny(s, ":/?#")
	if end < 0 || s[end] != ':' {
		return true // relative reference: no scheme
	}
	// Anything before the first colon makes the URL absolute or
	// unreadable; either way only a listed scheme passes.
	scheme := strings.ToLower(s[:end])
	if allowed[scheme] {
		return true
	}
	if dataImages && scheme == "data" {
		mediaType := s[end+1:]
		if i := strings.IndexAny(mediaType, ";,"); i >= 0 {
			mediaType = mediaType[:i]
		}
		return dataImageTypes[strings.ToLower(strings.TrimSpace(mediaType))]
	}
	return false
}

// cleanStyle keeps the declarations a document's tables need (text-align,
// which Markdown table alignment renders as) and drops the rest: style is
// how a document would position content over the viewer's own controls.
func cleanStyle(style string) string {
	var kept []string
	for decl := range strings.SplitSeq(style, ";") {
		prop, val, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		prop = strings.ToLower(strings.TrimSpace(prop))
		val = strings.ToLower(strings.TrimSpace(val))
		if prop != "text-align" {
			continue
		}
		switch val {
		case "left", "right", "center", "justify", "start", "end":
			kept = append(kept, prop+":"+val)
		}
	}
	return strings.Join(kept, ";")
}

func cleanClass(class string) string {
	var kept []string
	for c := range strings.FieldsSeq(class) {
		if strings.HasPrefix(strings.ToLower(c), reservedClassPrefix) {
			continue
		}
		kept = append(kept, c)
	}
	return strings.Join(kept, " ")
}

// renderedDrop are elements stripped from renderer output wherever they
// appear, in any namespace: they run script, load another document, or
// (the SVG animation elements) can rewrite an attribute such as href
// after the scrub has looked at it.
var renderedDrop = map[string]bool{
	"script": true, "iframe": true, "frame": true, "frameset": true,
	"object": true, "embed": true, "applet": true, "base": true,
	"meta": true, "link": true, "form": true, "portal": true,
	"handler": true, "listener": true,
	"animate": true, "set": true, "animatemotion": true,
	"animatetransform": true, "animatecolor": true, "discard": true,
}

// renderedURLAttrs are attributes that a browser dereferences as a URL.
var renderedURLAttrs = map[string]bool{
	"href": true, "src": true, "action": true, "formaction": true,
	"data": true, "background": true, "poster": true, "codebase": true,
	"srcset": true, "lowsrc": true, "dynsrc": true, "ping": true,
}

// Rendered returns fragment (HTML or SVG from one of vellum's renderers)
// without script elements, event handlers, or URLs outside http, https,
// mailto, fragment-only references and data: images.
func Rendered(fragment string) string {
	nodes, err := html.ParseFragment(strings.NewReader(fragment), bodyContext)
	if err != nil {
		return html.EscapeString(fragment)
	}
	var b bytes.Buffer
	for _, n := range nodes {
		if scrubRendered(n) {
			_ = html.Render(&b, n)
		}
	}
	return b.String()
}

// scrubRendered cleans n in place and reports whether n itself stays.
func scrubRendered(n *html.Node) bool {
	switch n.Type {
	case html.ElementNode:
	case html.TextNode, html.CommentNode:
		return true
	default:
		return false
	}
	if renderedDrop[strings.ToLower(n.Data)] {
		return false
	}
	out := n.Attr[:0]
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		if strings.HasPrefix(key, "on") {
			continue
		}
		if renderedURLAttrs[key] && !safeURL(a.Val, linkSchemes, true) {
			continue
		}
		out = append(out, a)
	}
	n.Attr = out
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if !scrubRendered(c) {
			n.RemoveChild(c)
		}
		c = next
	}
	return true
}
