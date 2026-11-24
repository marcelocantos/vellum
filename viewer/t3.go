// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/coder/websocket"

	"github.com/marcelocantos/vellum/embed"
)

// T3 Code shows the viewer inside its own window: a Markdown file link
// clicked in a chat opens the document as a card, on any T3 client, so a
// phone or another machine reads and edits a file on the machine that
// hosts the thread. T3 reaches a plugin's pages only through its gateway,
// and that gateway shapes everything here:
//
//   - It forwards no fetch metadata. On the ordinary listener every
//     relayed request would look like one the user made and would grant
//     its folder (see roots.go). So T3 gets a listener of its own, where
//     no request grants anything: only T3's own link request does, over
//     the plugin socket.
//   - Its capability covers the plugin's whole origin, so the T3 listener
//     has only the routes an embedded page needs. No /mcp, no clipboard,
//     no reveal, no PDF, no homepage, no raw file but a raster image.
//   - It replaces the viewer's Content-Security-Policy with its own,
//     which allows inline script. Nothing served here may be a document
//     someone else wrote that a browser would run (HTML, SVG), and an
//     embedded page restates its script policy in a <meta> element.
//   - It refuses a path whose decoded form has a space or a percent
//     sign, and a card's path cannot carry a query. A document is
//     therefore addressed by one base64url segment, never by its path.
//   - It forwards only Accept and Content-Type and returns only
//     Content-Type, so stamps travel in the query and the body (edit.go).
//
// The protocol is T3 Code's panel-plugin protocol version 1
// (docs/operations/panel-plugins.md in the T3 Code plugin build).
const (
	// T3PluginID is the plugin's identity in T3 Code: a user's decision to
	// enable it is remembered under this id.
	T3PluginID = "github.com/marcelocantos/vellum"
	// T3PluginName is the name T3 shows in Settings → Plugins.
	T3PluginName = "Vellum"
	// T3PluginPath is the plugin endpoint on the T3 listener: a plain GET
	// is T3's probe, a WebSocket upgrade is its protocol connection.
	T3PluginPath = "/t3"
	// t3DocPrefix serves a document: /d/<token>, where the token is
	// t3Token of its path.
	t3DocPrefix = "/d/"
	// t3FilePrefix serves a raster image a document shows: /f/<token>.
	t3FilePrefix = "/f/"
	// t3Root is the listener's root as seen from a document page. Every
	// URL an embedded page uses is relative, because the page's own URL is
	// the gateway's.
	t3Root = "../"

	t3Protocol = 1
	// t3LinkID names the one link pattern: Markdown file links.
	t3LinkID = "markdown"
	// t3LinkGlob is matched by T3 against a file link's absolute path.
	t3LinkGlob = "/**/*.{md,markdown}"
	// t3CardHeight is the card's starting height in px (T3 allows 80 to
	// 800). The page asks for the whole window once it is shown.
	t3CardHeight = 640
	// t3PortOffset places the T3 listener beside the view listener when no
	// address is given: the port must not change between runs, because T3
	// asks the user again when an enabled plugin moves.
	t3PortOffset = 1
	// t3FrameLimit bounds one protocol frame; T3 sends at most 1 MiB.
	t3FrameLimit = 1 << 20
)

// t3ImageTypes are the files /f/ serves, by extension. Raster images
// only: the gateway's policy lets an SVG or HTML file run script if the
// frame is navigated to it.
var t3ImageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
}

// t3Token names an absolute path in one URL segment: the base64url of its
// URL-path form (/Users/me/a b.md, /C:/docs/a.md), which chrome.js can
// also build from the path a watch message carries.
func t3Token(absPath string) string {
	slash := filepath.ToSlash(absPath)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return base64.RawURLEncoding.EncodeToString([]byte(slash))
}

// t3TokenPath is t3Token's inverse. It returns a cleaned absolute path or
// an error; whether the viewer serves that path is checked where it is
// opened.
func t3TokenPath(token string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", fmt.Errorf("bad document token")
	}
	u := string(raw)
	if !strings.HasPrefix(u, "/") || strings.ContainsRune(u, 0) {
		return "", fmt.Errorf("bad document token")
	}
	if runtime.GOOS == "windows" {
		return windowsURLPathToFS(u)
	}
	cleaned := filepath.Clean(u)
	if cleaned == "/" {
		return "", fmt.Errorf("missing file path")
	}
	return cleaned, nil
}

