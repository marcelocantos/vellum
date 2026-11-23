// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/vellum/internal/fileurl"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// InlineArgs says where InlineResources may get a page's resources.
type InlineArgs struct {
	// Read returns the bytes of a local file, given its absolute path.
	// An error drops the reference. Nil drops every local reference.
	Read func(abs string) ([]byte, error)
	// Fetch returns an http or https resource's bytes and media type.
	// An error drops the reference. Nil drops every remote reference.
	Fetch func(ctx context.Context, rawURL string) (data []byte, mediaType string, err error)
}

// maxInlineDepth bounds how far a stylesheet's @import and url() are
// followed. A cycle is also stopped by the seen map.
const maxInlineDepth = 8

// InlineResources prepares a rendered page for WriteSelfContainedPDF,
// whose engine fetches nothing but data: URLs.
//
// Every URL the body names as a resource (element src, data and href
// attributes, CSS url() and @import) becomes a data: URL holding bytes
// that Read or Fetch returned, or is removed. Relative references and
// local paths resolve against baseDir and go through Read; http and https
// go through Fetch; data: URLs stay as written; anything else is removed.
// <base> elements are removed, so nothing resolves against a base the
// document chose.
//
// vellum decides every byte the engine sees. A stylesheet's own url()
// and @import are rewritten before it becomes a data: URL. What a data:
// resource names in turn (an SVG's <image> or <use>) is not followed
// here: the engine refuses it.
//
// The head is left alone. It is vellum's own (the stylesheet, and KaTeX
// inlined by Options.KaTeXInline); anything there that is not a data:
// URL is refused by the engine as well.
func InlineResources(ctx context.Context, fullHTML, baseDir string, args *InlineArgs) (string, error) {
	if args == nil {
		args = &InlineArgs{}
	}
	in := &inliner{ctx: ctx, base: baseDir, args: args, seen: map[string]refDecision{}}
	return in.rewrite(fullHTML)
}

type inliner struct {
	ctx   context.Context
	base  string
	args  *InlineArgs
	seen  map[string]refDecision
	depth int
}

type refDecision struct {
	kind int
	repl string
}

const (
	refKeep = iota
	refDrop
	refReplace
)

func (in *inliner) rewrite(fullHTML string) (string, error) {
	doc, err := html.Parse(strings.NewReader(fullHTML))
	if err != nil {
		return "", err
	}
	stripBases(doc)
	var body *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if body != nil {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Body {
			body = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if body != nil {
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				in.rewriteElement(n)
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(body)
	}
	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func stripBases(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode && (c.DataAtom == atom.Base || strings.EqualFold(c.Data, "base")) {
			n.RemoveChild(c)
		} else {
			stripBases(c)
		}
		c = next
	}
}

func (in *inliner) rewriteElement(n *html.Node) {
	if n.DataAtom == atom.Style || strings.EqualFold(n.Data, "style") {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				c.Data = rewriteCSSURLs(c.Data, in.decide)
			}
		}
	}
	attrs := make([]html.Attribute, 0, len(n.Attr))
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		switch {
		case key == "srcset":
			continue
		case key == "style":
			a.Val = rewriteCSSURLs(a.Val, in.decide)
			attrs = append(attrs, a)
		case key == "src" || key == "poster" || strings.HasSuffix(key, ":src"):
			if v, ok := in.keptAttr(a.Val); ok {
				a.Val = v
				attrs = append(attrs, a)
			}
		case key == "data" && isEmbeddedResource(n):
			if v, ok := in.keptAttr(a.Val); ok {
				a.Val = v
				attrs = append(attrs, a)
			}
		case (key == "href" || strings.HasSuffix(key, ":href")) && isResourceHref(n):
			if v, ok := in.keptAttr(a.Val); ok {
				a.Val = v
				attrs = append(attrs, a)
			}
		default:
			attrs = append(attrs, a)
		}
	}
	n.Attr = attrs
}

func isEmbeddedResource(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Object, atom.Embed:
		return true
	}
	switch strings.ToLower(n.Data) {
	case "object", "embed":
		return true
	}
	return false
}

