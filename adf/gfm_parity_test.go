// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package adf_test

import (
	"context"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/adf"
	"github.com/marcelocantos/vellum/convert"
)

// gfmTablesAndHeadings is the shared fixture for ENT-010. Both pipelines
// must keep these headings and this table; a GFM fix on only one side
// fails this test.
const gfmTablesAndHeadings = `# Heading

| Col A | Col B |
| ----- | ----- |
| hello | world |

## Second heading
`

func TestGFMTablesAndHeadings_ConvertAndADF(t *testing.T) {
	html, _, err := convert.Render(context.Background(), []byte(gfmTablesAndHeadings), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<h1 id="heading">Heading</h1>`,
		`<h2 id="second-heading">Second heading</h2>`,
		`<th>Col A</th>`,
		`<th>Col B</th>`,
		`<td>hello</td>`,
		`<td>world</td>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("convert HTML missing %s\n%s", want, html)
		}
	}

	doc, err := adf.Convert(gfmTablesAndHeadings, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := textsOf(doc.Content, "heading"); !sameStrings(got, []string{"Heading", "Second heading"}) {
		t.Errorf("adf headings = %#v", got)
	}
	if got := textsOf(doc.Content, "tableHeader"); !sameStrings(got, []string{"Col A", "Col B"}) {
		t.Errorf("adf table headers = %#v", got)
	}
	if got := textsOf(doc.Content, "tableCell"); !sameStrings(got, []string{"hello", "world"}) {
		t.Errorf("adf table cells = %#v", got)
	}
}

func textsOf(nodes []adf.Node, typ string) []string {
	var out []string
	var walk func([]adf.Node)
	walk = func(ns []adf.Node) {
		for _, n := range ns {
			if n.Type == typ {
				out = append(out, strings.TrimSpace(nodeText(n)))
				continue
			}
			walk(n.Content)
		}
	}
	walk(nodes)
	return out
}

func nodeText(n adf.Node) string {
	if n.Text != "" {
		return n.Text
	}
	var b strings.Builder
	for _, c := range n.Content {
		b.WriteString(nodeText(c))
	}
	return b.String()
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
