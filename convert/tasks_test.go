// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Tasks and a SourceLines render agree: the same checkboxes, numbered in
// the same order, with the line of each marker; code, diagrams and math
// hold none.
func TestTasks_MatchTheRenderedCheckboxes(t *testing.T) {
	src := "---\ntitle: T\n---\n```\n- [ ] in code\n```\n\n~~~\n- [ ] tilde\n~~~\n\n    - [ ] indented code\n\n" +
		"> - [ ] quoted\n\n- [ ] loose a\n\n- [x] loose b\n\n```mermaid\ngraph TD\n%% - [ ] no\n```\n\n$$\n- [ ] no\n$$\n\n" +
		"1. [X] numbered $x$\n   - [ ] nested\n"
	tasks, err := Tasks([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := []Task{{0, 14, 0, false}, {1, 16, 0, false}, {2, 18, 0, true}, {3, 29, 0, true}, {4, 30, 0, false}}
	if len(tasks) != len(want) {
		t.Fatalf("tasks %+v, want %d", tasks, len(want))
	}
	lines := strings.SplitAfter(src, "\n")
	for i, w := range want {
		got := tasks[i]
		if got.Index != w.Index || got.Line != w.Line || got.Checked != w.Checked {
			t.Errorf("task %d: %+v, want index %d line %d checked %v", i, got, w.Index, w.Line, w.Checked)
		}
		if src[got.Offset] != '[' || src[got.Offset+2] != ']' || !strings.Contains(lines[got.Line-1], src[got.Offset:got.Offset+3]) {
			t.Errorf("task %d offset %d is not its marker on line %d", i, got.Offset, got.Line)
		}
	}
	html, _, err := Render(context.Background(), []byte(src), &Options{Defer: true, SourceLines: true, Sanitize: true})
	if err != nil {
		t.Fatal(err)
	}
	html = html[strings.Index(html, "<body"):] // the stylesheet mentions checkboxes too
	items := regexp.MustCompile(`data-task-index="(\d+)" data-task-line="(\d+)" class="task-list-item">\s*(?:<p[^>]*>)?<input([^>]*)>`).FindAllStringSubmatch(html, -1)
	if len(items) != len(want) || strings.Count(html, `type="checkbox"`) != len(want) {
		t.Fatalf("rendered %d annotated items, %d checkboxes; want %d:\n%s", len(items), strings.Count(html, `type="checkbox"`), len(want), html)
	}
	for i, m := range items {
		if m[1] != strconv.Itoa(want[i].Index) || m[2] != strconv.Itoa(want[i].Line) || strings.Contains(m[3], "disabled") {
			t.Errorf("rendered item %d: %q", i, m[0])
		}
	}
	// Without SourceLines (every output but the viewer) checkboxes stay
	// GFM's read-only ones and carry no viewer attributes.
	plain, _, err := Render(context.Background(), []byte(src), &Options{Defer: true})
	if err != nil {
		t.Fatal(err)
	}
	plain = plain[strings.Index(plain, "<body"):]
	if strings.Contains(plain, TaskIndexAttr) || strings.Count(plain, `disabled="" type="checkbox"`) != len(want) {
		t.Fatalf("plain render changed:\n%s", plain)
	}
}
