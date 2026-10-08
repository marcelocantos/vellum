// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"context"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/embed"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

const mathDoc = "# Math\n\nInline $E = mc^2$ and display:\n\n$$\\int_0^\\infty e^{-x^2}\\,dx = \\frac{\\sqrt{\\pi}}{2}$$\n"

var cssURLRe = regexp.MustCompile(`url\(([^)]*)\)`)

// networkFetchRe matches the attributes and CSS references a browser or
// PDF engine would fetch over the network: stylesheet and script links,
// src attributes and url() values with an http(s) scheme. An xmlns on
// KaTeX's inline SVG is an identifier, not a fetch, so a bare "://"
// is not evidence.
var networkFetchRe = regexp.MustCompile(`(?i)(<link[^>]+href|<script[^>]+src|\bsrc)=["']https?://|url\(["']?https?://`)

// The vendored stylesheet must resolve entirely inside the embedded tree:
// every url() is a woff2 font that ships with it, and nothing points at
// a host. This is what makes "no network" a property of the bytes rather
// than of whichever machine rendered the document.
func TestKaTeXAssetsSelfContained(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(embed.KaTeXVersion) {
		t.Fatalf("embed.KaTeXVersion %q is not a release version", embed.KaTeXVersion)
	}
	tree := embed.KaTeX()
	css, err := fs.ReadFile(tree, embed.KaTeXCSSName)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(css, []byte("://")) {
		t.Fatalf("%s references a host", embed.KaTeXCSSName)
	}
	refs := cssURLRe.FindAllSubmatch(css, -1)
	if len(refs) == 0 {
		t.Fatalf("%s has no url() references; did vendoring strip the fonts?", embed.KaTeXCSSName)
	}
	seen := map[string]bool{}
	for _, m := range refs {
		ref := string(m[1])
		if !strings.HasPrefix(ref, embed.KaTeXFontsDir+"/") || !strings.HasSuffix(ref, ".woff2") {
			t.Errorf("stylesheet references %q; only %s/*.woff2 ships", ref, embed.KaTeXFontsDir)
			continue
		}
		if _, err := fs.Stat(tree, ref); err != nil {
			t.Errorf("stylesheet references %q, which is not embedded", ref)
		}
		seen[ref] = true
	}
	fonts, err := katexFontRefs()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fonts {
		if !seen[f] {
			t.Errorf("embedded font %q is not referenced by the stylesheet", f)
		}
	}
}

// The stylesheet must match the KaTeX that renders the HTML, or class
// names and metrics drift apart. Re-run scripts/vendor-katex.sh when
// this fails after an npm upgrade.
func TestKaTeXVersionMatchesRenderer(t *testing.T) {
	testdeps.Need(t, "node")
	cmd := exec.Command("node", "-e", katexResolveScript+`process.stdout.write(String(katex.version));`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("resolving katex via node: %v: %s", err, stderr.String())
	}
	if got := strings.TrimSpace(string(out)); got != embed.KaTeXVersion {
		t.Fatalf("renderer is katex %s but embed/katex is %s; run scripts/vendor-katex.sh", got, embed.KaTeXVersion)
	}
}

// A math document links vellum's own copy of the stylesheet, laid out
// under the user cache directory, and nothing on the network.
func TestRender_MathLinksLocalKaTeXCSS(t *testing.T) {
	testdeps.Need(t, "node")
	html, _, err := Render(context.Background(), []byte(mathDoc), nil)
	if err != nil {
		t.Fatal(err)
	}
	if m := networkFetchRe.FindString(html); m != "" {
		t.Fatalf("math HTML fetches %q from the network:\n%s", m, html)
	}
	m := regexp.MustCompile(`<link rel="stylesheet" href="(file://[^"]+)">`).FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("math HTML does not link a file:// KaTeX stylesheet:\n%s", html)
	}
	u, err := url.Parse(m[1])
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(u.Path) != embed.KaTeXCSSName {
		t.Fatalf("linked %q, want %s", u.Path, embed.KaTeXCSSName)
	}
	if !strings.Contains(u.Path, string(filepath.Separator)+embed.KaTeXVersion+string(filepath.Separator)) {
		t.Fatalf("linked %q is not under the %s version directory", u.Path, embed.KaTeXVersion)
	}
	onDisk, err := os.ReadFile(u.Path)
	if err != nil {
		t.Fatalf("linked stylesheet is not on disk: %v", err)
	}
	embedded, _ := fs.ReadFile(embed.KaTeX(), embed.KaTeXCSSName)
	if !bytes.Equal(onDisk, embedded) {
		t.Fatal("stylesheet on disk differs from the embedded one")
	}
	for _, m := range cssURLRe.FindAllSubmatch(onDisk, -1) {
		font := filepath.Join(filepath.Dir(u.Path), string(m[1]))
		if _, err := os.Stat(font); err != nil {
			t.Errorf("font %s is not beside the stylesheet: %v", m[1], err)
		}
	}
}

// A sink that serves the assets itself (the view server) links its own
// route instead, and then no cache directory is involved.
func TestRender_MathHonoursKaTeXCSSHref(t *testing.T) {
	testdeps.Need(t, "node")
	html, _, err := Render(context.Background(), []byte(mathDoc), &Options{KaTeXCSSHref: "/_vellum/katex/katex.min.css"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `<link rel="stylesheet" href="/_vellum/katex/katex.min.css">`) {
		t.Fatalf("math HTML does not link the given href:\n%s", html)
	}
	if strings.Contains(html, "file://") || networkFetchRe.MatchString(html) {
		t.Fatalf("math HTML links something besides the given href:\n%s", html)
	}
}

// With every outbound HTTP route pointed at a closed port, the PDF still
// embeds KaTeX's fonts, which can only have come from vellum's own copy.
func TestConvert_MathPDFOffline(t *testing.T) {
	testdeps.Need(t, "node", "pdffonts")
	for _, v := range []string{"http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY"} {
		t.Setenv(v, "http://127.0.0.1:9")
	}
	t.Setenv("no_proxy", "")
	t.Setenv("NO_PROXY", "")
	for _, backend := range backends(t) {
		t.Run(backend, func(t *testing.T) {
			dir := t.TempDir()
			in := filepath.Join(dir, "math.md")
			out := filepath.Join(dir, "math.pdf")
			if err := os.WriteFile(in, []byte(mathDoc), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := Convert(context.Background(), in, out, &Options{Backend: backend}); err != nil {
				t.Fatalf("Convert: %v", err)
			}
			fonts, err := exec.Command("pdffonts", out).CombinedOutput()
			if err != nil {
				t.Fatalf("pdffonts: %v: %s", err, fonts)
			}
			if !strings.Contains(string(fonts), "KaTeX_Main") || !strings.Contains(string(fonts), "KaTeX_Math") {
				t.Fatalf("PDF lacks KaTeX fonts, so its stylesheet was not applied:\n%s", fonts)
			}
		})
	}
}
