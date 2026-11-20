// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// servedTask is one task checkbox as the served page shows it.
type servedTask struct {
	index, line int
	checked     bool
}

var (
	pageStampRe = regexp.MustCompile(`data-stamp="([^"]*)"`)
	pageTaskRe  = regexp.MustCompile(`<li[^>]*\bdata-task-index="(\d+)"[^>]*\bdata-task-line="(\d+)"[^>]*>\s*(?:<p[^>]*>)?<input([^>]*)>`)
)

// renderTasks GETs md's page and returns its stamp and task checkboxes,
// as chrome.js would read them.
func renderTasks(t *testing.T, ts, md string) (string, []servedTask) {
	t.Helper()
	status, _, page := getText(t, ts+pathURL(md))
	if status != http.StatusOK {
		t.Fatalf("page %d: %s", status, page)
	}
	m := pageStampRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("page has no data-stamp:\n%s", page)
	}
	var tasks []servedTask
	for _, tm := range pageTaskRe.FindAllStringSubmatch(page, -1) {
		i, _ := strconv.Atoi(tm[1])
		l, _ := strconv.Atoi(tm[2])
		if strings.Contains(tm[3], "disabled") {
			t.Fatalf("served task %d is disabled: %s", i, tm[0])
		}
		tasks = append(tasks, servedTask{index: i, line: l, checked: strings.Contains(tm[3], "checked")})
	}
	if got := strings.Count(articleOf(page), `type="checkbox"`); got != len(tasks) {
		t.Fatalf("page has %d checkboxes but %d annotated tasks:\n%s", got, len(tasks), articleOf(page))
	}
	return m[1], tasks
}

