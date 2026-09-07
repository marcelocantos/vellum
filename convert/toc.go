// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// tocMaxLevel is the deepest heading included in a generated Contents
// block (h1..h3). Deeper headings stay in the document, not the list.
const tocMaxLevel = 3

// tocHintRe matches a vellum TOC placeholder in rendered HTML. The
// paragraph-wrapped form is tried first so a goldmark <p><!-- … --></p>
// wrapper is consumed as one unit rather than leaving an empty <p>.
var tocHintRe = regexp.MustCompile(`(?s)<p>\s*<!--\s*vellum:toc\s*-->\s*</p>|<!--\s*vellum:toc\s*-->`)

var (
	headingHTMLRe = regexp.MustCompile(`(?is)<h([1-6])\b([^>]*)>(.*?)</h[1-6]>`)
	headingIDRe   = regexp.MustCompile(`(?i)\bid\s*=\s*["']([^"']+)["']`)
	htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	htmlTagRe     = regexp.MustCompile(`<[^>]*>`)
)

type tocHeading struct {
	Level int
	ID    string
	Text  string
}

type tocItem struct {
	tocHeading
	Kids []tocItem
}

// applyTOC inserts a static Contents block when the document asked for
// one: a <!-- vellum:toc --> hint in the rendered HTML, or style.toc.
// The hint wins for placement. Returns the (possibly unchanged) body and
// whether a Contents block was injected.
func applyTOC(body string, style *Style) (string, bool) {
	loc := tocHintRe.FindStringIndex(body)
	want := loc != nil || style.TOCOn()
	if !want {
		return body, false
	}

	heads := collectHeadings(body, tocMaxLevel)
	if len(heads) == 0 {
		if loc != nil {
			return tocHintRe.ReplaceAllString(body, ""), false
		}
		return body, false
	}

	block := renderTOC(heads)
	if loc != nil {
		body = body[:loc[0]] + block + body[loc[1]:]
		body = tocHintRe.ReplaceAllString(body, "")
		return body, true
	}
	return block + body, true
}

func collectHeadings(body string, maxLevel int) []tocHeading {
	var out []tocHeading
	for _, m := range headingHTMLRe.FindAllStringSubmatch(body, -1) {
		level, err := strconv.Atoi(m[1])
		if err != nil || level < 1 || level > maxLevel {
			continue
		}
		id := ""
		if idm := headingIDRe.FindStringSubmatch(m[2]); len(idm) == 2 {
			id = idm[1]
		}
		if id == "" {
			continue
		}
		text := headingText(m[3])
		if text == "" {
			continue
		}
		out = append(out, tocHeading{Level: level, ID: id, Text: text})
	}
	return out
}

func headingText(inner string) string {
	s := htmlCommentRe.ReplaceAllString(inner, "")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

func nestTOC(heads []tocHeading) []tocItem {
	var walk func(start, parentLevel int) (int, []tocItem)
	walk = func(start, parentLevel int) (int, []tocItem) {
		var items []tocItem
		i := start
		for i < len(heads) {
			h := heads[i]
			if h.Level <= parentLevel {
				break
			}
			item := tocItem{tocHeading: h}
			var kids []tocItem
			i++
			i, kids = walk(i, h.Level)
			item.Kids = kids
			items = append(items, item)
		}
		return i, items
	}
	_, items := walk(0, 0)
	return items
}

func renderTOC(heads []tocHeading) string {
	var b strings.Builder
	b.WriteString(`<div class="vellum-contents">`)
	b.WriteString("\n<h2>Contents</h2>\n")
	writeTOCItems(&b, nestTOC(heads))
	b.WriteString("</div>\n")
	return b.String()
}

func writeTOCItems(b *strings.Builder, items []tocItem) {
	b.WriteString("<ul>\n")
	for _, it := range items {
		b.WriteString(`<li><a href="#`)
		b.WriteString(html.EscapeString(it.ID))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(it.Text))
		b.WriteString("</a>")
		if len(it.Kids) > 0 {
			b.WriteByte('\n')
			writeTOCItems(b, it.Kids)
		}
		b.WriteString("</li>\n")
	}
	b.WriteString("</ul>\n")
}

// tocCSS styles an in-document Contents block. Visual rules apply in
// HTML/email; leader() and target-counter() are paged-media extras that
// lightweight renderers ignore. Harmless when no .vellum-contents exists.
const tocCSS = `.vellum-contents {
  margin: 0 0 24px 0;
  break-after: page;
}
.vellum-contents > h2 {
  bookmark-level: none;
}
.vellum-contents ul {
  list-style: none;
  padding-left: 0;
  margin: 0 0 16px 0;
}
.vellum-contents li {
  margin: 0.15em 0;
}
.vellum-contents li ul {
  padding-left: 1.25em;
  margin: 0;
}
.vellum-contents a {
  text-decoration: none;
}
.vellum-contents a::after {
  content: leader(dotted) " " target-counter(attr(href), page);
  color: #656d76;
}
`
