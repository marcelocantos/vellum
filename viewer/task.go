// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var (
	liStartCheckboxRe = regexp.MustCompile(`(?is)<li(\b[^>]*)>(\s*)<input(\b[^>]*)>`)
	liClassAttrRe     = regexp.MustCompile(`(?i)(\bclass\s*=\s*)(["'])([^"']*)(["'])`)
	disabledAttrRe    = regexp.MustCompile(`(?i)\sdisabled(?:\s*=\s*(?:""|'')|\s*=\s*disabled)?`)
	taskMarkerRe      = regexp.MustCompile(`(?m)^(\s*[-*+]\s+\[)([ xX])(\])`)
)

func isCheckboxAttrs(attrs string) bool {
	lower := strings.ToLower(attrs)
	return strings.Contains(lower, `type="checkbox"`) ||
		strings.Contains(lower, `type='checkbox'`) ||
		strings.Contains(lower, "type=checkbox")
}

func ensureClassAttr(attrs, class string) string {
	if liClassAttrRe.MatchString(attrs) {
		return liClassAttrRe.ReplaceAllStringFunc(attrs, func(m string) string {
			sub := liClassAttrRe.FindStringSubmatch(m)
			if len(sub) != 5 || containsWord(sub[3], class) {
				return m
			}
			existing := sub[3]
			if existing != "" {
				existing += " "
			}
			return sub[1] + sub[2] + existing + class + sub[4]
		})
	}
	return attrs + ` class="` + class + `"`
}

func annotateTaskCheckboxes(html string) string {
	n := 0
	return liStartCheckboxRe.ReplaceAllStringFunc(html, func(m string) string {
		sub := liStartCheckboxRe.FindStringSubmatch(m)
		if len(sub) != 4 || !isCheckboxAttrs(sub[3]) {
			return m
		}
		liAttrs, ws, inAttrs := sub[1], sub[2], sub[3]
		if !strings.Contains(strings.ToLower(liAttrs), "data-task-index=") {
			liAttrs += ` data-task-index="` + strconv.Itoa(n) + `"`
		}
		liAttrs = ensureClassAttr(liAttrs, "task-list-item")
		inAttrs = disabledAttrRe.ReplaceAllString(inAttrs, "")
		if !strings.Contains(strings.ToLower(inAttrs), "data-task-index=") {
			inAttrs += ` data-task-index="` + strconv.Itoa(n) + `"`
		}
		n++
		return "<li" + liAttrs + ">" + ws + "<input" + inAttrs + ">"
	})
}

func toggleTaskMarker(src []byte, index int) ([]byte, error) {
	matches := taskMarkerRe.FindAllSubmatchIndex(src, -1)
	if index < 0 || index >= len(matches) {
		return nil, fmt.Errorf("task index %d out of range (have %d)", index, len(matches))
	}
	loc := matches[index]
	mark := src[loc[4]:loc[5]]
	next := byte('x')
	if mark[0] != ' ' {
		next = ' '
	}
	out := make([]byte, len(src))
	copy(out, src)
	out[loc[4]] = next
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
	indexStr := strings.TrimSpace(r.URL.Query().Get("index"))
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		http.Error(w, "missing or invalid index", http.StatusBadRequest)
		return
	}
	src, _, err := s.readServed(absPath)
	if servedStatus(w, r, err) {
		return
	}
	out, err := toggleTaskMarker(src, index)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if servedStatus(w, r, s.writeServed(absPath, out)) {
		return
	}
	info, err := s.statServed(absPath)
	if servedStatus(w, r, err) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("stamp " + sourceStamp(info) + "\n"))
}