// clickTask posts the toggle chrome.js sends for task (the reader saw
// task.checked) from a page rendered at stamp.
func clickTask(t *testing.T, ts, md, stamp string, task servedTask) (int, string) {
	t.Helper()
	was := "0"
	if task.checked {
		was = "1"
	}
	u := ts + ChromeTaskTogglePath + "?path=" + url.QueryEscape(md) +
		"&index=" + strconv.Itoa(task.index) + "&line=" + strconv.Itoa(task.line) + "&checked=" + was
	req, _ := http.NewRequest(http.MethodPost, u, nil)
	if stamp != "" {
		req.Header.Set(StampHeader, stamp)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func taskServer(t *testing.T, src string) (ts, md string, s *Server) {
	t.Helper()
	dir := t.TempDir()
	md = filepath.Join(dir, "tasks.md")
	if err := os.WriteFile(md, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s = &Server{CacheDir: filepath.Join(dir, "cache"), Roots: []string{dir}}
	return startTestServer(t, s).URL, md, s
}

// bumpMtime moves p's mtime a second on, so an edit of the same size
// still changes the stamp whatever the filesystem's clock resolution.
func bumpMtime(t *testing.T, p string) {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	later := info.ModTime().Add(time.Second)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
}

// Each document is clicked at one task the page shows; the file must
// change in exactly that task's marker and nowhere else.
func TestTaskToggle_ChangesExactlyTheClickedTask(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		click int    // index of the served task to click
		want  string // the file afterwards
	}{
		{"report: marker inside a fenced block comes first",
			"```\n- [ ] example in code\n```\n\n- [ ] real task\n", 0,
			"```\n- [ ] example in code\n```\n\n- [x] real task\n"},
		{"report: quoted task before a plain one",
			"> - [ ] quoted task\n\n- [ ] real task\n", 0,
			"> - [x] quoted task\n\n- [ ] real task\n"},
		{"the plain task after a quoted one",
			"> - [ ] quoted task\n\n- [ ] real task\n", 1,
			"> - [ ] quoted task\n\n- [x] real task\n"},
		{"tilde fence and indented code",
			"~~~\n- [ ] fenced\n~~~\n\n    - [ ] indented code\n\n- [ ] real\n", 0,
			"~~~\n- [ ] fenced\n~~~\n\n    - [ ] indented code\n\n- [x] real\n"},
		{"loose list before a tight one",
			"- [ ] loose one\n\n- [ ] loose two\n\nText.\n\n* [ ] tight\n", 2,
			"- [ ] loose one\n\n- [ ] loose two\n\nText.\n\n* [x] tight\n"},
		{"nested and numbered",
			"1. [ ] outer\n   - [X] inner\n2. [ ] second\n", 1,
			"1. [ ] outer\n   - [ ] inner\n2. [ ] second\n"},
		{"mermaid and display math hold task-like lines",
			"```mermaid\ngraph TD\n%% - [ ] not a task\n```\n\n$$\n- [ ] not a task\n$$\n\n+ [ ] task $x$\n", 0,
			"```mermaid\ngraph TD\n%% - [ ] not a task\n```\n\n$$\n- [ ] not a task\n$$\n\n+ [x] task $x$\n"},
		{"html comment and front matter",
			"---\ntitle: T\n---\n<!--\n- [ ] commented\n-->\n\n- [ ] real\n", 0,
			"---\ntitle: T\n---\n<!--\n- [ ] commented\n-->\n\n- [x] real\n"},
		{"same text twice",
			"- [ ] same\n- [ ] same\n", 1,
			"- [ ] same\n- [x] same\n"},
		{"crlf line endings",
			"- [ ] a\r\n- [x] b\r\n", 1,
			"- [ ] a\r\n- [ ] b\r\n"},
		{"tab-indented nested task",
			"- [ ] outer\n\t- [ ] inner\n- [ ] other\n", 1,
			"- [ ] outer\n\t- [x] inner\n- [ ] other\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts, md, _ := taskServer(t, c.src)
			stamp, tasks := renderTasks(t, ts, md)
			if c.click >= len(tasks) {
				t.Fatalf("page shows %d tasks, want at least %d", len(tasks), c.click+1)
			}
			code, body := clickTask(t, ts, md, stamp, tasks[c.click])
			if code != http.StatusOK || !strings.HasPrefix(body, "stamp ") {
				t.Fatalf("status %d: %s", code, body)
			}
			got, _ := os.ReadFile(md)
			if string(got) != c.want {
				t.Fatalf("file after clicking task %d:\n%q\nwant\n%q", c.click, got, c.want)
			}
			// The page's next click (stamp adopted from the response)
			// toggles it back.
			tasks[c.click].checked = !tasks[c.click].checked
			code, body = clickTask(t, ts, md, strings.TrimSpace(strings.TrimPrefix(body, "stamp ")), tasks[c.click])
			if code != http.StatusOK {
				t.Fatalf("toggle back: %d %s", code, body)
			}
			// A tick is always written as a lower-case x.
			back := strings.ReplaceAll(c.src, "[X]", "[x]")
			if got, _ := os.ReadFile(md); string(got) != back {
				t.Fatalf("toggle back:\n%q\nwant\n%q", got, back)
			}
		})
	}
}

// The document changes between render and click, or the click names a
// task that is not there: every such toggle is refused and the file
// keeps whatever it had.
func TestTaskToggle_RefusesWhenTheDocumentChanged(t *testing.T) {
	const src = "- [ ] alpha\n- [ ] beta\n"
	first := func(ts []servedTask) servedTask { return ts[0] }
	for _, c := range []struct {
		name  string
		edit  string // file contents written after the render
		task  func(tasks []servedTask) servedTask
		stamp func(stamp string) string
		code  int
	}{
		{"task inserted above", "- [ ] new\n- [ ] alpha\n- [ ] beta\n", first, nil, http.StatusConflict},
		{"same length edit elsewhere", "- [ ] alpha\n- [ ] BETA\n", first, nil, http.StatusConflict},
		{"ticked elsewhere", "- [x] alpha\n- [ ] beta\n", first, nil, http.StatusConflict},
		// The stamp is current but what the click names is not there:
		// a forged or stale index, line or state.
		{"wrong line", "", func(ts []servedTask) servedTask { t := ts[0]; t.line = 2; return t }, nil, http.StatusConflict},
		{"wrong state", "", func(ts []servedTask) servedTask { t := ts[0]; t.checked = true; return t }, nil, http.StatusConflict},
		{"index out of range", "", func(ts []servedTask) servedTask { t := ts[1]; t.index = 7; return t }, nil, http.StatusConflict},
		{"no stamp", "", first, func(string) string { return "" }, http.StatusPreconditionRequired},
	} {
		t.Run(c.name, func(t *testing.T) {
			ts, md, _ := taskServer(t, src)
			stamp, tasks := renderTasks(t, ts, md)
			want := src
			if c.edit != "" {
				if err := os.WriteFile(md, []byte(c.edit), 0o644); err != nil {
					t.Fatal(err)
				}
				bumpMtime(t, md)
				want = c.edit
			}
			if c.stamp != nil {
				stamp = c.stamp(stamp)
			}
			code, body := clickTask(t, ts, md, stamp, c.task(tasks))
			if code != c.code {
				t.Fatalf("status %d, want %d: %s", code, c.code, body)
			}
			if got, _ := os.ReadFile(md); string(got) != want {
				t.Fatalf("file changed by a refused toggle:\n%q\nwant\n%q", got, want)
			}
		})
	}
}

// Two clicks sent with the same page stamp: the first lands, the second
// is refused rather than applied to a file it did not see (chrome.js
// sends them one at a time, adopting each response's stamp).
func TestTaskToggle_SecondClickOnTheSameStampIsRefused(t *testing.T) {
	ts, md, _ := taskServer(t, "- [ ] one\n- [ ] two\n")
	stamp, tasks := renderTasks(t, ts, md)
	if code, body := clickTask(t, ts, md, stamp, tasks[0]); code != http.StatusOK {
		t.Fatalf("first: %d %s", code, body)
	}
	if code, _ := clickTask(t, ts, md, stamp, tasks[1]); code != http.StatusConflict {
		t.Fatalf("second on the old stamp: %d, want 409", code)
	}
	if got, _ := os.ReadFile(md); string(got) != "- [x] one\n- [ ] two\n" {
		t.Fatalf("file %q", got)
	}
}

func TestTaskToggle_RejectsGETNonMarkdownAndBadQuery(t *testing.T) {
	ts, md, _ := taskServer(t, "- [ ] a\n")
	resp, err := http.Get(ts + ChromeTaskTogglePath + "?path=" + url.QueryEscape(md) + "&index=0&line=1&checked=0")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d, want 405", resp.StatusCode)
	}
	for _, q := range []string{
		"?path=" + url.QueryEscape(strings.TrimSuffix(md, ".md")+".txt") + "&index=0&line=1&checked=0",
		"?path=" + url.QueryEscape(md) + "&index=0",
		"?path=" + url.QueryEscape(md) + "&index=0&line=1",
		"?path=" + url.QueryEscape(md) + "&index=x&line=1&checked=0",
		"?path=" + url.QueryEscape(md) + "&index=0&line=1&checked=yes",
	} {
		resp, err := http.Post(ts+ChromeTaskTogglePath+q, "text/plain", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", q, resp.StatusCode)
		}
	}
	if got, _ := os.ReadFile(md); string(got) != "- [ ] a\n" {
		t.Fatalf("file changed: %q", got)
	}
}

