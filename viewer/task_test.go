// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/convert"
)

func TestAnnotateTaskCheckboxes_IndexesAndEnables(t *testing.T) {
	in := `<ul>
<li class="task-list-item"><input disabled="" type="checkbox"> one</li>
<li class="task-list-item"><input disabled="" type="checkbox" checked=""> two</li>
</ul>`
	out := annotateTaskCheckboxes(in)
	if !strings.Contains(out, `data-task-index="0"`) || !strings.Contains(out, `data-task-index="1"`) {
		t.Fatalf("missing indexes:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "disabled") {
		t.Fatalf("checkbox still disabled:\n%s", out)
	}
}

func TestAnnotateTaskCheckboxes_GoldmarkShape(t *testing.T) {
	in := `<ul>
<li><input disabled="" type="checkbox"> a</li>
<li><input checked="" disabled="" type="checkbox"> b</li>
</ul>`
	out := annotateTaskCheckboxes(in)
	if !strings.Contains(out, `data-task-index="0"`) || !strings.Contains(out, `data-task-index="1"`) {
		t.Fatalf("missing indexes:\n%s", out)
	}
	if !strings.Contains(out, `class="task-list-item"`) {
		t.Fatalf("missing task-list-item class:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "disabled") {
		t.Fatalf("checkbox still disabled:\n%s", out)
	}
}

func TestToggleTaskMarker_FlipsNth(t *testing.T) {
	src := []byte("- [ ] a\n- [x] b\n- [ ] c\n")
	out, err := toggleTaskMarker(src, 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "- [ ] a\n- [ ] b\n- [ ] c\n" {
		t.Fatalf("got %q", out)
	}
	out, err = toggleTaskMarker(out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "- [x] a\n- [ ] b\n- [ ] c\n" {
		t.Fatalf("got %q", out)
	}
	if _, err := toggleTaskMarker(src, 3); err == nil {
		t.Fatal("expected out of range")
	}
}

func TestToggleTaskMarker_StarPlusAndIndent(t *testing.T) {
	src := []byte("  * [X] nested\n+ [ ] plus\n")
	out, err := toggleTaskMarker(src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "  * [ ] nested\n+ [ ] plus\n" {
		t.Fatalf("got %q", out)
	}
	out, err = toggleTaskMarker(out, 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "  * [ ] nested\n+ [x] plus\n" {
		t.Fatalf("got %q", out)
	}
}

func TestAction_TaskTogglePOST(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "tasks.md")
	if err := os.WriteFile(md, []byte("# T\n\n- [ ] one\n- [x] two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var wrote []byte
	s := &Server{
		CacheDir: filepath.Join(dir, "cache"),
		WriteFile: func(path string, data []byte) error {
			if path != md {
				t.Fatalf("write path %q", path)
			}
			wrote = append([]byte(nil), data...)
			return os.WriteFile(path, data, 0o644)
		},
	}
	ts := startTestServer(t, s)
	u := ts.URL + ChromeTaskTogglePath + "?path=" + url.QueryEscape(md) + "&index=0"
	resp, err := http.Post(u, "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(wrote), "- [x] one") {
		t.Fatalf("write body:\n%s", wrote)
	}
	got, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "- [x] one") || !strings.Contains(string(got), "- [x] two") {
		t.Fatalf("file:\n%s", got)
	}
}

func TestAction_TaskToggleRejectsGET(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	resp, err := http.Get(ts.URL + ChromeTaskTogglePath + "?path=/tmp/x.md&index=0")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", resp.StatusCode)
	}
}

func TestAction_TaskToggleRejectsNonMarkdown(t *testing.T) {
	s := &Server{CacheDir: t.TempDir()}
	ts := startTestServer(t, s)
	resp, err := http.Post(ts.URL+ChromeTaskTogglePath+"?path=/tmp/x.txt&index=0", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
}

func TestAction_TaskToggleOutOfRange(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "tasks.md")
	if err := os.WriteFile(md, []byte("- [ ] only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache")}
	ts := startTestServer(t, s)
	u := ts.URL + ChromeTaskTogglePath + "?path=" + url.QueryEscape(md) + "&index=4"
	resp, err := http.Post(u, "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, want 409: %s", resp.StatusCode, body)
	}
}

func TestAction_TaskToggleWriteError(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "tasks.md")
	if err := os.WriteFile(md, []byte("- [ ] a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		CacheDir: filepath.Join(dir, "cache"),
		WriteFile: func(path string, data []byte) error {
			return os.ErrPermission
		},
	}
	ts := startTestServer(t, s)
	u := ts.URL + ChromeTaskTogglePath + "?path=" + url.QueryEscape(md) + "&index=0"
	resp, err := http.Post(u, "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", resp.StatusCode)
	}
	got, _ := os.ReadFile(md)
	if string(got) != "- [ ] a\n" {
		t.Fatalf("file corrupted: %q", got)
	}
}

func TestChromeJS_TaskToggleIsCheckboxOnly(t *testing.T) {
	if !strings.Contains(chromeJS, `t.type !== "checkbox"`) {
		t.Fatal("chrome.js missing checkbox change guard")
	}
	if strings.Contains(chromeJS, "input.click()") {
		t.Fatal("chrome.js forwards list-item clicks onto the task checkbox")
	}
}

func TestInjectChrome_TaskIndexesOnServe(t *testing.T) {
	in := `<!DOCTYPE html><html><head></head><body>
<ul>
<li><input disabled="" type="checkbox"> do it</li>
</ul>
</body></html>`
	out := injectChrome(in, "/docs/note.md")
	if !strings.Contains(out, `data-task-index="0"`) {
		t.Fatalf("missing task index:\n%s", out)
	}
	if !strings.Contains(out, `id="vellum-consent"`) || !strings.Contains(out, `role="dialog"`) {
		t.Fatalf("missing consent dialog:\n%s", out)
	}
	if !strings.Contains(out, "task-toggle") {
		t.Fatalf("missing task-toggle script:\n%s", out)
	}
	start := strings.Index(out, `class="vellum-article"`)
	end := strings.Index(out, `id="vellum-lightbox"`)
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("article bounds:\n%s", out)
	}
	article := out[start:end]
	if !strings.Contains(article, `type="checkbox"`) {
		t.Fatalf("checkbox missing from article:\n%s", article)
	}
	if strings.Contains(strings.ToLower(article), "disabled") {
		t.Fatalf("served checkbox still disabled:\n%s", article)
	}
}

func TestInjectChrome_ConvertGFMTaskList(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "tasks.md")
	if err := os.WriteFile(md, []byte("# Tasks\n\n- [ ] a\n- [x] b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	html, _, err := convert.RenderFile(context.Background(), md, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `type="checkbox"`) {
		t.Fatalf("convert HTML missing checkbox:\n%s", html)
	}
	out := injectChrome(html, md)
	if !strings.Contains(out, `data-task-index="0"`) || !strings.Contains(out, `data-task-index="1"`) {
		t.Fatalf("indexes not injected into convert HTML:\n%s", out)
	}
}
