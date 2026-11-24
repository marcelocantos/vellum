// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"encoding/json"
	"errors"
	"image/color"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// t3Fixture is a folder the viewer does not serve yet, with a document
// that links to another document, shows two images and links to a file
// that is neither.
type t3Fixture struct {
	s     *Server
	ts    *httptest.Server
	dir   string
	doc   string
	other string
	png   string
	svg   string
}

func newT3Fixture(t *testing.T) *t3Fixture {
	t.Helper()
	// A real folder: macOS temp paths go through a symlink, and the
	// served-roots check compares resolved paths.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "my docs")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &t3Fixture{
		dir:   dir,
		doc:   filepath.Join(dir, "note one.md"),
		other: filepath.Join(dir, "other.md"),
		png:   filepath.Join(dir, "pic.png"),
		svg:   filepath.Join(dir, "drawing.svg"),
	}
	doc := "# Note\n\nSee [the other](other.md#part) and [data](table.csv) and [site](https://example.com/).\n\n" +
		"![pic](pic.png)\n\n![drawing](drawing.svg)\n\n$x^2$\n"
	for path, body := range map[string]string{
		f.doc:                           doc,
		f.other:                         "# Other\n\n## Part\n\nbody\n",
		f.svg:                           `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		filepath.Join(dir, "table.csv"): "a,b\n",
		filepath.Join(dir, "page.html"): "<script>alert(1)</script>",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(f.png, pngBytes(t, 3, 2, color.RGBA{0, 0, 255, 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	f.s = &Server{CacheDir: filepath.Join(base, "cache"), MCP: http.NotFoundHandler()}
	f.ts = httptest.NewServer(f.s.T3Handler())
	t.Cleanup(f.ts.Close)
	return f
}

func (f *t3Fixture) get(t *testing.T, path string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(f.ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

// t3Ask sends frames over a plugin connection and returns each answer.
func (f *t3Fixture) ask(t *testing.T, frames ...string) []map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(f.ts.URL, "http")+T3PluginPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var out []map[string]any
	for _, frame := range frames {
		if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
			t.Fatal(err)
		}
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("no answer to %s: %v", frame, err)
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("answer %q: %v", data, err)
		}
		out = append(out, msg)
	}
	return out
}

func linkRequest(id, path string) string {
	b, _ := json.Marshal(map[string]string{"type": "surface", "requestId": id, "scope": "link", "linkId": t3LinkID, "url": path})
	return string(b)
}

// gatewaySafe is T3's pluginRelativePath: the paths its gateway relays.
func gatewaySafe(path string) bool {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || regexp.MustCompile(`[\\\x00-\x20?#]`).MatchString(path) {
		return false
	}
	decoded, err := url.PathUnescape(path)
	if err != nil || regexp.MustCompile(`[\\%\x00-\x20]`).MatchString(decoded) {
		return false
	}
	for _, part := range strings.Split(decoded, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func TestT3_ProbeDeclaresMarkdownLinks(t *testing.T) {
	f := newT3Fixture(t)
	resp, body := f.get(t, T3PluginPath)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("probe: status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var probe t3Probe
	if err := json.Unmarshal([]byte(body), &probe); err != nil {
		t.Fatal(err)
	}
	if probe.Protocol != 1 || probe.ID != T3PluginID || probe.Name != T3PluginName {
		t.Fatalf("probe %+v", probe)
	}
	if len(probe.Scopes) != 1 || probe.Scopes[0] != "links" {
		t.Fatalf("scopes %v, want links only", probe.Scopes)
	}
	if len(probe.Links) != 1 || probe.Links[0].Glob != "/**/*.{md,markdown}" || probe.Links[0].Click != "activate" {
		t.Fatalf("links %+v", probe.Links)
	}
	// hover must be on the wire as false: absent means "show on hover".
	if !strings.Contains(body, `"hover":false`) {
		t.Fatalf("probe does not turn the hover card off: %s", body)
	}
}

// TestT3_LinkRequestOpensTheDocument is the whole path T3 takes: a link
// request over the socket, then the card page and what it loads.
func TestT3_LinkRequestOpensTheDocument(t *testing.T) {
	f := newT3Fixture(t)
	token := t3Token(f.doc)
	if resp, _ := f.get(t, t3DocPrefix+token); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("before any link request: status %d, want 403", resp.StatusCode)
	}

	answers := f.ask(t, linkRequest("r1", f.doc))
	web, _ := answers[0]["web"].(map[string]any)
	if answers[0]["type"] != "result" || answers[0]["requestId"] != "r1" || answers[0]["ok"] != true || web == nil {
		t.Fatalf("answer %v", answers[0])
	}
	path, _ := web["path"].(string)
	if path != t3DocPrefix+token || !gatewaySafe(path) {
		t.Fatalf("card path %q: want %q, and one T3's gateway relays", path, t3DocPrefix+token)
	}
	if h, _ := web["height"].(float64); h < 80 || h > 800 {
		t.Fatalf("card height %v: T3 allows 80 to 800", web["height"])
	}

	resp, page := f.get(t, path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("card page: status %d: %s", resp.StatusCode, page)
	}
	for _, want := range []string{
		`data-embedded`,
		`data-api="../_vellum/"`,
		`data-docs="../d/"`,
		`data-editor="../_vellum/editor/`,
		`href="../_vellum/favicon.svg"`,
		`href="../_vellum/katex/`,
		`href="../d/` + t3Token(f.other) + `#part"`,
		`src="../f/` + t3Token(f.png) + `"`,
		`href="https://example.com/"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("card page lacks %s", want)
		}
	}
	for _, not := range []string{
		`<base href`, `class="vellum-btn vellum-home"`, `data-vellum="clipboard"`, `data-vellum="pdf"`, `data-vellum="reveal"`,
		`href="/`, `src="/`, `table.csv"`, `href="other.md`, `src="pic.png`,
	} {
		if strings.Contains(page, not) {
			t.Errorf("card page has %s", not)
		}
	}
	// The link to a file that is not Markdown has no target.
	if !strings.Contains(page, `<a data-vellum-local="">data</a>`) {
		t.Errorf("the link to table.csv still has a target, or lost its text")
	}

	// The page restates its script rule, with the nonce its scripts carry.
	meta := regexp.MustCompile(`<head[^>]*><meta http-equiv="Content-Security-Policy" content="script-src 'nonce-([^']+)' 'strict-dynamic'; base-uri 'none'">`).FindStringSubmatch(page)
	if meta == nil {
		t.Fatalf("no script policy first in the head")
	}
	// A gateway that enforces a plugin's policy gets the same rule.
	if got, want := resp.Header.Get(t3PolicyHeader), "script-src 'nonce-"+meta[1]+"' 'strict-dynamic'; base-uri 'none'"; got != want {
		t.Errorf("%s = %q, want %q", t3PolicyHeader, got, want)
	}
	scripts := regexp.MustCompile(`<script[^>]*>`).FindAllString(page, -1)
	if len(scripts) == 0 {
		t.Fatal("no scripts in the page")
	}
	for _, tag := range scripts {
		if !strings.Contains(tag, `nonce="`+meta[1]+`"`) {
			t.Errorf("script %s does not carry the policy's nonce", tag)
		}
	}

	resp, body := f.get(t, t3FilePrefix+t3Token(f.png))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" || len(body) == 0 {
		t.Errorf("image: status %d, type %q, %d bytes", resp.StatusCode, resp.Header.Get("Content-Type"), len(body))
	}
	// Anything that is not a document page is inert if navigated to.
	if got := resp.Header.Get(t3PolicyHeader); !strings.Contains(got, "default-src 'none'") || !strings.Contains(got, "sandbox") {
		t.Errorf("image %s = %q, want an inert policy", t3PolicyHeader, got)
	}
	if resp, _ := f.get(t, "/d/"+t3Token(f.other)); resp.StatusCode != http.StatusOK {
		t.Errorf("linked document: status %d", resp.StatusCode)
	}
}

// TestT3_NoRequestGrantsAFolder: T3's gateway forwards no fetch metadata,
// so every request it relays looks like one the user made. On the T3
// listener none of them may open a folder; only a link request does.
func TestT3_NoRequestGrantsAFolder(t *testing.T) {
	f := newT3Fixture(t)
	q := "?path=" + url.QueryEscape(f.doc)
	for _, path := range []string{
		t3DocPrefix + t3Token(f.doc),
		t3FilePrefix + t3Token(f.png),
		ChromeEditPath + q,
		ChromeFragmentPath + q + "&id=0000000000000000",
		pathURL(f.doc),
	} {
		resp, _ := f.get(t, path)
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 403 or 404", path, resp.StatusCode)
		}
	}
	if err := f.s.checkServed(f.doc); err == nil {
		t.Fatal("a request to the T3 listener made the viewer serve the folder")
	}
	if _, err := os.Stat(filepath.Join(f.s.CacheDir, rootsFileName)); err == nil {
		t.Fatal("a request to the T3 listener wrote a grant")
	}
}

