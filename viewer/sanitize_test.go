// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Payloads a document can carry, in raw HTML and inside the Markdown
// constructs whose text ends up in attributes or elements. Each one sets
// window.__pwned or alerts if it ever runs; the browser journey
// (scripts/viewer-security-journey.cjs) loads the same kinds of document
// in Chrome and checks that nothing ran.
var documentPayloads = []struct{ name, md string }{
	{"raw script block", "<script>window.__pwned = 1</script>\n"},
	{"raw script inline", "text <script>window.__pwned = 1</script> text\n"},
	{"event handler", "<img src=x onerror=\"window.__pwned=1\">\n"},
	{"event handler inline", "a <span onmouseover=\"window.__pwned=1\">b</span>\n"},
	{"body onload", "<body onload=\"window.__pwned=1\">\n"},
	{"details ontoggle", "<details open ontoggle=\"window.__pwned=1\"><summary>s</summary>x</details>\n"},
	{"markdown javascript link", "[go](javascript:window.__pwned=1)\n"},
	{"markdown javascript link encoded", "[go](JaVaScRiPt&#58;window.__pwned=1)\n"},
	{"markdown javascript autolink", "<javascript:window.__pwned=1>\n"},
	{"reference javascript link", "[go][r]\n\n[r]: javascript:window.__pwned=1\n"},
	{"markdown data link", "[go](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)\n"},
	{"markdown javascript image", "![x](javascript:window.__pwned=1)\n"},
	{"raw javascript href", "<a href=\"javascript:window.__pwned=1\">x</a>\n"},
	{"raw data image svg", "<img src=\"data:image/svg+xml,<svg onload='window.__pwned=1'>\">\n"},
	{"svg with script", "<svg><script>window.__pwned=1</script></svg>\n"},
	{"svg onload", "<svg onload=\"window.__pwned=1\"></svg>\n"},
	{"svg link javascript", "<svg><a href=\"javascript:window.__pwned=1\"><text>x</text></a></svg>\n"},
	{"svg set href", "<svg><a href=\"#x\"><set attributeName=\"href\" to=\"javascript:window.__pwned=1\"/><text>x</text></a></svg>\n"},
	{"svg foreignObject script", "<svg><foreignObject><div><script>window.__pwned=1</script><img src=x onerror=\"window.__pwned=1\"></div></foreignObject></svg>\n"},
	{"iframe", "<iframe src=\"javascript:window.top.__pwned=1\"></iframe>\n"},
	{"iframe srcdoc", "<iframe srcdoc=\"<script>parent.__pwned=1</script>\"></iframe>\n"},
	{"object", "<object data=\"javascript:window.__pwned=1\"></object>\n"},
	{"embed", "<embed src=\"javascript:window.__pwned=1\">\n"},
	{"meta refresh", "<meta http-equiv=\"refresh\" content=\"0;url=javascript:window.__pwned=1\">\n"},
	{"base", "<base href=\"https://evil.example/\">\n"},
	{"style element", "<style>.vellum-toolbar{display:none}</style>\n"},
	{"style attribute", "<div style=\"position:fixed;inset:0;background:red\">cover</div>\n"},
	{"form", "<form action=\"https://evil.example/\"><button>go</button></form>\n"},
	{"link title", "[a](https://example.com \"\\\"><script>window.__pwned=1</script>\")\n"},
	{"image alt", "![\"><img src=x onerror=window.__pwned=1>](pic.png)\n"},
	{"image title", "![a](pic.png \"\\\" onerror=\\\"window.__pwned=1\")\n"},
	{"code fence info", "```\"><script>window.__pwned=1</script>\nx\n```\n"},
	{"code fence info attr", "``` x\" onmouseover=\"window.__pwned=1\nx\n```\n"},
	{"code span", "`<script>window.__pwned=1</script>`\n"},
	{"footnote body", "a[^1]\n\n[^1]: <img src=x onerror=\"window.__pwned=1\">\n"},
	{"footnote label", "a[^\"><script>window.__pwned=1</script>]\n\n[^\"><script>window.__pwned=1</script>]: b\n"},
	{"raw html block", "<div>\n<img src=x onerror=\"window.__pwned=1\">\n</div>\n"},
	{"html block split tag", "<div>\n<img src=x alt=\"\n\n\" onerror=\"window.__pwned=1\">\n"},
	{"table cell", "| a |\n|---|\n| <img src=x onerror=\"window.__pwned=1\"> |\n"},
	{"heading", "# <img src=x onerror=\"window.__pwned=1\">\n"},
	{"front matter title", "---\ntitle: \"</title><script>window.__pwned=1</script>\"\n---\n# x\n"},
	{"closing body early", "</body></html><script>window.__pwned=1</script>\n"},
	{"unclosed comment", "<!-- swallow the chrome\n"},
	{"forged chrome class", "<div class=\"vellum-editor vellum-deferred vellum-deferred-mermaid\" data-vellum-fragment=\"0000000000000000\">x</div>\n"},
	{"name clobbering", "<img name=\"getElementById\" src=\"pic.png\"><form name=\"body\"></form>\n"},
}