// The file changes after the toggle has read it and before it writes.
// The write must not replace that newer content with the page's copy.
func TestTaskToggle_RefusesWhenTheFileChangesAfterTheRead(t *testing.T) {
	const src = "- [ ] alpha\n- [ ] beta\n"
	const edited = "- [ ] alpha\n- [ ] beta!\n"
	ts, md, s := taskServer(t, src)
	stamp, tasks := renderTasks(t, ts, md)
	opens := 0
	s.beforeOpen = func(string) {
		opens++
		if opens != 2 {
			return
		}
		if err := os.WriteFile(md, []byte(edited), 0o644); err != nil {
			t.Error(err)
		}
		bumpMtime(t, md)
	}
	code, body := clickTask(t, ts, md, stamp, tasks[0])
	if code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", code, body)
	}
	if got, _ := os.ReadFile(md); string(got) != edited {
		t.Fatalf("toggle wrote over the newer file:\n%q", got)
	}
}

func TestTaskToggle_WriteErrorLeavesFile(t *testing.T) {
	ts, md, s := taskServer(t, "- [ ] a\n")
	stamp, tasks := renderTasks(t, ts, md)
	s.WriteFile = func(string, []byte) error { return os.ErrPermission }
	if code, _ := clickTask(t, ts, md, stamp, tasks[0]); code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", code)
	}
	if got, _ := os.ReadFile(md); string(got) != "- [ ] a\n" {
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

// chrome.js names the task the way the server checks it.
func TestChromeJS_TaskToggleNamesTheTask(t *testing.T) {
	for _, want := range []string{`"&line="`, `"&checked="`, `"X-Vellum-Stamp"`, `getAttribute("data-stamp")`, `getAttribute("data-task-line")`, "toggleChain"} {
		if !strings.Contains(chromeJS, want) {
			t.Errorf("chrome.js lacks %s", want)
		}
	}
}

func TestChromeJS_RestoresTaskFocusOnReload(t *testing.T) {
	if !strings.Contains(chromeJS, "function captureFocus") || !strings.Contains(chromeJS, "function restoreFocus") {
		t.Fatal("chrome.js missing focus persist/restore")
	}
	if !strings.Contains(chromeJS, "pos.focus") {
		t.Fatal("chrome.js does not store focus with scroll position")
	}
	if !strings.Contains(chromeJS, `[data-task-index="`) {
		t.Fatal("chrome.js does not restore the focused task checkbox")
	}
}

func TestChromeJS_TaskToggleDoesNotReload(t *testing.T) {
	start := strings.Index(chromeJS, "function applyTaskToggle")
	if start < 0 {
		t.Fatal("missing applyTaskToggle")
	}
	rest := chromeJS[start+1:]
	endRel := strings.Index(rest, "\n  function ")
	if endRel < 0 {
		t.Fatal("applyTaskToggle bounds")
	}
	body := chromeJS[start : start+1+endRel]
	if strings.Contains(body, "reloadView(") || strings.Contains(body, "location.reload(") {
		t.Fatal("applyTaskToggle still reloads the page")
	}
	if strings.Contains(body, "input.disabled") {
		t.Fatal("applyTaskToggle disables the checkbox and drops keyboard focus")
	}
	if !strings.Contains(body, "adoptStamp") {
		t.Fatal("applyTaskToggle does not adopt the source stamp")
	}
}

func TestInjectChrome_ConsentDialogAndStamp(t *testing.T) {
	in := `<!DOCTYPE html><html><head></head><body><p>x</p></body></html>`
	out := injectChrome(in, &chromePage{SourcePath: "/docs/note.md", Stamp: "12 34"})
	if !strings.Contains(out, `id="vellum-consent"`) || !strings.Contains(out, `role="dialog"`) {
		t.Fatalf("missing consent dialog:\n%s", out)
	}
	if !strings.Contains(out, `data-stamp="12 34"`) {
		t.Fatalf("missing page stamp:\n%s", out)
	}
	if !strings.Contains(out, "task-toggle") {
		t.Fatalf("missing task-toggle script:\n%s", out)
	}
}

// The stamp a toggle or save reports names that write, not whatever is
// on disk a moment later: the page adopts it, and an outside write that
// lands right after must still look like a change.
func TestWriteServed_ReportsItsOwnWrite(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache"), Roots: []string{dir}}
	ours, err := s.writeServed(md, []byte("ours\n"))
	if err != nil {
		t.Fatal(err)
	}
	if ours.Size() != int64(len("ours\n")) {
		t.Fatalf("returned info size %d", ours.Size())
	}
	if now, _ := os.Stat(md); sourceStamp(now) != sourceStamp(ours) {
		t.Fatalf("stamp of our write %q differs from the file %q", sourceStamp(ours), sourceStamp(now))
	}
	if err := os.WriteFile(md, []byte("theirs, longer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bumpMtime(t, md)
	if now, _ := os.Stat(md); sourceStamp(now) == sourceStamp(ours) {
		t.Fatal("an outside write after ours carries our stamp")
	}
}
