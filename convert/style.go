// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"fmt"
	"strings"
)

// Style holds optional CSS-valued knobs for tweaking the default document
// style. Any empty field inherits the built-in default from embed/github.css.
// Values are interpolated as-is into CSS, so each must be a valid CSS value
// for its property (e.g. "14px", "1cm", "1.4", "A4", "Georgia, serif").
type Style struct {
	FontSize           string `yaml:"font_size,omitempty" json:"font_size,omitempty" jsonschema:"body font size (CSS value, e.g. 14px)"`
	LineHeight         string `yaml:"line_height,omitempty" json:"line_height,omitempty" jsonschema:"body line height (CSS value, e.g. 1.4)"`
	FontFamily         string `yaml:"font_family,omitempty" json:"font_family,omitempty" jsonschema:"body font-family (CSS value)"`
	CodeFontFamily     string `yaml:"code_font_family,omitempty" json:"code_font_family,omitempty" jsonschema:"font-family applied to code and pre (CSS value)"`
	PageSize           string `yaml:"page_size,omitempty" json:"page_size,omitempty" jsonschema:"@page size (e.g. A4, Letter)"`
	PageMargin         string `yaml:"page_margin,omitempty" json:"page_margin,omitempty" jsonschema:"@page margin (e.g. 1cm)"`
	PageFirstTopMargin string `yaml:"page_first_top_margin,omitempty" json:"page_first_top_margin,omitempty" jsonschema:"@page :first margin-top (e.g. 1.5cm)"`

	// Defaults to nil/false. Use pointer-to-bool so callers can distinguish
	// "explicitly off" from "not set" when overlaying styles. Bookmarks
	// default ON (see effective accessors below).
	PageNumbers *bool `yaml:"page_numbers,omitempty" json:"page_numbers,omitempty" jsonschema:"emit page numbers at @bottom-center (default off)"`
	RunningHead *bool `yaml:"running_head,omitempty" json:"running_head,omitempty" jsonschema:"emit current H1 text as a running head at @top-center (default off)"`
	Bookmarks   *bool `yaml:"bookmarks,omitempty" json:"bookmarks,omitempty" jsonschema:"emit PDF outline bookmarks from H1..H6 (default on)"`
	Hyphenate   *bool `yaml:"hyphenate,omitempty" json:"hyphenate,omitempty" jsonschema:"enable automatic hyphenation (default off; works out-of-the-box on WeasyPrint, needs dictionary setup on Prince)"`

	// Lang is the document language as a BCP-47 tag (e.g. en, en-GB, de).
	// When set, it lands on <html lang=...>. Required for hyphenation to
	// engage. Defaults to "en" when Hyphenate is on and Lang is empty.
	Lang string `yaml:"lang,omitempty" json:"lang,omitempty" jsonschema:"document language (BCP-47 tag, e.g. en); required for hyphenation"`

	// PDFA requests a PDF/A archival-profile output (e.g. "PDF/A-1b",
	// "PDF/A-2b", "PDF/A-3b"). Empty = standard PDF. Not CSS — passed
	// to the renderer as a command-line flag.
	PDFA string `yaml:"pdfa,omitempty" json:"pdfa,omitempty" jsonschema:"PDF/A archival profile (e.g. PDF/A-3b); empty = standard PDF"`
}

// PageNumbersOn returns the effective value (default false).
func (s *Style) PageNumbersOn() bool {
	return s != nil && s.PageNumbers != nil && *s.PageNumbers
}

// RunningHeadOn returns the effective value (default false).
func (s *Style) RunningHeadOn() bool {
	return s != nil && s.RunningHead != nil && *s.RunningHead
}

// BookmarksOn returns the effective value (default true).
func (s *Style) BookmarksOn() bool {
	if s == nil || s.Bookmarks == nil {
		return true
	}
	return *s.Bookmarks
}

// HyphenateOn returns the effective value (default false).
func (s *Style) HyphenateOn() bool {
	return s != nil && s.Hyphenate != nil && *s.Hyphenate
}

// EffectiveLang returns Lang, defaulting to "en" when Hyphenate is on but no
// Lang is explicitly set. Returns "" when neither Lang is set nor hyphenation
// is on, so the <html> tag stays bare.
func (s *Style) EffectiveLang() string {
	if s == nil {
		return ""
	}
	if s.Lang != "" {
		return s.Lang
	}
	if s.HyphenateOn() {
		return "en"
	}
	return ""
}

