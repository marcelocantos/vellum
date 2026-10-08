// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/embed"
)

// A viewed math document links the view server's own KaTeX route, and
// that route serves the embedded stylesheet and fonts, so the page loads
// nothing from the network.
func TestServer_KaTeXServedLocally(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "math.md")
	if err := os.WriteFile(doc, []byte("# Math\n\n$$a^2 + b^2 = c^2$$\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: filepath.Join(dir, "cache")}
	ts := startTestServer(t, s)

	get := func(url string) (int, string, []byte) {
		t.Helper()
		resp, err := http.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), body
	}

	status, _, page := get(ViewURL(ts.URL, doc))
	if status != http.StatusOK {
		t.Fatalf("page status %d: %s", status, page)
	}
	link := `<link rel="stylesheet" href="` + ChromeKaTeXPrefix + embed.KaTeXCSSName + `">`
	if !strings.Contains(string(page), link) {
		t.Fatalf("page does not link %s:\n%s", link, page)
	}
	// <base> names this server's own origin; anything else with an http(s)
	// scheme in a fetchable position would leave the machine.
	external := regexp.MustCompile(`(?i)(<link[^>]+href|<script[^>]+src|\bsrc)=["']https?://|url\(["']?https?://`)
	if m := external.FindString(string(page)); m != "" {
		t.Fatalf("page fetches %q from the network:\n%s", m, page)
	}

	status, ctype, css := get(ts.URL + ChromeKaTeXPrefix + embed.KaTeXCSSName)
	if status != http.StatusOK || !strings.HasPrefix(ctype, "text/css") {
		t.Fatalf("stylesheet: status %d, type %q", status, ctype)
	}
	embedded, _ := fs.ReadFile(embed.KaTeX(), embed.KaTeXCSSName)
	if !bytes.Equal(css, embedded) {
		t.Fatal("served stylesheet differs from the embedded one")
	}

	font := embed.KaTeXFontsDir + "/KaTeX_Main-Regular.woff2"
	status, ctype, body := get(ts.URL + ChromeKaTeXPrefix + font)
	if status != http.StatusOK || !strings.HasPrefix(ctype, "font/woff2") {
		t.Fatalf("font: status %d, type %q", status, ctype)
	}
	if !bytes.HasPrefix(body, []byte("wOF2")) {
		t.Fatalf("font body is not woff2: % x", body[:4])
	}
}