// DefaultT3Addr is the T3 listener's address when none is given: the view
// address with the next port, or "" when viewAddr has no usable port.
func DefaultT3Addr(viewAddr string) string {
	host, port, err := net.SplitHostPort(viewAddr)
	if err != nil {
		return ""
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 || n+t3PortOffset > 65535 {
		return ""
	}
	return net.JoinHostPort(host, strconv.Itoa(n+t3PortOffset))
}

// T3Handler returns the HTTP handler for the T3 listener.
func (s *Server) T3Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(HealthPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc(T3PluginPath, s.handleT3Plugin)
	mux.HandleFunc(t3DocPrefix, s.handleT3Doc)
	mux.HandleFunc(t3FilePrefix, s.handleT3File)
	mux.HandleFunc(ChromeWatchPath, s.handleWatch)
	mux.HandleFunc(ChromeTaskTogglePath, s.handleTaskToggle)
	mux.HandleFunc(ChromeEditPath, s.handleEdit)
	mux.HandleFunc(ChromeEditorPrefix, s.handleEditorAsset)
	mux.HandleFunc(ChromeFragmentPath, s.handleFragment)
	mux.HandleFunc(ChromeFaviconPath, s.handleFavicon)
	mux.Handle(ChromeKaTeXPrefix, http.StripPrefix(ChromeKaTeXPrefix, http.FileServerFS(embed.KaTeX())))
	stateChanging := map[string]bool{
		T3PluginPath:         true,
		ChromeTaskTogglePath: true,
		ChromeEditPath:       true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// T3's gateway connects from this machine and forwards neither
		// Origin nor fetch metadata, so it passes as a local client. A
		// browser page that reaches this port directly is held to the
		// same rule as on the view listener.
		_, pattern := mux.Handler(r)
		if r.URL.Path != HealthPath && !localRequestAllowed(r, stateChanging[pattern]) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set(cspHeader, lockdownCSP)
		mux.ServeHTTP(w, r)
	})
}

// t3Probe is the answer to T3's probe, and what the discovery file says.
type t3Probe struct {
	Protocol int      `json:"protocol"`
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Scopes   []string `json:"scopes,omitempty"`
	Links    []t3Link `json:"links,omitempty"`
}

type t3Link struct {
	ID    string `json:"id"`
	Glob  string `json:"glob"`
	Click string `json:"click"`
}

func (s *Server) handleT3Plugin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		s.serveT3Socket(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(t3Probe{
		Protocol: t3Protocol,
		ID:       T3PluginID,
		Name:     T3PluginName,
		Scopes:   []string{"links"},
		Links:    []t3Link{{ID: t3LinkID, Glob: t3LinkGlob, Click: "card"}},
	})
}

// t3Frame is one frame from T3. Fields a frame type does not use are
// empty.
type t3Frame struct {
	Type           string `json:"type"`
	RequestID      string `json:"requestId"`
	SubscriptionID string `json:"subscriptionId"`
	Scope          string `json:"scope"`
	LinkID         string `json:"linkId"`
	URL            string `json:"url"`
}

type t3WebCard struct {
	Path   string `json:"path"`
	Height int    `json:"height"`
}

type t3Result struct {
	Type      string     `json:"type"`
	RequestID string     `json:"requestId"`
	OK        bool       `json:"ok"`
	Error     string     `json:"error,omitempty"`
	Web       *t3WebCard `json:"web,omitempty"`
}

type t3Panels struct {
	Type           string   `json:"type"`
	SubscriptionID string   `json:"subscriptionId"`
	Panels         []string `json:"panels"`
}

// serveT3Socket is T3's protocol connection. The plugin serves link cards
// only, so the one request that matters is a `surface` for a link: the
// user pointed at or clicked a Markdown file link in a chat.
func (s *Server) serveT3Socket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(t3FrameLimit)
	ctx := r.Context()
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var frame t3Frame
		if json.Unmarshal(data, &frame) != nil {
			continue
		}
		var reply any
		switch {
		case frame.Type == "surface" && frame.RequestID != "":
			reply = s.t3LinkCard(&frame)
		case frame.Type == "subscribe" && frame.SubscriptionID != "":
			// No panels: an empty snapshot keeps T3 from waiting for one.
			reply = t3Panels{Type: "panels", SubscriptionID: frame.SubscriptionID, Panels: []string{}}
		case frame.RequestID != "":
			reply = t3Result{Type: "result", RequestID: frame.RequestID, Error: "Vellum does not handle this request."}
		default:
			continue
		}
		out, err := json.Marshal(reply)
		if err != nil {
			continue
		}
		if err := conn.Write(ctx, websocket.MessageText, out); err != nil {
			return
		}
	}
}