// OverlayOn returns a new Style where each non-empty field of s overrides the
// corresponding field of base. Nil receivers and nil base behave as empty.
func (s *Style) OverlayOn(base *Style) *Style {
	var out Style
	if base != nil {
		out = *base
	}
	if s == nil {
		return &out
	}
	if s.FontSize != "" {
		out.FontSize = s.FontSize
	}
	if s.LineHeight != "" {
		out.LineHeight = s.LineHeight
	}
	if s.FontFamily != "" {
		out.FontFamily = s.FontFamily
	}
	if s.CodeFontFamily != "" {
		out.CodeFontFamily = s.CodeFontFamily
	}
	if s.PageSize != "" {
		out.PageSize = s.PageSize
	}
	if s.PageMargin != "" {
		out.PageMargin = s.PageMargin
	}
	if s.PageFirstTopMargin != "" {
		out.PageFirstTopMargin = s.PageFirstTopMargin
	}
	if s.PageNumbers != nil {
		out.PageNumbers = s.PageNumbers
	}
	if s.RunningHead != nil {
		out.RunningHead = s.RunningHead
	}
	if s.Bookmarks != nil {
		out.Bookmarks = s.Bookmarks
	}
	if s.Hyphenate != nil {
		out.Hyphenate = s.Hyphenate
	}
	if s.Lang != "" {
		out.Lang = s.Lang
	}
	if s.PDFA != "" {
		out.PDFA = s.PDFA
	}
	return &out
}

// CSS returns a CSS fragment with rules for the non-empty fields, ready to
// append after the base stylesheet. Empty/nil Style returns "".
func (s *Style) CSS() string {
	// Treat nil as an empty Style so default-on flags (like Bookmarks) still
	// emit their CSS even for callers that don't supply any overrides.
	if s == nil {
		s = &Style{}
	}
	var body, page strings.Builder
	if s.FontSize != "" {
		fmt.Fprintf(&body, "  font-size: %s;\n", s.FontSize)
	}
	if s.LineHeight != "" {
		fmt.Fprintf(&body, "  line-height: %s;\n", s.LineHeight)
	}
	if s.FontFamily != "" {
		fmt.Fprintf(&body, "  font-family: %s;\n", s.FontFamily)
	}
	if s.PageSize != "" {
		fmt.Fprintf(&page, "  size: %s;\n", s.PageSize)
	}
	if s.PageMargin != "" {
		fmt.Fprintf(&page, "  margin: %s;\n", s.PageMargin)
	}

	var out strings.Builder
	if body.Len() > 0 {
		fmt.Fprintf(&out, "body {\n%s}\n", body.String())
	}
	if s.CodeFontFamily != "" {
		fmt.Fprintf(&out, "code, pre {\n  font-family: %s;\n}\n", s.CodeFontFamily)
	}
	if page.Len() > 0 {
		fmt.Fprintf(&out, "@page {\n%s}\n", page.String())
	}
	if s.PageFirstTopMargin != "" {
		fmt.Fprintf(&out, "@page :first {\n  margin-top: %s;\n}\n", s.PageFirstTopMargin)
	}
	if s.PageNumbersOn() {
		out.WriteString(pageNumbersCSS)
	}
	if s.RunningHeadOn() {
		out.WriteString(runningHeadCSS)
	}
	if s.BookmarksOn() {
		out.WriteString(bookmarksCSS)
	}
	if s.HyphenateOn() {
		out.WriteString(hyphenateCSS)
	}
	return out.String()
}

// CSS fragments for the boolean style options. Kept as constants so they
// can be inspected and tested without re-deriving from the struct.

const pageMarginNoteCSS = `font-size: 0.85em; color: #656d76;`

const pageNumbersCSS = `@page {
  @bottom-center {
    content: counter(page);
    ` + pageMarginNoteCSS + `
  }
}
`

// content(text) captures the heading's textual content into a named CSS
// string; string(...) retrieves it in the @page margin context. Supported
// by both Prince and WeasyPrint.
const runningHeadCSS = `h1 { string-set: vellum-chapter content(text); }
@page {
  @top-center {
    content: string(vellum-chapter);
    ` + pageMarginNoteCSS + `
  }
}
`

// bookmark-level: <n> emits a PDF outline entry at depth <n>.
const bookmarksCSS = `h1 { bookmark-level: 1; bookmark-state: open; }
h2 { bookmark-level: 2; bookmark-state: closed; }
h3 { bookmark-level: 3; bookmark-state: closed; }
h4 { bookmark-level: 4; bookmark-state: closed; }
h5 { bookmark-level: 5; bookmark-state: closed; }
h6 { bookmark-level: 6; bookmark-state: closed; }
`

// hyphens: auto engages the renderer's hyphenation engine when a lang attribute
// is present on the document or element. WeasyPrint uses pyphen dictionaries;
// Prince needs an explicit prince-hyphenate-dictionary setup (not bundled).
const hyphenateCSS = `body { hyphens: auto; -webkit-hyphens: auto; }
`