func isResourceHref(n *html.Node) bool {
	if n.DataAtom == atom.Link {
		return true
	}
	switch strings.ToLower(n.Data) {
	case "image", "use", "feimage", "link":
		return true
	case "a", "area":
		// WeasyPrint embeds the target of rel=attachment as a file in the
		// PDF; any other link is only a link.
		return relHas(n, "attachment")
	}
	return false
}

func relHas(n *html.Node, token string) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, "rel") {
			for _, f := range strings.Fields(a.Val) {
				if strings.EqualFold(f, token) {
					return true
				}
			}
		}
	}
	return false
}

func (in *inliner) keptAttr(val string) (string, bool) {
	switch d := in.decide(val); d.kind {
	case refKeep:
		return val, true
	case refReplace:
		return d.repl, true
	default:
		return "", false
	}
}

// decide maps one reference to what the engine gets: the reference
// itself (a fragment or a data: URL), a data: URL vellum filled, or
// nothing.
func (in *inliner) decide(ref string) refDecision {
	ref = strings.Trim(ref, " \t\n\r\f")
	if ref == "" || strings.HasPrefix(ref, "#") {
		return refDecision{kind: refKeep}
	}
	if strings.ContainsRune(ref, 0) {
		return refDecision{kind: refDrop}
	}
	if p, ok := fileurl.ToPath(ref); ok {
		if p == "" || strings.ContainsRune(p, 0) {
			return refDecision{kind: refDrop}
		}
		return in.local(filepath.Clean(p))
	}
	u, err := url.Parse(ref)
	if err != nil {
		return refDecision{kind: refDrop}
	}
	switch strings.ToLower(u.Scheme) {
	case "data":
		return refDecision{kind: refKeep}
	case "http", "https":
		return in.remote(ref)
	case "":
	default:
		return refDecision{kind: refDrop}
	}
	if strings.HasPrefix(ref, "//") {
		return refDecision{kind: refDrop}
	}
	p := u.Path
	if strings.ContainsRune(p, 0) || strings.ContainsAny(p, "\r\n") {
		return refDecision{kind: refDrop}
	}
	p = filepath.FromSlash(p)
	if p == "" || p == "." {
		return refDecision{kind: refDrop}
	}
	abs := p
	if !filepath.IsAbs(p) {
		abs = filepath.Join(in.base, p)
	}
	return in.local(filepath.Clean(abs))
}

func (in *inliner) local(abs string) refDecision {
	key := "file\x00" + abs
	if d, ok := in.seen[key]; ok {
		return d
	}
	// A placeholder stops a stylesheet that imports itself. The real
	// decision replaces it when the bytes are in hand.
	in.seen[key] = refDecision{kind: refDrop}
	d := refDecision{kind: refDrop}
	if in.args.Read != nil && in.depth < maxInlineDepth {
		if b, err := in.args.Read(abs); err == nil {
			d = in.packed(abs, filepath.Dir(abs), b, "")
		}
	}
	in.seen[key] = d
	return d
}

func (in *inliner) remote(ref string) refDecision {
	key := "url\x00" + ref
	if d, ok := in.seen[key]; ok {
		return d
	}
	in.seen[key] = refDecision{kind: refDrop}
	d := refDecision{kind: refDrop}
	if in.args.Fetch != nil && in.depth < maxInlineDepth {
		if b, mediaType, err := in.args.Fetch(in.ctx, ref); err == nil {
			d = in.packed(ref, ref, b, mediaType)
		}
	}
	in.seen[key] = d
	return d
}

