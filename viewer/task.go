// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/marcelocantos/vellum/convert"
)

// A task toggle names the exact checkbox the reader clicked: its index
// among the document's task checkboxes, the source line of its marker
// (both rendered onto the list item, see convert.TaskIndexAttr), the state
// the reader saw, and in StampHeader the source stamp the page was
// rendered from. The server checks every one against the file before it
// changes a byte, and answers 409 when any differs: the document changed
// underneath the page, and a reload shows the reader what is there now.

// errTaskMoved wraps every reason a toggle no longer matches the file.
type errTaskMoved struct{ msg string }

func (e *errTaskMoved) Error() string { return e.msg + "; reload to see the document as it is now" }

// toggleTask flips the marker of task index in src after checking that
// it is on line and currently checked (or not) as the reader saw it.
func toggleTask(src []byte, index, line int, checked bool) ([]byte, error) {
	tasks, err := convert.Tasks(src)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(tasks) {
		return nil, &errTaskMoved{fmt.Sprintf("task %d is not in the document (it has %d)", index, len(tasks))}
	}
	t := tasks[index]
	if t.Line != line {
		return nil, &errTaskMoved{fmt.Sprintf("task %d is on line %d now, not line %d", index, t.Line, line)}
	}
	if t.Checked != checked {
		state := "unchecked"
		if t.Checked {
			state = "checked"
		}
		return nil, &errTaskMoved{fmt.Sprintf("the task on line %d is already %s", line, state)}
	}
	out := make([]byte, len(src))
	copy(out, src)
	mark := byte('x')
	if checked {
		mark = ' '
	}
	out[t.Offset+1] = mark
	return out, nil
}

func (s *Server) handleTaskToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	absPath, err := queryAbsPath(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !isMarkdownPath(absPath) {
		http.Error(w, "not a markdown path", http.StatusBadRequest)
		return
	}
	q := r.URL.Query()
	index, errIndex := strconv.Atoi(strings.TrimSpace(q.Get("index")))
	line, errLine := strconv.Atoi(strings.TrimSpace(q.Get("line")))
	was := q.Get("checked")
	if errIndex != nil || errLine != nil || (was != "0" && was != "1") {
		http.Error(w, "missing or invalid index, line or checked (reload the page)", http.StatusBadRequest)
		return
	}
	expected := strings.TrimSpace(r.Header.Get(StampHeader))
	if expected == "" {
		http.Error(w, "missing "+StampHeader+" (reload the page)", http.StatusPreconditionRequired)
		return
	}
	// The stamp comes from the open file before it is read, so the bytes
	// checked below are never older than the stamp compared here. The
	// same stamp is checked again immediately before the write: a change
	// that landed after the read must not be replaced by this page's copy.
	// writeMu keeps a second click that read the old stamp from writing
	// after this one has committed.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	src, info, err := s.readServed(absPath)
	if servedStatus(w, r, err) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if current := sourceStamp(info); current != expected {
		w.Header().Set(StampHeader, current)
		http.Error(w, "the file changed on disk since this page was rendered; reload to see the document as it is now", http.StatusConflict)
		return
	}
	out, err := toggleTask(src, index, line, was == "1")
	if err != nil {
		w.Header().Set(StampHeader, expected)
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	again, err := s.statServed(absPath)
	if servedStatus(w, r, err) {
		return
	}
	if current := sourceStamp(again); current != expected {
		w.Header().Set(StampHeader, current)
		http.Error(w, "the file changed on disk since this page was rendered; reload to see the document as it is now", http.StatusConflict)
		return
	}
	after, err := s.writeServed(absPath, out)
	if servedStatus(w, r, err) {
		return
	}
	stamp := sourceStamp(after)
	w.Header().Set(StampHeader, stamp)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("stamp " + stamp + "\n"))
}