// t3LinkCard answers a link's surface request with the document's page.
// This is the one place the T3 listener grants a folder: the request is
// T3 relaying the user's own pointer on a link, the same act as opening
// the document with vellum view.
func (s *Server) t3LinkCard(frame *t3Frame) t3Result {
	res := t3Result{Type: "result", RequestID: frame.RequestID}
	if frame.Scope != "link" || frame.LinkID != t3LinkID {
		res.Error = "Vellum shows Markdown file links only."
		return res
	}
	abs := filepath.Clean(frame.URL)
	if !filepath.IsAbs(frame.URL) || !isMarkdownPath(abs) || filepath.Dir(abs) == abs {
		res.Error = "Not a Markdown file path."
		return res
	}
	if err := s.grant(abs); err != nil {
		res.Error = "Vellum cannot open that folder."
		return res
	}
	res.OK = true
	res.Web = &t3WebCard{Path: t3DocPrefix + t3Token(abs), Height: t3CardHeight}
	return res
}

func (s *Server) handleT3Doc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	abs, err := t3TokenPath(strings.TrimPrefix(r.URL.Path, t3DocPrefix))
	if err != nil || !isMarkdownPath(abs) {
		http.NotFound(w, r)
		return
	}
	s.serveMarkdown(w, r, abs, true)
}