// scriptCheck walks a served page and reports anything that could run
// besides the viewer's own nonce'd scripts.
func scriptCheck(t *testing.T, page, nonce string) []string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	scripts := 0
	var walk func(n *html.Node, inArticle bool)
	walk = func(n *html.Node, inArticle bool) {
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			for _, a := range n.Attr {
				key := strings.ToLower(a.Key)
				val := strings.ToLower(strings.Join(strings.FieldsFunc(a.Val, func(r rune) bool { return r <= ' ' }), ""))
				if strings.HasPrefix(key, "on") {
					found = append(found, tag+" "+key)
				}
				if (key == "href" || key == "src" || key == "action" || key == "data" || key == "formaction") &&
					(strings.HasPrefix(val, "javascript:") || strings.HasPrefix(val, "vbscript:") || strings.HasPrefix(val, "data:text")) {
					found = append(found, tag+" "+key+"="+a.Val)
				}
				if key == "class" && strings.Contains(val, "vellum-") && inArticle && !strings.HasPrefix(val, "vellum-deferred") {
					found = append(found, tag+" class="+a.Val)
				}
				if key == "name" && tag != "a" && inArticle {
					found = append(found, tag+" name="+a.Val)
				}
			}
			switch tag {
			case "script":
				scripts++
				if attr(n, "nonce") != nonce || nonce == "" {
					found = append(found, "script without this response's nonce")
				}
			case "iframe", "object", "embed", "frame", "form", "meta":
				if inArticle {
					found = append(found, "<"+tag+"> in the article")
				}
			case "set", "animate", "animatemotion", "animatetransform", "animatecolor":
				if name := strings.ToLower(strings.TrimSpace(attr(n, "attributeName"))); name == "href" || strings.HasSuffix(name, ":href") {
					found = append(found, "<"+tag+"> rewrites a link")
				}
			case "style":
				// A drawing's own <style> is part of the drawing (🎯T83);
				// an HTML one is not allowed.
				if inArticle && n.Namespace == "" {
					found = append(found, "<style> in the article")
				}
			case "base":
				if !strings.HasPrefix(attr(n, "href"), "http://127.0.0.1") {
					found = append(found, "foreign <base>")
				}
			}
			if strings.Contains(attr(n, "class"), "vellum-article") {
				inArticle = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inArticle)
		}
	}
	walk(doc, false)
	if scripts != 2 {
		found = append(found, "script elements: "+string(rune('0'+scripts))+" (want the viewer's 2)")
	}
	return found
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func TestServer_DocumentHTMLCannotRun(t *testing.T) {
	for _, p := range documentPayloads {
		t.Run(p.name, func(t *testing.T) {
			dir := t.TempDir()
			md := filepath.Join(dir, "doc.md")
			if err := os.WriteFile(md, []byte("# Doc\n\n"+p.md+"\nTail paragraph.\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			s := &Server{CacheDir: filepath.Join(dir, "cache"), Roots: []string{dir}}
			ts := startTestServer(t, s)
			resp, err := http.Get(ts.URL + pathURL(md))
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", resp.StatusCode, body)
			}
			csp := resp.Header.Get(cspHeader)
			nonce := cspNonce(csp)
			if nonce == "" {
				t.Fatalf("no script nonce in CSP %q", csp)
			}
			if found := scriptCheck(t, string(body), nonce); len(found) > 0 {
				t.Fatalf("document content could run: %v\n%s", found, articleOf(string(body)))
			}
			if !strings.Contains(string(body), "Tail paragraph.") {
				t.Fatalf("content after the payload was lost:\n%s", articleOf(string(body)))
			}
		})
	}
}

// The CSP is the second layer: it must refuse inline script, script
// URLs and remote script even if the sanitiser let one through.
func TestServer_DocumentCSP(t *testing.T) {
	dir, md := writeDoc(t, "# Doc\n")
	s := &Server{CacheDir: filepath.Join(dir, "cache"), Roots: []string{dir}}
	ts := startTestServer(t, s)
	var nonces []string
	for range 2 {
		resp, err := http.Get(ts.URL + pathURL(md))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		csp := resp.Header.Get(cspHeader)
		directives := map[string]string{}
		for d := range strings.SplitSeq(csp, ";") {
			name, val, _ := strings.Cut(strings.TrimSpace(d), " ")
			directives[name] = val
		}
		script := directives["script-src"]
		if script == "" || directives["default-src"] != "'none'" {
			t.Fatalf("CSP %q: want default-src 'none' and a script-src", csp)
		}
		for _, banned := range []string{"'unsafe-inline'", "'unsafe-eval'", "*", "data:", "http:", "https:", "'self'"} {
			for _, tok := range strings.Fields(script) {
				if tok == banned {
					t.Fatalf("script-src admits %s: %q", banned, script)
				}
			}
		}
		if !strings.Contains(script, ChromeEditorPrefix) {
			t.Fatalf("script-src does not admit the editor bundle: %q", script)
		}
		for _, tok := range strings.Fields(directives["connect-src"]) {
			if tok != "'self'" && !strings.HasPrefix(tok, "ws://127.0.0.1:") {
				t.Fatalf("connect-src reaches %s: %q", tok, csp)
			}
		}
		for _, d := range []string{"base-uri", "form-action", "frame-ancestors"} {
			if directives[d] == "" {
				t.Fatalf("CSP lacks %s: %q", d, csp)
			}
		}
		nonce := cspNonce(csp)
		if strings.Count(string(body), `nonce="`+nonce+`"`) != 2 {
			t.Fatalf("viewer scripts do not carry the response nonce %q", nonce)
		}
		nonces = append(nonces, nonce)
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("page lacks nosniff")
		}
	}
	if nonces[0] == nonces[1] {
		t.Fatalf("nonce reused across responses: %q", nonces[0])
	}
}