// TestT3_ListenerHasOnlyEmbeddedRoutes: T3's capability covers the whole
// origin, so what the listener does not route, an embedded page cannot do.
func TestT3_ListenerHasOnlyEmbeddedRoutes(t *testing.T) {
	f := newT3Fixture(t)
	f.ask(t, linkRequest("r1", f.doc))
	q := "?path=" + url.QueryEscape(f.doc)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/"},
		{http.MethodGet, pathURL(f.doc)},
		{http.MethodGet, pathURL(f.png)},
		{http.MethodPost, MCPPath},
		{http.MethodGet, ChromePDFPath + q},
		{http.MethodPost, ChromeClipboardPath + q},
		{http.MethodPost, ChromeRevealPath + q},
		{http.MethodGet, ChromeFileManagerIconPath},
	} {
		req, _ := http.NewRequest(tc.method, f.ts.URL+tc.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s: status %d, want 404", tc.method, tc.path, resp.StatusCode)
		}
	}
}

// TestT3_FilesAreRasterImagesOnly: the gateway's policy lets a document a
// browser runs (SVG, HTML) script the frame, with the page's own access.
func TestT3_FilesAreRasterImagesOnly(t *testing.T) {
	f := newT3Fixture(t)
	f.ask(t, linkRequest("r1", f.doc))
	for _, name := range []string{"drawing.svg", "page.html", "table.csv", "other.md"} {
		resp, body := f.get(t, t3FilePrefix+t3Token(filepath.Join(f.dir, name)))
		if resp.StatusCode != http.StatusUnsupportedMediaType {
			t.Errorf("%s: status %d, want 415", name, resp.StatusCode)
		}
		if strings.Contains(body, "<script") {
			t.Errorf("%s: the file's bytes were served", name)
		}
	}
}