func (s *Server) handleT3File(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	abs, err := t3TokenPath(strings.TrimPrefix(r.URL.Path, t3FilePrefix))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctype, ok := t3ImageTypes[strings.ToLower(filepath.Ext(abs))]
	if !ok {
		http.Error(w, "only raster images are shown inside T3 Code", http.StatusUnsupportedMediaType)
		return
	}
	f, err := s.openServed(abs)
	if servedStatus(w, r, err) {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", info.ModTime(), f)
}

var (
	srcAttrRe = regexp.MustCompile(`(?i)(<img\b[^>]*?\bsrc\s*=\s*)(["'])([^"']*)(["'])`)
	baseTagRe = regexp.MustCompile(`(?i)<base\b[^>]*>\s*`)
)

// embedLocalRefs makes a rendered document's references work from an
// embedded page, which has no <base> and whose own URL is not the
// document's path. A link to a Markdown file becomes that document's
// page; an image in a served folder becomes its /f/ URL; the viewer's
// own assets become relative. A link to any other local file is left
// without a target: nothing on the T3 listener would serve it, and a
// stray relative URL must not reach another route.
func embedLocalRefs(page, sourcePath string) string {
	dir := filepath.Dir(sourcePath)
	page = baseTagRe.ReplaceAllString(page, "")
	page = hrefAttrRe.ReplaceAllStringFunc(page, func(m string) string {
		sub := hrefAttrRe.FindStringSubmatch(m)
		prefix, q, raw := sub[1], sub[2], sub[3]
		if strings.HasPrefix(raw, ChromePrefix) {
			return prefix + q + t3Root + strings.TrimPrefix(raw, "/") + q
		}
		abs, frag, ok := localRef(raw, dir)
		if !ok {
			return m
		}
		if !isMarkdownPath(abs) {
			return `data-vellum-local=` + q + q
		}
		href := t3Root + strings.TrimPrefix(t3DocPrefix, "/") + t3Token(abs)
		if frag != "" {
			href += "#" + frag
		}
		return prefix + q + href + q
	})
	return srcAttrRe.ReplaceAllStringFunc(page, func(m string) string {
		sub := srcAttrRe.FindStringSubmatch(m)
		prefix, q, raw := sub[1], sub[2], sub[3]
		abs, _, ok := localRef(raw, dir)
		if !ok {
			return m
		}
		return prefix + q + t3Root + strings.TrimPrefix(t3FilePrefix, "/") + t3Token(abs) + q
	})
}

// localRef resolves raw (an href or src as written in the page: HTML- and
// URL-escaped) against dir. ok is false for a reference that is not a
// local file: a fragment, or anything with a scheme.
func localRef(raw, dir string) (abs, frag string, ok bool) {
	ref := strings.TrimSpace(html.UnescapeString(raw))
	if ref == "" || strings.HasPrefix(ref, "#") {
		return "", "", false
	}
	ref, frag = splitFragment(ref)
	if i := strings.IndexByte(ref, '?'); i >= 0 {
		ref = ref[:i]
	}
	if ref == "" || strings.HasPrefix(ref, "//") || hasScheme(ref) {
		return "", "", false
	}
	path, err := url.PathUnescape(ref)
	if err != nil {
		return "", "", false
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return filepath.Clean(path), frag, true
}

// hasScheme reports whether ref starts with a URL scheme (http:, data:,
// mailto:). A Windows drive letter is not one.
func hasScheme(ref string) bool {
	i := strings.IndexByte(ref, ':')
	if i <= 0 {
		return false
	}
	if i == 1 && runtime.GOOS == "windows" {
		return false
	}
	for _, c := range ref[:i] {
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		if !letter && !(c >= '0' && c <= '9') && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

// t3Manifest is the discovery file T3 reads.
type t3Manifest struct {
	Protocol   int          `json:"protocol"`
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Connection t3Connection `json:"connection"`
}

type t3Connection struct {
	Kind    string `json:"kind"`
	BaseURL string `json:"baseUrl"`
}

// t3PluginDir is where T3 looks for advertisements:
// $XDG_DATA_HOME/t3code/plugins, or ~/.local/share/t3code/plugins.
func t3PluginDir() (string, error) {
	if x := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "t3code", "plugins"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "t3code", "plugins"), nil
}

// t3Advertise writes the discovery file for the T3 listener at ln and
// returns a function that removes it, if it still holds what was written.
// Each running viewer has its own file, named with its port, so a dev and
// a release viewer never share one.
func t3Advertise(ln net.Addr) (remove func(), err error) {
	ta, ok := ln.(*net.TCPAddr)
	if !ok {
		return nil, errors.New("not a TCP listener")
	}
	dir, err := t3PluginDir()
	if err != nil {
		return nil, err
	}
	// T3 ignores a directory other users can write to.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	port := strconv.Itoa(ta.Port)
	body, err := json.MarshalIndent(t3Manifest{
		Protocol: t3Protocol,
		ID:       T3PluginID,
		Name:     T3PluginName,
		Connection: t3Connection{
			Kind:    "server",
			BaseURL: "http://" + net.JoinHostPort("127.0.0.1", port) + T3PluginPath,
		},
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	body = append(body, '\n')
	name := strings.ReplaceAll(T3PluginID, "/", "+") + "+" + port + ".json"
	path := filepath.Join(dir, name)
	// T3 skips names that begin with a dot, so a half-written file is
	// never read.
	tmp := filepath.Join(dir, "."+name+".tmp")
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return func() {
		if got, err := os.ReadFile(path); err == nil && string(got) == string(body) {
			_ = os.Remove(path)
		}
	}, nil
}

// serveT3 runs the T3 listener at Server.T3Addr until ctx ends. It never
// fails the view server: a port that is taken, or a discovery file that
// cannot be written, is reported and the viewer carries on without T3.
func (s *Server) serveT3(ctx context.Context) {
	addr := s.T3Addr
	report := func(msg string) {
		if s.Log != nil {
			s.Log(msg)
		}
	}
	if err := requireLoopbackAddr(addr); err != nil {
		report("T3 Code plugin off: " + err.Error())
		return
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		report("T3 Code plugin off: " + err.Error())
		return
	}
	remove, err := t3Advertise(ln.Addr())
	if err != nil {
		report("T3 Code plugin not advertised: " + err.Error())
		remove = func() {}
	}
	srv := &http.Server{Handler: s.T3Handler(), ReadHeaderTimeout: readHeaderTimeout}
	go func() {
		<-ctx.Done()
		remove()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	report("T3 Code plugin listening on http://" + ln.Addr().String() + T3PluginPath)
	_ = srv.Serve(ln)
}
