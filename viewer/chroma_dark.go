// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"bytes"
	"regexp"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
)

var chromaDarkThemeCSS string

var chromaClassRe = regexp.MustCompile(`\.chroma \.([a-z0-9]+)`)

// github-dark omits several token classes that github defines with dark-on-light
// colors; unscoped light rules then win and text disappears on dark backgrounds.
var chromaGapColors = map[string]string{
	"nb": "#79c0ff", // NameBuiltin
	"bp": "#8b949e", // NameBuiltinPseudo
}

func init() {
	lightFmt := chromahtml.New(chromahtml.WithClasses(true), chromahtml.WithAllClasses(true))
	darkFmt := chromahtml.New(chromahtml.WithClasses(true), chromahtml.WithAllClasses(true))

	var lightBuf, darkBuf bytes.Buffer
	if err := lightFmt.WriteCSS(&lightBuf, styles.Get("github")); err != nil {
		panic("viewer: github chroma CSS: " + err.Error())
	}
	if err := darkFmt.WriteCSS(&darkBuf, styles.Get("github-dark")); err != nil {
		panic("viewer: github-dark chroma CSS: " + err.Error())
	}

	lightRaw := lightBuf.String()
	darkRaw := darkBuf.String()
	chromaDarkThemeCSS = scopeChromaCSS(darkRaw, `html[data-vellum-theme="dark"] .vellum-article `) +
		chromaGapCSS(lightRaw, darkRaw, `html[data-vellum-theme="dark"] .vellum-article `) +
		"@media (prefers-color-scheme: dark) {\n" +
		scopeChromaCSS(darkRaw, `html[data-vellum-theme="system"] .vellum-article `) +
		chromaGapCSS(lightRaw, darkRaw, `html[data-vellum-theme="system"] .vellum-article `) +
		"}\n"
}

func chromaTokenClasses(css string) map[string]bool {
	out := make(map[string]bool)
	for line := range strings.SplitSeq(css, "\n") {
		if m := chromaClassRe.FindStringSubmatch(line); len(m) == 2 {
			out[m[1]] = true
		}
	}
	return out
}

func chromaGapCSS(lightCSS, darkCSS, scope string) string {
	light := chromaTokenClasses(lightCSS)
	dark := chromaTokenClasses(darkCSS)
	var b strings.Builder
	for cls := range light {
		if dark[cls] {
			continue
		}
		color := chromaGapColors[cls]
		if color == "" {
			color = "#e6edf3"
		}
		b.WriteString(scope)
		b.WriteString(".chroma .")
		b.WriteString(cls)
		b.WriteString(" { color: ")
		b.WriteString(color)
		b.WriteString("; }\n")
	}
	return b.String()
}

// scopeChromaCSS prefixes every selector in a chroma stylesheet so light-theme
// rules embedded at convert time do not win inside the viewer dark themes.
func scopeChromaCSS(css, prefix string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(css, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, "*/"); idx >= 0 && strings.HasPrefix(line, "/*") {
			b.WriteString(line[:idx+2])
			line = strings.TrimSpace(line[idx+2:])
			if line == "" {
				b.WriteByte('\n')
				continue
			}
			b.WriteByte(' ')
		}
		open := strings.IndexByte(line, '{')
		if open < 0 {
			b.WriteString(line)
			b.WriteByte('\n')
			continue
		}
		sel := strings.TrimSpace(line[:open])
		rest := line[open:]
		for i, part := range strings.Split(sel, ",") {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(prefix)
			b.WriteString(strings.TrimSpace(part))
		}
		b.WriteString(" ")
		b.WriteString(rest)
		b.WriteByte('\n')
	}
	return b.String()
}