// TestT3_EditWithOnlyTheHeadersTheGatewayForwards saves through the T3
// listener the way an embedded page does: Content-Type and nothing else.
func TestT3_EditWithOnlyTheHeadersTheGatewayForwards(t *testing.T) {
	f := newT3Fixture(t)
	f.ask(t, linkRequest("r1", f.doc))
	u := f.ts.URL + ChromeEditPath + "?path=" + url.QueryEscape(f.doc)
	resp, body := doEdit(t, http.MethodGet, u+"&"+StampLineParam+"=1", "", "", nil)
	first, src, ok := strings.Cut(body, "\n")
	if resp.StatusCode != http.StatusOK || !ok || !strings.HasPrefix(first, "stamp ") {
		t.Fatalf("read: status %d, body %q", resp.StatusCode, body)
	}
	stamp := strings.TrimPrefix(first, "stamp ")
	resp, body = doEdit(t, http.MethodPut, u+"&"+StampParam+"="+url.QueryEscape(stamp), "", src+"\nadded\n",
		map[string]string{"Content-Type": "text/markdown; charset=utf-8"})
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(body, "stamp ") {
		t.Fatalf("save: status %d, body %q", resp.StatusCode, body)
	}
	if got, _ := os.ReadFile(f.doc); !strings.HasSuffix(string(got), "\nadded\n") {
		t.Fatalf("file not saved: %q", got)
	}
}

