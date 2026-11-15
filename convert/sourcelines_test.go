// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

var sourceSpanRe = regexp.MustCompile(`<(\w+)[^>]*? data-source-line="(\d+)" data-source-end="(\d+)"[^>]*>([^<]*)`)

// sourceLinesDoc puts a block after each construct that replaces source
// lines before goldmark sees them (front matter, display math, Mermaid),
// so a drifted line number shows up on the block that follows.
const sourceLinesDoc = `---
title: Lines
---
# Heading

First line of a paragraph
that wraps onto a second.

$$
a + b
$$

After math.

` + "```mermaid\ngraph TD\nA-->B\n```" + `

After mermaid.

- one
- two

> quoted
> twice

| a | b |
|---|---|
| 1 | 2 |

Last.
`

func renderSpans(t *testing.T, src string, sourceLines bool) map[string][2]string {
	t.Helper()
	html, _, err := Render(context.Background(), []byte(src), &Options{Defer: true, SourceLines: sourceLines})
	if err != nil {
		t.Fatal(err)
	}
	spans := map[string][2]string{}
	for _, m := range sourceSpanRe.FindAllStringSubmatch(html, -1) {
		text, _, _ := strings.Cut(strings.TrimSpace(m[4]), "\n")
		key := m[1] + ":" + strings.TrimSpace(text)
		if _, seen := spans[key]; !seen {
			spans[key] = [2]string{m[2], m[3]}
		}
	}
	return spans
}

func TestSourceLines_BlocksCarryTheirSourceSpan(t *testing.T) {
	for name, src := range map[string]string{
		"lf":   sourceLinesDoc,
		"crlf": strings.ReplaceAll(sourceLinesDoc, "\n", "\r\n"),
	} {
		t.Run(name, func(t *testing.T) {
			spans := renderSpans(t, src, true)
			for key, want := range map[string][2]string{
				"h1:Heading":                  {"4", "4"},
				"p:First line of a paragraph": {"6", "7"},
				"p:After math.":               {"13", "13"},
				"p:After mermaid.":            {"20", "20"},
				"ul:":                         {"22", "23"},
				"li:one":                      {"22", "22"},
				"li:two":                      {"23", "23"},
				"blockquote:":                 {"25", "26"},
				"table:":                      {"28", "30"},
				"p:Last.":                     {"32", "32"},
			} {
				if got, ok := spans[key]; !ok || got != want {
					t.Errorf("%s: lines %v (present %v), want %v", key, got, ok, want)
				}
			}
		})
	}
}

func TestSourceLines_OffByDefault(t *testing.T) {
	html, _, err := Render(context.Background(), []byte(sourceLinesDoc), &Options{Defer: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "data-source-") {
		t.Fatal("source line attributes without Options.SourceLines")
	}
}
