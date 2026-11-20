// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/marcelocantos/vellum/internal/fileurl"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// IsolateLocalFiles prepares a rendered page for a PDF engine whose base
// URL resolves relative references as files.
//
// A base URL pointed at a fresh directory is not enough: url(../x) and
// @import "../x" resolve outside that directory. When read is non-nil,
// every non-remote URL in the document body (elements and CSS) is copied
// through read into a new temporary directory, which is returned as the
// base to pass to WritePDF. The caller removes that directory. A URL read
// refuses is removed, so the engine never resolves it. The head is left
// alone: its stylesheet link is an absolute file URL, and that
// stylesheet's own font urls are relative to the stylesheet, not to the
// document base.
//
// When read is nil the page and baseDir are returned unchanged and the
// caller must not remove baseDir. That is the CLI and MCP path, which
// still loads whatever the document's directory can see.
func IsolateLocalFiles(fullHTML, baseDir string, read func(abs string) ([]byte, error)) (htmlOut, resourceDir string, err error) {
	if read == nil {
		return fullHTML, baseDir, nil
	}
	dir, err := os.MkdirTemp("", "vellum-pdf-")
	if err != nil {
		return "", "", err
	}
	j := &pdfJail{dir: dir, base: baseDir, read: read}
	out, err := j.rewrite(fullHTML)
	if err != nil {
		os.RemoveAll(dir)
		return "", "", err
	}
	return out, dir, nil
}

type pdfJail struct {
	dir  string
	base string
	read func(abs string) ([]byte, error)
	n    int
	err  error
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

func (j *pdfJail) rewrite(fullHTML string) (string, error) {
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
				j.rewriteElement(n)
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(body)
	}
	if j.err != nil {
		return "", j.err
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

func (j *pdfJail) rewriteElement(n *html.Node) {
	if n.DataAtom == atom.Style || strings.EqualFold(n.Data, "style") {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				c.Data = rewriteCSSURLs(c.Data, j.decide)
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
			a.Val = rewriteCSSURLs(a.Val, j.decide)
			attrs = append(attrs, a)
		case key == "src" || key == "poster" || strings.HasSuffix(key, ":src"):
			if v, ok := j.keptAttr(a.Val); ok {
				a.Val = v
				attrs = append(attrs, a)
			}
		case key == "data" && isEmbeddedResource(n):
			if v, ok := j.keptAttr(a.Val); ok {
				a.Val = v
				attrs = append(attrs, a)
			}
		case (key == "href" || strings.HasSuffix(key, ":href")) && isResourceHref(n):
			if v, ok := j.keptAttr(a.Val); ok {
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
	}
	return false
}

func (j *pdfJail) keptAttr(val string) (string, bool) {
	switch d := j.decide(val); d.kind {
	case refKeep:
		return val, true
	case refReplace:
		return d.repl, true
	default:
		return "", false
	}
}

func (j *pdfJail) decide(ref string) refDecision {
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
		return j.copy(p, ref)
	}
	u, err := url.Parse(ref)
	if err != nil {
		return refDecision{kind: refDrop}
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "data", "blob", "mailto":
		return refDecision{kind: refKeep}
	case "file":
		return refDecision{kind: refDrop}
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
		abs = filepath.Join(j.base, p)
	}
	return j.copy(filepath.Clean(abs), ref)
}

func (j *pdfJail) copy(abs, hint string) refDecision {
	b, err := j.read(abs)
	if err != nil {
		return refDecision{kind: refDrop}
	}
	name := strconv.Itoa(j.n) + safeExt(hint)
	j.n++
	if err := os.WriteFile(filepath.Join(j.dir, name), b, 0o644); err != nil {
		j.err = err
		return refDecision{kind: refDrop}
	}
	return refDecision{kind: refReplace, repl: name}
}

func safeExt(ref string) string {
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	ext := filepath.Ext(ref)
	if len(ext) < 2 || len(ext) > 8 {
		return ""
	}
	for _, c := range ext[1:] {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		default:
			return ""
		}
	}
	return ext
}

// rewriteCSSURLs rewrites url() and @import in one CSS declaration or
// stylesheet. Remote urls stay as written. A local url is replaced, or
// becomes none when it cannot be copied. An @import that cannot be copied
// is removed through its semicolon, so the engine does not resolve it
// against the document base.
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