func TestT3_BrowserPagesFromOtherSitesAreRefused(t *testing.T) {
	f := newT3Fixture(t)
	f.ask(t, linkRequest("r1", f.doc))
	u := f.ts.URL + ChromeEditPath + "?path=" + url.QueryEscape(f.doc)
	resp, _ := doEdit(t, http.MethodGet, u, "", "", map[string]string{secFetchSiteHeader: "cross-site"})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site read of the source: status %d, want 403", resp.StatusCode)
	}
	resp, _ = doEdit(t, http.MethodGet, f.ts.URL+T3PluginPath, "", "", map[string]string{"Origin": "https://example.com"})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("plugin endpoint with a foreign Origin: status %d, want 403", resp.StatusCode)
	}
}

func activateRequest(id, path string, local bool) string {
	b, _ := json.Marshal(map[string]any{
		"type": "activate", "requestId": id, "scope": "link", "linkId": t3LinkID, "url": path,
		"client": map[string]bool{"local": local},
	})
	return string(b)
}

// TestT3_ActivateOpensHereAndDeclinesElsewhere: a click at this machine
// opens the document in the browser; a click anywhere else is declined, so
// T3 shows the card. Neither grants a folder on the T3 listener.
func TestT3_ActivateOpensHereAndDeclinesElsewhere(t *testing.T) {
	f := newT3Fixture(t)
	f.s.Addr = "127.0.0.1:18999"
	var opened []string
	fail := false
	f.s.OpenURL = func(u string) error {
		opened = append(opened, u)
		if fail {
			return errors.New("no browser")
		}
		return nil
	}
	handled := func(a map[string]any) any {
		act, _ := a["activate"].(map[string]any)
		if a["type"] != "result" || a["ok"] != true || act == nil {
			t.Fatalf("answer %v", a)
		}
		return act["handled"]
	}

	a := f.ask(t, activateRequest("r1", f.doc, false))[0]
	if handled(a) != false || len(opened) != 0 {
		t.Fatalf("a click elsewhere: %v, opened %v; want a decline and nothing opened here", a, opened)
	}
	a = f.ask(t, activateRequest("r2", f.doc, true))[0]
	want := "http://127.0.0.1:18999" + pathURL(f.doc)
	if handled(a) != true || len(opened) != 1 || opened[0] != want {
		t.Fatalf("a click here: %v, opened %v; want handled and %s", a, opened, want)
	}
	// An open that fails is a decline: the reader still gets the card.
	fail = true
	a = f.ask(t, activateRequest("r3", f.doc, true))[0]
	if handled(a) != false {
		t.Fatalf("a failed open: %v, want a decline", a)
	}
	// Not a Markdown path: an error, which T3 treats as an unclaimed link.
	a = f.ask(t, activateRequest("r4", filepath.Join(f.dir, "table.csv"), true))[0]
	if a["ok"] != false || a["activate"] != nil || len(opened) != 2 {
		t.Fatalf("a click on a non-Markdown path: %v, opened %v", a, opened)
	}
	if err := f.s.checkServed(f.doc); err == nil {
		t.Fatal("an activate request granted the folder on the T3 listener")
	}
}

