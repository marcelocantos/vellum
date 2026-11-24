// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func editTestDoc(t *testing.T, content string) (string, *Server, string) {
	t.Helper()
	dir := t.TempDir()
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache"), Roots: []string{dir}}
	ts := startTestServer(t, s)
	return md, s, ts.URL + ChromeEditPath + "?path=" + url.QueryEscape(md)
}

func doEdit(t *testing.T, method, u, stamp, body string, header map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, u, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if stamp != "" {
		req.Header.Set(StampHeader, stamp)
	}
	for k, v := range header {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, string(data)
}

func TestEdit_GetReturnsRawSourceAndStamp(t *testing.T) {
	src := "# Title\r\n\r\n<b>raw</b> & text\r\n"
	md, _, u := editTestDoc(t, src)
	resp, body := doEdit(t, http.MethodGet, u, "", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if body != src {
		t.Fatalf("body %q, want the file byte for byte %q", body, src)
	}
	info, _ := os.Stat(md)
	if got := resp.Header.Get(StampHeader); got != sourceStamp(info) {
		t.Fatalf("stamp %q, want %q", got, sourceStamp(info))
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control %q", cc)
	}
}

func TestEdit_PutWritesWithMatchingStamp(t *testing.T) {
	md, _, u := editTestDoc(t, "# Old\n")
	resp, _ := doEdit(t, http.MethodGet, u, "", "", nil)
	stamp := resp.Header.Get(StampHeader)

	resp, body := doEdit(t, http.MethodPut, u, stamp, "# New\n\nBody\n", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	got, _ := os.ReadFile(md)
	if string(got) != "# New\n\nBody\n" {
		t.Fatalf("file %q", got)
	}
	info, _ := os.Stat(md)
	if resp.Header.Get(StampHeader) != sourceStamp(info) || body != "stamp "+sourceStamp(info)+"\n" {
		t.Fatalf("stamp header %q body %q, want %q", resp.Header.Get(StampHeader), body, sourceStamp(info))
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v, want 0640 preserved", info.Mode().Perm())
	}
}

func TestEdit_PutConflictsWhenDiskMoved(t *testing.T) {
	md, _, u := editTestDoc(t, "# One\n")
	resp, _ := doEdit(t, http.MethodGet, u, "", "", nil)
	stale := resp.Header.Get(StampHeader)
	if err := os.WriteFile(md, []byte("# Someone else's edit\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	resp, body := doEdit(t, http.MethodPut, u, stale, "# Mine\n", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d: %s, want 409", resp.StatusCode, body)
	}
	got, _ := os.ReadFile(md)
	if string(got) != "# Someone else's edit\n" {
		t.Fatalf("conflicting PUT wrote the file: %q", got)
	}
	current := resp.Header.Get(StampHeader)
	info, _ := os.Stat(md)
	if current != sourceStamp(info) {
		t.Fatalf("409 stamp %q, want current %q", current, sourceStamp(info))
	}

	// Overwrite: resend naming the stamp the conflict reported.
	resp, body = doEdit(t, http.MethodPut, u, current, "# Mine\n", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overwrite status %d: %s", resp.StatusCode, body)
	}
	got, _ = os.ReadFile(md)
	if string(got) != "# Mine\n" {
		t.Fatalf("file %q after overwrite", got)
	}
}

func TestEdit_PutRequiresStamp(t *testing.T) {
	md, _, u := editTestDoc(t, "# Keep\n")
	resp, body := doEdit(t, http.MethodPut, u, "", "# Lost\n", nil)
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("status %d: %s, want 428", resp.StatusCode, body)
	}
	if got, _ := os.ReadFile(md); string(got) != "# Keep\n" {
		t.Fatalf("file %q", got)
	}
}

func TestEdit_RejectsForeignRequests(t *testing.T) {
	md, _, u := editTestDoc(t, "# Safe\n")
	resp, _ := doEdit(t, http.MethodGet, u, "", "", nil)
	stamp := resp.Header.Get(StampHeader)
	cases := map[string]map[string]string{
		"cross-origin":    {"Origin": "https://evil.example"},
		"cross-site":      {"Sec-Fetch-Site": "cross-site"},
		"same-site":       {"Sec-Fetch-Site": "same-site"},
		"rebinding host":  {"Host": "evil.example:18742"},
		"lan host":        {"Host": "192.168.1.10:18742"},
		"origin mismatch": {"Origin": "http://127.0.0.1:1"},
	}
	for name, hdr := range cases {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			resp, body := doEdit(t, method, u, stamp, "# Pwned\n", hdr)
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s %s: status %d (%s), want 403", name, method, resp.StatusCode, body)
			}
		}
	}
	if got, _ := os.ReadFile(md); string(got) != "# Safe\n" {
		t.Fatalf("file written by a foreign request: %q", got)
	}
}

func TestEdit_AllowsLocalhostAndSameOrigin(t *testing.T) {
	_, _, u := editTestDoc(t, "# Ok\n")
	parsed, _ := url.Parse(u)
	_, port, _ := strings.Cut(parsed.Host, ":")
	for name, hdr := range map[string]map[string]string{
		"same-origin": {"Origin": "http://" + parsed.Host, "Sec-Fetch-Site": "same-origin"},
		"localhost":   {"Host": "localhost:" + port},
		"ipv6":        {"Host": "[::1]:" + port},
	} {
		resp, body := doEdit(t, http.MethodGet, u, "", "", hdr)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d: %s", name, resp.StatusCode, body)
		}
	}
}

