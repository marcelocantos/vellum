// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package svgfit

import (
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// fontRule is one selector of a <style> rule that sets font-size.
type fontRule struct {
	sel       selector
	value     string
	important bool
	order     int // position in the sheet; later wins a tie
}

// outranks reports whether r wins over other for an element both match.
func (r *fontRule) outranks(other *fontRule) bool {
	if r.important != other.important {
		return r.important
	}
	if r.sel.specificity != other.sel.specificity {
		return r.sel.specificity > other.sel.specificity
	}
	return r.order > other.order
}

// fontValue is a font-size found among declarations.
type fontValue struct {
	value     string
	important bool
}

// fontRules collects the font-size rules of every <style> under root.
func fontRules(root *html.Node) []fontRule {
	var sheet strings.Builder
	var collect func(n *html.Node)
	collect = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "style") {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					sheet.WriteString(c.Data)
				}
			}
			sheet.WriteByte('\n')
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(root)
	return parseSheet(sheet.String())
}

// parseSheet reads the style rules of a sheet. At-rules (@media,
// @font-face, @keyframes) are skipped whole: none sets the size text is
// drawn at on an ordinary screen.
func parseSheet(css string) []fontRule {
	css = stripComments(css)
	var rules []fontRule
	for {
		open := strings.IndexByte(css, '{')
		if open < 0 {
			return rules
		}
		prelude := strings.TrimSpace(css[:open])
		shut := blockEnd(css, open)
		block := css[open+1 : shut]
		css = css[min(shut+1, len(css)):]
		// A statement at-rule (@import …;) may sit before the prelude.
		if semi := strings.LastIndexByte(prelude, ';'); semi >= 0 {
			prelude = strings.TrimSpace(prelude[semi+1:])
		}
		if prelude == "" || strings.HasPrefix(prelude, "@") {
			continue
		}
		font := fontDeclaration(block)
		if font.value == "" {
			continue
		}
		for text := range strings.SplitSeq(prelude, ",") {
			if sel, ok := parseSelector(text); ok {
				rules = append(rules, fontRule{sel: sel, value: font.value, important: font.important, order: len(rules)})
			}
		}
	}
}

func stripComments(css string) string {
	for {
		start := strings.Index(css, "/*")
		if start < 0 {
			return css
		}
		end := strings.Index(css[start+2:], "*/")
		if end < 0 {
			return css[:start]
		}
		css = css[:start] + " " + css[start+2+end+2:]
	}
}

// blockEnd is the index of the brace closing the block opened at open,
// or len(css) when it never closes.
func blockEnd(css string, open int) int {
	depth := 0
	for i := open; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return len(css)
}

// declaration returns the value of property prop in a declaration list
// (a style attribute or a rule's block), lower-cased, without !important.
// The last one wins, as in CSS.
func declaration(decls, prop string) string {
	value, _ := lookup(decls, prop)
	return value
}

func lookup(decls, prop string) (value string, important bool) {
	for decl := range strings.SplitSeq(decls, ";") {
		name, v, ok := strings.Cut(decl, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), prop) {
			continue
		}
		v = strings.ToLower(strings.TrimSpace(v))
		before, found := strings.CutSuffix(v, "!important")
		value, important = strings.TrimSpace(before), found
	}
	return value, important
}

// fontDeclaration finds the font size a declaration list sets, from
// font-size or from the font shorthand (whichever is written last).
func fontDeclaration(decls string) fontValue {
	var out fontValue
	for decl := range strings.SplitSeq(decls, ";") {
		name, _, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "font-size":
			v, important := lookup(decl, "font-size")
			out = fontValue{v, important}
		case "font":
			v, important := lookup(decl, "font")
			if size := shorthandSize(v); size != "" {
				out = fontValue{size, important}
			}
		}
	}
	return out
}

// shorthandSize picks the size out of a font shorthand: the first word
// that is a length with a unit ("12px/1.4" counts, a bare weight such as
// 400 does not) or a size keyword.
func shorthandSize(v string) string {
	for word := range strings.FieldsSeq(v) {
		word, _, _ = strings.Cut(word, "/")
		if _, ok := fontKeywords[word]; ok {
			return word
		}
		if _, unit, ok := splitLength(word); ok && unit != "" {
			return word
		}
	}
	return ""
}

// selector is a complex selector limited to what Measure can match
// without a layout: compounds of a tag, ids and classes, joined by
// descendant and child combinators.
type selector struct {
	compounds   []compound // outermost first
	specificity int
}

type compound struct {
	tag     string // "" matches any element
	ids     []string
	classes []string
	child   bool // joined to the compound before it by ">"
}

const (
	// Specificity packs (ids, classes, tags) into one comparable number.
	idWeight    = 1 << 16
	classWeight = 1 << 8
)

// parseSelector reads one selector. ok is false for anything outside the
// supported subset; the rule is then ignored for that selector.
func parseSelector(text string) (selector, bool) {
	var sel selector
	text = strings.TrimSpace(text)
	if text == "" {
		return sel, false
	}
	child := false
	for text != "" {
		text = strings.TrimLeft(text, " \t\r\n")
		if strings.HasPrefix(text, ">") {
			if child || len(sel.compounds) == 0 {
				return sel, false
			}
			child = true
			text = text[1:]
			continue
		}
		c := compound{child: child}
		child = false
		end := 0
		// A compound runs to the next combinator.
		for end < len(text) && !strings.ContainsRune(" \t\r\n>", rune(text[end])) {
			end++
		}
		part := text[:end]
		text = text[end:]
		if part == "" {
			return sel, false
		}
		for part != "" {
			kind := part[0]
			if kind == '#' || kind == '.' {
				part = part[1:]
			}
			n := 0
			for n < len(part) && isIdent(part[n]) {
				n++
			}
			name := part[:n]
			part = part[n:]
			switch {
			case kind == '*' && c.tag == "" && len(c.ids)+len(c.classes) == 0:
				part = part[1:]
			case name == "":
				return sel, false
			case kind == '#':
				c.ids = append(c.ids, name)
				sel.specificity += idWeight
			case kind == '.':
				c.classes = append(c.classes, name)
				sel.specificity += classWeight
			case c.tag == "" && len(c.ids)+len(c.classes) == 0:
				c.tag = strings.ToLower(name)
				sel.specificity++
			default:
				return sel, false
			}
		}
		sel.compounds = append(sel.compounds, c)
	}
	if child || len(sel.compounds) == 0 {
		return sel, false
	}
	return sel, true
}

func isIdent(b byte) bool {
	return b == '-' || b == '_' || b >= 0x80 ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// matches reports whether the selector matches the last element of chain.
func (s selector) matches(chain []elem) bool {
	return matchFrom(s.compounds, chain)
}

func matchFrom(compounds []compound, chain []elem) bool {
	if len(compounds) == 0 {
		return true
	}
	if len(chain) == 0 {
		return false
	}
	last := compounds[len(compounds)-1]
	if !last.matches(chain[len(chain)-1]) {
		return false
	}
	rest, above := compounds[:len(compounds)-1], chain[:len(chain)-1]
	if len(rest) == 0 {
		return true
	}
	if last.child {
		return matchFrom(rest, above)
	}
	for ; len(above) > 0; above = above[:len(above)-1] {
		if matchFrom(rest, above) {
			return true
		}
	}
	return false
}

func (c compound) matches(e elem) bool {
	if c.tag != "" && c.tag != e.tag {
		return false
	}
	for _, id := range c.ids {
		if id != e.id {
			return false
		}
	}
	for _, class := range c.classes {
		if !slices.Contains(e.classes, class) {
			return false
		}
	}
	return true
}