func TestT3_OtherRequestsAreAnsweredNotGranted(t *testing.T) {
	f := newT3Fixture(t)
	outside := filepath.Join(f.dir, "notes.txt")
	answers := f.ask(t,
		`{"type":"subscribe","protocol":1,"subscriptionId":"s1","threadId":"t","cwd":"/w","filters":[]}`,
		`{"type":"resolve","requestId":"r2","scope":"link","linkId":"markdown","url":"`+filepath.ToSlash(f.doc)+`"}`,
		linkRequest("r3", outside),
		linkRequest("r4", "relative.md"),
		`{"type":"surface","requestId":"r5","scope":"thread","threadId":"t","cwd":"/w","panelId":"p","rowId":"r","surfaceId":"s"}`,
	)
	if answers[0]["type"] != "panels" || answers[0]["subscriptionId"] != "s1" {
		t.Errorf("subscribe: %v", answers[0])
	}
	for i, a := range answers[1:] {
		if a["type"] != "result" || a["ok"] != false || a["web"] != nil {
			t.Errorf("request %d: %v, want a refusal", i+2, a)
		}
	}
	if err := f.s.checkServed(f.doc); err == nil {
		t.Fatal("a refused request granted the folder")
	}
}

func TestT3Token_RoundTripsAndIsGatewaySafe(t *testing.T) {
	paths := []string{"/Users/me/My Drive/a b.md", "/tmp/naïve 100% #1?.md", "/x/y.markdown"}
	if runtime.GOOS == "windows" {
		paths = []string{`C:\Users\me\My Drive\a b.md`, `D:\x\naïve 100% #1.md`}
	}
	for _, p := range paths {
		token := t3Token(p)
		if !gatewaySafe(t3DocPrefix + token) {
			t.Errorf("token for %q is not a path T3's gateway relays: %s", p, token)
		}
		back, err := t3TokenPath(token)
		if err != nil || back != p {
			t.Errorf("token for %q decodes to %q, %v", p, back, err)
		}
	}
	for _, bad := range []string{"", "!!!", t3Token("/")[:1], "cmVsYXRpdmUubWQ"} { // the last is "relative.md"
		if p, err := t3TokenPath(bad); err == nil {
			t.Errorf("token %q decoded to %q, want an error", bad, p)
		}
	}
}

func TestT3Advertise_WritesAndRemovesItsOwnFile(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 49152}
	remove, err := t3Advertise(addr)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(data, "t3code", "plugins")
	file := filepath.Join(dir, "github.com+marcelocantos+vellum+49152.json")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var m t3Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Protocol != 1 || m.ID != T3PluginID || m.Name != T3PluginName || m.Connection.Kind != "server" || m.Connection.BaseURL != "http://127.0.0.1:49152/t3" {
		t.Fatalf("manifest %+v", m)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
			t.Errorf("plugin folder mode %v: T3 ignores a folder others can write", info.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("%d files in the plugin folder, want only the manifest", len(entries))
	}
	remove()
	if _, err := os.Stat(file); err == nil {
		t.Fatal("the manifest outlived the listener")
	}

	// A file another viewer wrote under the same name is not ours to remove.
	remove, err = t3Advertise(addr)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"someone":"else"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	remove()
	if _, err := os.Stat(file); err != nil {
		t.Fatal("removed a manifest it did not write")
	}
}

func TestDefaultT3Addr(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1:18742": "127.0.0.1:18743",
		"localhost:18744": "localhost:18745",
		"[::1]:9000":      "[::1]:9001",
		"127.0.0.1:65535": "",
		"nonsense":        "",
	} {
		if got := DefaultT3Addr(in); got != want {
			t.Errorf("DefaultT3Addr(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestListenAndServe_AdvertisesT3WhileRunning runs both listeners and
// checks the discovery file's lifetime is the server's.
func TestListenAndServe_AdvertisesT3WhileRunning(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	freePort := func() string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		return ln.Addr().String()
	}
	s := &Server{Addr: freePort(), T3Addr: freePort(), CacheDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.ListenAndServe(ctx) }()

	_, port, _ := net.SplitHostPort(s.T3Addr)
	file := filepath.Join(data, "t3code", "plugins", "github.com+marcelocantos+vellum+"+port+".json")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(file); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("no discovery file while the server runs")
		}
		time.Sleep(20 * time.Millisecond)
	}
	resp, err := http.Get("http://" + s.T3Addr + T3PluginPath)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("probe on the advertised address: status %d", resp.StatusCode)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ListenAndServe did not return")
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatal("the discovery file outlived the server")
	}
}
