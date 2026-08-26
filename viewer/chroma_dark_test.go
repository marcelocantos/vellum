// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"strings"
	"testing"
)

func TestScopeChromaCSS_PrefixesSelectors(t *testing.T) {
	in := "/* Keyword */ .chroma .k { color: #f00 }\n/* Background */ .bg { color: #fff; background-color: #000; }\n"
	out := scopeChromaCSS(in, `html[data-vellum-theme="dark"] .vellum-article `)
	want := `/* Keyword */ html[data-vellum-theme="dark"] .vellum-article .chroma .k { color: #f00 }
/* Background */ html[data-vellum-theme="dark"] .vellum-article .bg { color: #fff; background-color: #000; }
`
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestChromaDarkThemeCSS_IncludesGitHubDarkTokens(t *testing.T) {
	if chromaDarkThemeCSS == "" {
		t.Fatal("empty chroma dark CSS")
	}
	for _, needle := range []string{
		`/* Keyword */ html[data-vellum-theme="dark"] .vellum-article .chroma .k { color: #ff7b72 }`,
		`html[data-vellum-theme="system"] .vellum-article .chroma .s { color: #a5d6ff }`,
		`html[data-vellum-theme="dark"] .vellum-article .chroma .nx { color: #e6edf3; }`,
		`html[data-vellum-theme="dark"] .vellum-article .chroma .nb { color: #79c0ff; }`,
		"@media (prefers-color-scheme: dark)",
	} {
		if !strings.Contains(chromaDarkThemeCSS, needle) {
			t.Fatalf("missing %q in chroma dark CSS", needle)
		}
	}
}

func TestChromaGapCSS_FillsLightOnlyClasses(t *testing.T) {
	light := "/* NameOther */ .chroma .nx { color: #1f2328 }\n/* Keyword */ .chroma .k { color: #f00 }\n"
	dark := "/* Keyword */ .chroma .k { color: #ff7b72 }\n"
	out := chromaGapCSS(light, dark, "html[data-vellum-theme=\"dark\"] .vellum-article ")
	if !strings.Contains(out, `.chroma .nx { color: #e6edf3; }`) {
		t.Fatalf("missing nx gap fill: %q", out)
	}
	if strings.Contains(out, `.chroma .k`) {
		t.Fatalf("should not override class present in dark style: %q", out)
	}
}
