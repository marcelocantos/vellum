// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package mdmath finds the $…$ and $$…$$ math in Markdown source that is
// not code. Code is what CommonMark says it is (fenced blocks of ``` or
// ~~~ indented up to three spaces, indented code blocks, and code spans
// of any backtick count), found by parsing the source with goldmark, the
// parser that renders it. A dollar sign inside code stays a dollar sign.
package mdmath

import (
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// Span is one math expression in the source.
type Span struct {
	// Start and End are the byte range of the expression, delimiters
	// included. A display span runs from its opening "$$" line to the end
	// of its closing "$$" line (newline excluded).
	Start, End int
	// Expr is the expression between the delimiters, trimmed.
	Expr string
	// Display marks $$…$$ on lines of their own.
	Display bool
}

var (
	// displayDelimRe is a line that opens or closes display math.
	displayDelimRe = regexp.MustCompile(`^\$\$[ \t]*\r?$`)
	// inlineMathRe is $…$ on one line, anchored at the opening dollar.
	inlineMathRe = regexp.MustCompile(`^\$([^\n$]+?)\$`)

	codeParser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()
)

// Find returns the math in src outside code, in source order.
func Find(src string) []Span {
	code := codeRanges([]byte(src))
	var spans []Span

	// Display math: a "$$" line, then the next "$$" line, neither of them
	// inside code, with at least one line between.
	type line struct{ start, end int }
	var lines []line
	for start := 0; start < len(src); {
		end := strings.IndexByte(src[start:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += start
		}
		lines = append(lines, line{start, end})
		start = end + 1
	}
	isDelim := func(l line) bool {
		return displayDelimRe.MatchString(src[l.start:l.end]) && !code.has(l.start)
	}
	for i := 0; i < len(lines); i++ {
		if !isDelim(lines[i]) {
			continue
		}
		for j := i + 2; j < len(lines); j++ {
			if !isDelim(lines[j]) {
				continue
			}
			expr := strings.TrimSpace(src[lines[i].end+1 : lines[j].start])
			if expr != "" {
				spans = append(spans, Span{Start: lines[i].start, End: lines[j].end, Expr: expr, Display: true})
				i = j
			}
			break
		}
	}

	// Inline math in what is left: both dollars outside code. A rejected
	// candidate moves on by one byte, so a dollar inside code never hides
	// real math after it.
	display := spans
	next := 0 // index of the next display span at or after i
	for i := 0; i < len(src); {
		for next < len(display) && display[next].End <= i {
			next++
		}
		if next < len(display) && i >= display[next].Start {
			i = display[next].End
			continue
		}
		if src[i] != '$' || code.has(i) {
			i++
			continue
		}
		m := inlineMathRe.FindStringSubmatchIndex(src[i:])
		if m == nil || code.has(i+m[1]-1) || (next < len(display) && i+m[1] > display[next].Start) {
			i++
			continue
		}
		spans = append(spans, Span{Start: i, End: i + m[1], Expr: strings.TrimSpace(src[i+m[2] : i+m[3]])})
		i += m[1]
	}
	sort.SliceStable(spans, func(a, b int) bool { return spans[a].Start < spans[b].Start })
	return spans
}

// ranges is a sorted list of disjoint [start, end) byte ranges.
type ranges [][2]int

func (r ranges) has(off int) bool {
	i := sort.Search(len(r), func(i int) bool { return r[i][1] > off })
	return i < len(r) && r[i][0] <= off
}

// codeRanges returns where src has code: fenced and indented code block
// content (and a fence's info string), and code span content.
func codeRanges(src []byte) ranges {
	doc := codeParser.Parse(text.NewReader(src))
	var out ranges
	add := func(start, stop int) {
		if stop > start {
			out = append(out, [2]int{start, stop})
		}
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.FencedCodeBlock:
			if n.Info != nil {
				add(n.Info.Segment.Start, n.Info.Segment.Stop)
			}
			for i := 0; i < n.Lines().Len(); i++ {
				seg := n.Lines().At(i)
				add(seg.Start, seg.Stop)
			}
			return ast.WalkSkipChildren, nil
		case *ast.CodeBlock:
			for i := 0; i < n.Lines().Len(); i++ {
				seg := n.Lines().At(i)
				add(seg.Start, seg.Stop)
			}
			return ast.WalkSkipChildren, nil
		case *ast.CodeSpan:
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					add(t.Segment.Start, t.Segment.Stop)
				}
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	// Merge, so has() can binary-search.
	merged := out[:0]
	for _, r := range out {
		if len(merged) > 0 && r[0] <= merged[len(merged)-1][1] {
			if r[1] > merged[len(merged)-1][1] {
				merged[len(merged)-1][1] = r[1]
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
}