// A file a document links to (HTML, SVG) opens sandboxed: the browser
// shows it without running its script on the viewer's origin.
func TestServer_LinkedFilesAreSandboxed(t *testing.T) {
	dir, _ := writeDoc(t, "# Doc\n")
	s := &Server{CacheDir: filepath.Join(dir, "cache"), Roots: []string{dir}}
	ts := startTestServer(t, s)
	for name, body := range map[string]string{
		"page.html":  "<script>parent.__pwned=1</script>",
		"image.svg":  `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`,
		"script.js":  "window.__pwned=1",
		"notes.txt":  "<script>alert(1)</script>",
		"data.xhtml": `<html xmlns="http://www.w3.org/1999/xhtml"><script>alert(1)</script></html>`,
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodGet, ts.URL+pathURL(p), nil)
		req.Header.Set(secFetchSiteHeader, secFetchSiteSameOrigin)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", name, resp.StatusCode)
		}
		csp := resp.Header.Get(cspHeader)
		if !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
			t.Fatalf("%s served with CSP %q, want sandboxed", name, csp)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s lacks nosniff", name)
		}
	}
}

func cspNonce(csp string) string {
	_, rest, ok := strings.Cut(csp, "'nonce-")
	if !ok {
		return ""
	}
	nonce, _, _ := strings.Cut(rest, "'")
	return nonce
}

func articleOf(page string) string {
	start := strings.Index(page, `class="vellum-article"`)
	end := strings.Index(page, `id="vellum-consent"`)
	if start < 0 || end < start {
		return page
	}
	return page[start:end]
}
