// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Attributes Options.SourceLines puts on a GFM task list item, so the
// viewer can name the exact task a reader clicked: its position among the
// document's task checkboxes, and the 1-based source line of its marker.
const (
	TaskIndexAttr = "data-task-index"
	TaskLineAttr  = "data-task-line"
)

// taskItemClass marks a task list item in the rendered page.
const taskItemClass = "task-list-item"

// Task is one GFM task list checkbox as Render sees it.
type Task struct {
	// Index is the task's position among the document's checkboxes, in
	// document order: the TaskIndexAttr a SourceLines render gives it.
	Index int
	// Line is the 1-based source line of the marker (TaskLineAttr).
	Line int
	// Offset is the byte offset in the source of the marker's "[".
	Offset int
	// Checked is the marker's state: "[x]" or "[X]" rather than "[ ]".
	Checked bool
}

// Tasks lists the task checkboxes of src exactly as Render finds them:
// after the same math and Mermaid extraction, through the same parser, so
// a marker in a code block, a diagram or display math is not a task and
// one inside a block quote is. Offsets are into src itself.
func Tasks(src []byte) ([]Task, error) {
	math := newMathPreprocessor()
	mermaid := newMermaidPreprocessor(MermaidSVG)
	processed := []byte(mermaid.Extract(math.Extract(string(src))))
	doc := md.Parser().Parse(text.NewReader(processed))
	var tasks []Task
	for _, n := range taskBoxes(doc) {
		off, ok := taskMarkerOffset(n)
		if !ok {
			return nil, fmt.Errorf("task %d: no source position", len(tasks))
		}
		line, col := lineAndColumn(processed, off)
		orig, ok := offsetAt(src, line, col)
		// Extraction keeps every line where it was and never touches a
		// list marker, so the original has the same marker at the same
		// place. Anything else is a renderer this code does not
		// understand: refuse rather than guess.
		if !ok || orig+3 > len(src) || !bytes.Equal(src[orig:orig+3], processed[off:off+3]) {
			return nil, fmt.Errorf("task %d: marker at line %d does not match the source", len(tasks), line)
		}
		tasks = append(tasks, Task{Index: len(tasks), Line: line, Offset: orig, Checked: n.IsChecked})
	}
	return tasks, nil
}

// taskBoxes returns the document's task checkboxes in document order.
func taskBoxes(doc ast.Node) []*extast.TaskCheckBox {
	var out []*extast.TaskCheckBox
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if box, ok := n.(*extast.TaskCheckBox); ok {
				out = append(out, box)
			}
		}
		return ast.WalkContinue, nil
	})
	return out
}

// taskMarkerOffset is the source offset of box's "[": the GFM parser
// only makes a checkbox from the first characters of a list item's first
// line, so its text block's first segment starts there.
func taskMarkerOffset(box *extast.TaskCheckBox) (int, bool) {
	block := box.Parent()
	if block == nil || block.Lines() == nil || block.Lines().Len() == 0 {
		return 0, false
	}
	return block.Lines().At(0).Start, true
}

// annotateTasks gives each task list item TaskIndexAttr, TaskLineAttr and
// the task-list-item class, and marks its checkbox for taskBoxRenderer.
func annotateTasks(doc ast.Node, lineOf func(int) int) {
	for i, box := range taskBoxes(doc) {
		off, ok := taskMarkerOffset(box)
		if !ok {
			continue
		}
		item := box.Parent().Parent()
		if _, isItem := item.(*ast.ListItem); !isItem {
			continue
		}
		index := []byte(strconv.Itoa(i))
		item.SetAttributeString(TaskIndexAttr, index)
		item.SetAttributeString(TaskLineAttr, []byte(strconv.Itoa(lineOf(off))))
		class := []byte(taskItemClass)
		if v, ok := item.AttributeString("class"); ok {
			if b, ok := v.([]byte); ok && len(b) > 0 {
				class = append(append(append([]byte(nil), b...), ' '), class...)
			}
		}
		item.SetAttributeString("class", class)
		box.SetAttributeString(TaskIndexAttr, index)
	}
}

// lineAndColumn returns the 1-based line of off in src and its byte
// column within that line.
func lineAndColumn(src []byte, off int) (line, col int) {
	start := bytes.LastIndexByte(src[:off], '\n') + 1
	return bytes.Count(src[:off], []byte{'\n'}) + 1, off - start
}

// offsetAt is the byte offset of (line, col) in src.
func offsetAt(src []byte, line, col int) (int, bool) {
	start := 0
	for l := 1; l < line; l++ {
		i := bytes.IndexByte(src[start:], '\n')
		if i < 0 {
			return 0, false
		}
		start += i + 1
	}
	end := bytes.IndexByte(src[start:], '\n')
	if end < 0 {
		end = len(src) - start
	}
	if col > end {
		return 0, false
	}
	return start + col, true
}

// taskBoxRenderer renders a checkbox annotateTasks marked (a SourceLines
// render, for the viewer) as an enabled input: the viewer edits the
// source when it is toggled. Every other checkbox keeps GFM's disabled
// rendering.
type taskBoxRenderer struct{}

// taskBoxRendererPriority registers after GFM's checkbox renderer (500):
// goldmark lets the lowest value win.
const taskBoxRendererPriority = 100

func (taskBoxRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(extast.KindTaskCheckBox, func(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		box := node.(*extast.TaskCheckBox)
		_, viewer := box.AttributeString(TaskIndexAttr)
		_, _ = w.WriteString("<input")
		if box.IsChecked {
			_, _ = w.WriteString(` checked=""`)
		}
		if !viewer {
			_, _ = w.WriteString(` disabled=""`)
		}
		_, _ = w.WriteString(` type="checkbox"> `)
		return ast.WalkContinue, nil
	})
}
