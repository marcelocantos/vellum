// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"strings"
	"testing"
)

func TestStyleCSS_NilAndEmpty(t *testing.T) {
	// nil Style is treated as "no overrides except defaults" — bookmarks
	// default on, so CSS() is non-empty for nil too.
	var s *Style
	if got := s.CSS(); !strings.Contains(got, "bookmark-level") {
		t.Errorf("nil Style: want bookmark default CSS, got %q", got)
	}
	if !s.BookmarksOn() {
		t.Errorf("nil Style: BookmarksOn() should default true")
	}
	if s.PageNumbersOn() {
		t.Errorf("nil Style: PageNumbersOn() should default false")
	}
	if s.RunningHeadOn() {
		t.Errorf("nil Style: RunningHeadOn() should default false")
	}

	// Explicitly-empty Style: same defaults as nil.
	empty := &Style{}
	if got := empty.CSS(); !strings.Contains(got, "bookmark-level") {
		t.Errorf("empty Style: want bookmark default CSS, got %q", got)
	}

	// Explicit off-bookmarks → no CSS at all.
	off := false
	noBookmarks := &Style{Bookmarks: &off}
	if got := noBookmarks.CSS(); got != "" {
		t.Errorf("bookmarks-off Style: want empty CSS, got %q", got)
	}
}

func TestStyleCSS_Fields(t *testing.T) {
	s := &Style{
		FontSize:           "14px",
		LineHeight:         "1.4",
		FontFamily:         `"Georgia", serif`,
		CodeFontFamily:     "Menlo, monospace",
		PageSize:           "Letter",
		PageMargin:         "1cm",
		PageFirstTopMargin: "1.5cm",
	}
	css := s.CSS()
	mustContain := []string{
		"font-size: 14px",
		"line-height: 1.4",
		`font-family: "Georgia", serif`,
		"code, pre",
		"font-family: Menlo, monospace",
		"@page {",
		"size: Letter",
		"margin: 1cm",
		"@page :first {",
		"margin-top: 1.5cm",
	}
	for _, frag := range mustContain {
		if !strings.Contains(css, frag) {
			t.Errorf("CSS missing %q\nGot:\n%s", frag, css)
		}
	}
}

func TestStyleCSS_Partial(t *testing.T) {
	off := false
	s := &Style{FontSize: "12px", Bookmarks: &off}
	css := s.CSS()
	if !strings.Contains(css, "font-size: 12px") {
		t.Errorf("missing font-size rule: %q", css)
	}
	if strings.Contains(css, "@page") {
		t.Errorf("unexpected @page rule for FontSize-only Style: %q", css)
	}
	if strings.Contains(css, "code, pre") {
		t.Errorf("unexpected code/pre rule: %q", css)
	}
	if strings.Contains(css, "bookmark-level") {
		t.Errorf("bookmarks off but found bookmark CSS: %q", css)
	}
}

func TestStyleCSS_BooleanFlags(t *testing.T) {
	on := true
	s := &Style{PageNumbers: &on, RunningHead: &on}
	css := s.CSS()
	mustContain := []string{
		"@bottom-center",
		"content: counter(page)",
		"string-set: vellum-chapter",
		"@top-center",
		"content: string(vellum-chapter)",
		"bookmark-level: 1",
	}
	for _, frag := range mustContain {
		if !strings.Contains(css, frag) {
			t.Errorf("CSS missing %q\nGot:\n%s", frag, css)
		}
	}
}

func TestStyleOverlayOn(t *testing.T) {
	base := &Style{
		FontSize:   "14px",
		PageMargin: "1cm",
	}
	override := &Style{
		FontSize:           "12px",
		PageFirstTopMargin: "2cm",
	}
	merged := override.OverlayOn(base)

	if merged.FontSize != "12px" {
		t.Errorf("FontSize: want 12px, got %q", merged.FontSize)
	}
	if merged.PageMargin != "1cm" {
		t.Errorf("PageMargin: want 1cm (from base), got %q", merged.PageMargin)
	}
	if merged.PageFirstTopMargin != "2cm" {
		t.Errorf("PageFirstTopMargin: want 2cm, got %q", merged.PageFirstTopMargin)
	}

	// base must not be mutated.
	if base.FontSize != "14px" {
		t.Errorf("base.FontSize mutated: %q", base.FontSize)
	}
}

func TestStyleOverlayOn_Nils(t *testing.T) {
	var nilStyle *Style
	if got := nilStyle.OverlayOn(nil); got == nil || *got != (Style{}) {
		t.Errorf("nil.OverlayOn(nil): want empty Style, got %+v", got)
	}

	base := &Style{FontSize: "14px"}
	if got := nilStyle.OverlayOn(base); got.FontSize != "14px" {
		t.Errorf("nil.OverlayOn(base): want FontSize 14px, got %q", got.FontSize)
	}

	s := &Style{FontSize: "12px"}
	if got := s.OverlayOn(nil); got.FontSize != "12px" {
		t.Errorf("s.OverlayOn(nil): want FontSize 12px, got %q", got.FontSize)
	}
}
