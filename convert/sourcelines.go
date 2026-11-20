// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"sort"
	"strconv"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// SourceLineAttr and SourceEndAttr are the attributes Options.SourceLines
// puts on rendered blocks: the 1-based source lines of the block's first
// and last content. The viewer maps scroll positions through them, so
// the editor opens where the reader is looking, and closes back there.
const (
	SourceLineAttr = "data-source-line"
	SourceEndAttr  = "data-source-end"
)

// sourceLinesKey marks a parse that should annotate blocks; the
// transformer is installed on the shared goldmark instance and does
// nothing without it.
var sourceLinesKey = parser.NewContextKey()

// sourceLinesPriority runs the transformer after every extension's own
// AST rewrite (lower values run first).
const sourceLinesPriority = 10000

type sourceLineTransformer struct{}

func (sourceLineTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	if pc.Get(sourceLinesKey) == nil {
		return
	}
	src := reader.Source()
	var newlines []int
	for i, b := range src {
		if b == '\n' {
			newlines = append(newlines, i)
		}
	}
	lineOf := func(offset int) int {
		return sort.SearchInts(newlines, offset) + 1
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Type() != ast.TypeBlock || n.Kind() == ast.KindDocument {
			return ast.WalkContinue, nil
		}
		start, ok := blockStart(n)
		if !ok {
			return ast.WalkContinue, nil
		}
		stop, _ := blockStop(n)
		first, last := lineOf(start), lineOf(stop)
		// A fenced block's lines are its content; the fences are outside.
		if n.Kind() == ast.KindFencedCodeBlock {
			first, last = max(first-1, 1), last+1
		}
		n.SetAttributeString(SourceLineAttr, []byte(strconv.Itoa(first)))
		n.SetAttributeString(SourceEndAttr, []byte(strconv.Itoa(max(last, first))))
		return ast.WalkContinue, nil
	})
	annotateTasks(doc, lineOf)
}

// blockStart is the source offset of a block's first line: its own
// segments for a leaf, else its first descendant's.
func blockStart(n ast.Node) (int, bool) {
	if lines := n.Lines(); lines != nil && lines.Len() > 0 {
		return lines.At(0).Start, true
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if c.Type() != ast.TypeBlock {
			continue
		}
		if off, ok := blockStart(c); ok {
			return off, true
		}
	}
	return 0, false
}

// blockStop is the source offset of a block's last content byte: its
// own last segment for a leaf, else its last descendant's.
func blockStop(n ast.Node) (int, bool) {
	if lines := n.Lines(); lines != nil && lines.Len() > 0 {
		seg := lines.At(lines.Len() - 1)
		return max(seg.Stop-1, seg.Start), true
	}
	for c := n.LastChild(); c != nil; c = c.PreviousSibling() {
		if c.Type() != ast.TypeBlock {
			continue
		}
		if off, ok := blockStop(c); ok {
			return off, true
		}
	}
	return 0, false
}

// lineCount is the number of newlines in s; preprocessors pad their
// placeholders with it so goldmark sees every later block at its line.
func lineCount(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			n++
		}
	}
	return n
}