// packed turns bytes into a data: URL. A stylesheet's own url() and
// @import are rewritten first, against cssBase: the file's directory, or
// the stylesheet's URL. Relative names in a remote stylesheet stay on
// that host; they are not read as local files.
func (in *inliner) packed(name, cssBase string, b []byte, mediaType string) refDecision {
	if mediaType == "" {
		mediaType = mediaTypeFor(name, b)
	}
	if i := strings.IndexAny(mediaType, ";,"); i >= 0 {
		mediaType = mediaType[:i]
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType == "text/css" && in.depth < maxInlineDepth {
		in.depth++
		rewritten := rewriteCSSURLs(string(b), func(ref string) refDecision {
			return in.decide(resolveCSSRef(cssBase, ref))
		})
		in.depth--
		b = []byte(rewritten)
	}
	return refDecision{kind: refReplace, repl: dataURL(mediaType, b)}
}

// resolveCSSRef makes a stylesheet-relative reference absolute against
// cssBase. cssBase is a directory path or an http(s) URL. An absolute
// reference, including a file: URL, is returned unchanged.
func resolveCSSRef(cssBase, ref string) string {
	ref = strings.Trim(ref, " \t\n\r\f")
	if ref == "" || strings.HasPrefix(ref, "#") || strings.HasPrefix(strings.ToLower(ref), "data:") {
		return ref
	}
	u, err := url.Parse(ref)
	if err != nil || u.IsAbs() || strings.HasPrefix(ref, "//") {
		return ref
	}
	if strings.HasPrefix(cssBase, "http://") || strings.HasPrefix(cssBase, "https://") {
		base, err := url.Parse(cssBase)
		if err != nil {
			return ref
		}
		return base.ResolveReference(u).String()
	}
	if cssBase == "" {
		return ref
	}
	return filepath.Join(cssBase, filepath.FromSlash(u.Path))
}

// inlineMediaTypes names the types of the files a page commonly shows,
// so an inlined file keeps its type without a system MIME table. SVG in
// particular cannot be sniffed.
var inlineMediaTypes = map[string]string{
	".png":   "image/png",
	".apng":  "image/apng",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".avif":  "image/avif",
	".bmp":   "image/bmp",
	".ico":   "image/x-icon",
	".svg":   "image/svg+xml",
	".css":   "text/css",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
}

func mediaTypeFor(name string, data []byte) string {
	if t, ok := inlineMediaTypes[strings.ToLower(filepath.Ext(name))]; ok {
		return t
	}
	return http.DetectContentType(data)
}

// dataURL encodes data as a base64 data: URL. mediaType loses any
// parameters that could end the URL's header early.
func dataURL(mediaType string, data []byte) string {
	if i := strings.IndexAny(mediaType, ";,"); i >= 0 {
		mediaType = mediaType[:i]
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// rewriteCSSURLs rewrites url() and @import in one CSS declaration or
// stylesheet. A reference decide replaces is written back; one it drops
// becomes none, and an @import it drops is removed through its semicolon
// so the engine does not resolve it against the document base.
func rewriteCSSURLs(css string, decide func(string) refDecision) string {
	var b strings.Builder
	i := 0
	for i < len(css) {
		if css[i] == '"' || css[i] == '\'' {
			j := consumeCSSString(css, i)
			b.WriteString(css[i:j])
			i = j
			continue
		}
		if strings.HasPrefix(css[i:], "/*") {
			j := strings.Index(css[i+2:], "*/")
			if j < 0 {
				b.WriteString(css[i:])
				return b.String()
			}
			j = i + 2 + j + 2
			b.WriteString(css[i:j])
			i = j
			continue
		}
		if atCSSIdent(css, i) && hasPrefixFold(css[i:], "url(") {
			end, raw, ok := readCSSURL(css, i)
			if !ok {
				b.WriteByte(css[i])
				i++
				continue
			}
			writeCSSURL(&b, css[i:end], unescapeCSS(raw), decide)
			i = end
			continue
		}
		if atCSSIdent(css, i) && hasPrefixFold(css[i:], "@import") {
			end, raw, ok := readCSSImport(css, i)
			if !ok {
				b.WriteByte(css[i])
				i++
				continue
			}
			switch d := decide(unescapeCSS(raw)); d.kind {
			case refDrop:
				k := end
				for k < len(css) && css[k] != ';' {
					k++
				}
				if k < len(css) {
					k++
				}
				i = k
			case refReplace:
				b.WriteString("@import ")
				b.WriteString(cssQuote(d.repl))
				i = end
			default:
				b.WriteString(css[i:end])
				i = end
			}
			continue
		}
		b.WriteByte(css[i])
		i++
	}
	return b.String()
}

func writeCSSURL(b *strings.Builder, original, unescaped string, decide func(string) refDecision) {
	switch d := decide(unescaped); d.kind {
	case refDrop:
		b.WriteString("none")
	case refReplace:
		b.WriteString("url(")
		b.WriteString(cssQuote(d.repl))
		b.WriteByte(')')
	default:
		b.WriteString(original)
	}
}

func atCSSIdent(css string, i int) bool {
	if i == 0 {
		return true
	}
	c := css[i-1]
	if c == '\\' {
		return false
	}
	if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
		return false
	}
	return true
}

func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

func consumeCSSString(css string, i int) int {
	q := css[i]
	i++
	for i < len(css) {
		if css[i] == '\\' && i+1 < len(css) {
			i += 2
			continue
		}
		if css[i] == q {
			return i + 1
		}
		if css[i] == '\n' {
			return i
		}
		i++
	}
	return i
}

func readCSSURL(css string, i int) (end int, raw string, ok bool) {
	j := skipCSSSpace(css, i+len("url("))
	if j >= len(css) {
		return 0, "", false
	}
	if css[j] == '"' || css[j] == '\'' {
		raw, j, ok = readCSSQuoted(css, j)
		if !ok {
			return 0, "", false
		}
		j = skipCSSSpace(css, j)
		if j >= len(css) || css[j] != ')' {
			return 0, "", false
		}
		return j + 1, raw, true
	}
	k := j
	for k < len(css) && css[k] != ')' {
		k++
	}
	if k >= len(css) {
		return 0, "", false
	}
	return k + 1, strings.Trim(css[j:k], " \t\n\r\f"), true
}

func readCSSImport(css string, i int) (end int, raw string, ok bool) {
	j := skipCSSSpace(css, i+len("@import"))
	if j >= len(css) {
		return 0, "", false
	}
	if hasPrefixFold(css[j:], "url(") && atCSSIdent(css, j) {
		return readCSSURL(css, j)
	}
	if css[j] == '"' || css[j] == '\'' {
		raw, end, ok = readCSSQuoted(css, j)
		return end, raw, ok
	}
	return 0, "", false
}

func readCSSQuoted(css string, i int) (raw string, end int, ok bool) {
	q := css[i]
	i++
	var b strings.Builder
	for i < len(css) {
		if css[i] == '\\' && i+1 < len(css) {
			b.WriteByte('\\')
			b.WriteByte(css[i+1])
			i += 2
			continue
		}
		if css[i] == q {
			return b.String(), i + 1, true
		}
		if css[i] == '\n' {
			return "", 0, false
		}
		b.WriteByte(css[i])
		i++
	}
	return "", 0, false
}

func skipCSSSpace(css string, i int) int {
	for i < len(css) {
		switch css[i] {
		case ' ', '\t', '\n', '\r', '\f':
			i++
			continue
		}
		if strings.HasPrefix(css[i:], "/*") {
			j := strings.Index(css[i+2:], "*/")
			if j < 0 {
				return len(css)
			}
			i = i + 2 + j + 2
			continue
		}
		break
	}
	return i
}

func unescapeCSS(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 >= len(s) {
			break
		}
		if s[i+1] == '\n' {
			i++
			continue
		}
		if s[i+1] == '\r' {
			i++
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			continue
		}
		if isHex(s[i+1]) {
			j := i + 1
			hex := 0
			n := 0
			for j < len(s) && n < 6 && isHex(s[j]) {
				hex = hex<<4 | hexVal(s[j])
				j++
				n++
			}
			if j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r' || s[j] == '\f') {
				j++
			}
			b.WriteRune(rune(hex))
			i = j - 1
			continue
		}
		b.WriteByte(s[i+1])
		i++
	}
	return b.String()
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}

func cssQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteByte(s[i])
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}