func TestEdit_RejectsNonMarkdownAndMissing(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := startTestServer(t, &Server{CacheDir: filepath.Join(dir, "cache"), Roots: []string{dir}})
	for path, want := range map[string]int{
		txt:                           http.StatusBadRequest,
		filepath.Join(dir, "gone.md"): http.StatusNotFound,
		"relative.md":                 http.StatusBadRequest,
	} {
		resp, body := doEdit(t, http.MethodGet, ts.URL+ChromeEditPath+"?path="+url.QueryEscape(path), "", "", nil)
		if resp.StatusCode != want {
			t.Errorf("%s: status %d (%s), want %d", path, resp.StatusCode, body, want)
		}
	}
	resp, _ := doEdit(t, http.MethodPost, ts.URL+ChromeEditPath+"?path="+url.QueryEscape(txt), "", "", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status %d, want 405", resp.StatusCode)
	}
}

func TestEditorAssets_ServedAsModules(t *testing.T) {
	ts := startTestServer(t, &Server{CacheDir: t.TempDir()})
	resp, body := doEdit(t, http.MethodGet, ts.URL+editorEntryURL(), "", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("entry status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("entry Content-Type %q", ct)
	}
	if !strings.Contains(body, "createEditor") {
		t.Fatal("entry does not export createEditor")
	}
	// Every chunk the entry imports is served from the embedded tree, so
	// the editor works offline.
	n := 0
	for _, part := range strings.Split(body, `"./chunks/`)[1:] {
		name, _, ok := strings.Cut(part, `"`)
		if !ok {
			continue
		}
		n++
		r, _ := doEdit(t, http.MethodGet, ts.URL+ChromeEditorPrefix+"chunks/"+name, "", "", nil)
		if r.StatusCode != http.StatusOK || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/javascript") {
			t.Errorf("chunk %s: status %d type %q", name, r.StatusCode, r.Header.Get("Content-Type"))
		}
	}
	if n == 0 {
		t.Fatal("entry imports no chunks; bundle layout changed")
	}
	for _, bad := range []string{"", "missing.js", "../edit.go"} {
		r, _ := doEdit(t, http.MethodGet, ts.URL+ChromeEditorPrefix+bad, "", "", nil)
		if r.StatusCode != http.StatusNotFound {
			t.Errorf("%q: status %d, want 404", bad, r.StatusCode)
		}
	}
	if _, err := fs.Stat(editorFS, "THIRD_PARTY_LICENSES.txt"); err != nil {
		t.Fatalf("bundle ships without its licence notices: %v", err)
	}
}

func TestInjectChrome_EditButtonAndModal(t *testing.T) {
	doc := `<html><head></head><body><h1>x</h1></body></html>`
	out := injectChrome(doc, &chromePage{SourcePath: "/docs/note.md"})
	for _, want := range []string{
		`data-vellum="edit"`,
		`id="vellum-editor"`,
		`data-editor="` + editorEntryURL() + `"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("chrome missing %s", want)
		}
	}
	waiting := injectChrome(doc, &chromePage{SourcePath: "/docs/new.md", Waiting: true})
	// Markup only: the inlined chrome.js mentions both selectors.
	waiting, _, _ = strings.Cut(waiting, chromeJS)
	if strings.Contains(waiting, `data-vellum="edit"`) || strings.Contains(waiting, "data-editor=") {
		t.Error("waiting page offers an editor for a file that does not exist")
	}
	for _, action := range []string{"pdf", "clipboard", "reveal"} {
		if strings.Contains(waiting, `data-vellum="`+action+`"`) {
			t.Errorf("waiting page offers %s for a file that does not exist", action)
		}
	}
	if !strings.Contains(waiting, `data-vellum="theme"`) || !strings.Contains(waiting, `class="vellum-btn vellum-home"`) {
		t.Error("waiting page lost the theme button or the home link")
	}
}

// TestEdit_StampWithoutHeaders: a page whose headers a proxy drops (T3's
// plugin gateway) reads and writes with the stamp in the body and query.
func TestEdit_StampWithoutHeaders(t *testing.T) {
	src := "# One\r\n\r\nstamp not a stamp line\r\n"
	md, _, u := editTestDoc(t, src)

	resp, body := doEdit(t, http.MethodGet, u+"&"+StampLineParam+"=1", "", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	info, _ := os.Stat(md)
	first, rest, ok := strings.Cut(body, "\n")
	if !ok || first != "stamp "+sourceStamp(info) || rest != src {
		t.Fatalf("body %q, want a stamp line then the file byte for byte", body)
	}
	stamp := strings.TrimPrefix(first, "stamp ")

	if err := os.WriteFile(md, []byte("# Someone else's edit\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	resp, body = doEdit(t, http.MethodPut, u+"&"+StampParam+"="+url.QueryEscape(stamp), "", "# Mine\n", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d: %s, want 409", resp.StatusCode, body)
	}
	info, _ = os.Stat(md)
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	current := strings.TrimPrefix(lines[len(lines)-1], "stamp ")
	if len(lines) < 2 || current != sourceStamp(info) {
		t.Fatalf("409 body %q, want a message and a last line naming the stamp on disk %q", body, sourceStamp(info))
	}

	resp, body = doEdit(t, http.MethodPut, u+"&"+StampParam+"="+url.QueryEscape(current), "", "# Mine\n", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("overwrite status %d: %s", resp.StatusCode, body)
	}
	if got, _ := os.ReadFile(md); string(got) != "# Mine\n" {
		t.Fatalf("file %q after overwrite", got)
	}

	resp, body = doEdit(t, http.MethodPut, u, "", "# No stamp\n", nil)
	if resp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf("status %d: %s, want 428 with no stamp at all", resp.StatusCode, body)
	}
}
