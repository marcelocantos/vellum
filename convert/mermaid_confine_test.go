// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/vellum/internal/fileurl"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

func TestMmdcCommand_RefusesAShellScript(t *testing.T) {
	bin := t.TempDir()
	script := filepath.Join(bin, "mmdc")
	if runtime.GOOS == "windows" {
		script += ".cmd"
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho unconfined\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := mmdcCommand(context.Background(), []string{"--version"})
	if err == nil || !strings.Contains(err.Error(), "unconfined") {
		t.Fatalf("err = %v, want a refusal to run mmdc unconfined", err)
	}
}

func TestMmdcCommand_ImportsTheConfineHook(t *testing.T) {
	testdeps.Need(t, "node")
	bin := t.TempDir()
	body := "#!/usr/bin/env node\n"
	name := "mmdc"
	if runtime.GOOS == "windows" {
		// This case is the shebang path. Windows uses the npm shim,
		// covered by the refusal test above when the shim is not one.
		t.Skip("shebang mmdc is the non-Windows launch")
	}
	script := filepath.Join(bin, name)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd, err := mmdcCommand(context.Background(), []string{"-e", "png"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cmd.Path, "node") && filepath.Base(cmd.Path) != "node" {
		t.Fatalf("command path = %q, want node", cmd.Path)
	}
	if len(cmd.Args) < 4 || cmd.Args[1] != "--import" || !strings.Contains(cmd.Args[2], "vellum-mmdc-confine-") || cmd.Args[3] != script {
		t.Fatalf("args = %#v, want node --import <hook> %s", cmd.Args, script)
	}
	hookPath, ok := fileurl.ToPath(cmd.Args[2])
	if !ok {
		t.Fatalf("--import %q is not a file: URL; Node on Windows refuses a bare path", cmd.Args[2])
	}
	hook, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(hook, []byte("registerHooks")) || !bytes.Contains(hook, []byte("request.respond")) {
		t.Fatal("hook file is not the confine hook")
	}
}

// TestMermaid_RefusesARenderTheHookDidNotConfine: the hook rewrites
// mermaid-cli only when it recognises the file by its URL
// (/@mermaid-js/mermaid-cli/src/…). An mmdc it does not recognise (a
// source checkout, a copy in another folder, a release that moves or
// bundles those files) runs with no rewrite and the browser loads
// whatever the diagram names. The stand-in here is a Node script that
// writes an image and exits 0, as any unrecognised mmdc would; vellum must
// not take its output.
func TestMermaid_RefusesARenderTheHookDidNotConfine(t *testing.T) {
	testdeps.Need(t, "node")
	bin := t.TempDir()
	const fake = `const fs = require("fs");
const o = process.argv[process.argv.indexOf("-o") + 1];
fs.writeFileSync(o, o.endsWith(".svg") ? "<svg xmlns='http://www.w3.org/2000/svg'/>" : "not confined");
`
	if runtime.GOOS == "windows" {
		// npm's global bin: mmdc.cmd names the script relative to itself.
		if err := os.MkdirAll(filepath.Join(bin, "fake"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, "fake", "cli.js"), []byte(fake), 0o644); err != nil {
			t.Fatal(err)
		}
		shimText := "@SETLOCAL\r\n\"%_prog%\"  \"%dp0%\\fake\\cli.js\" %*\r\n"
		if err := os.WriteFile(filepath.Join(bin, "mmdc.cmd"), []byte(shimText), 0o644); err != nil {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(filepath.Join(bin, "mmdc"), []byte("#!/usr/bin/env node\n"+fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, format := range []string{MermaidPNG, MermaidSVG} {
		html, err := renderMermaid(confineCtx(t), "flowchart TD\n  A --> B\n", format)
		if err == nil {
			t.Fatalf("%s: took the output of an mmdc the hook did not confine: %.80q", format, html)
		}
		if !strings.Contains(err.Error(), "not confined") {
			t.Fatalf("%s: err = %v, want a refusal naming the renderer as not confined", format, err)
		}
	}
}

// TestMermaid_ConfinedMatchesUnconfinedAndFetchesNothing is the proof for
// 🎯T68 at the renderer. The unconfined mmdc command is the one f926199
// runs. A diagram with no outside image must match it. Every way Mermaid
// 12 names an image or a fetch must leave the probe server at zero
// requests and must not draw the probe's pure-blue pixels.
func TestMermaid_ConfinedMatchesUnconfinedAndFetchesNothing(t *testing.T) {
	testdeps.Need(t, "mmdc", "node")

	var hits atomic.Int32
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(solidPNG(t, 40, 40, color.RGBA{R: 0, G: 0, B: 255, A: 255}))
	}))
	t.Cleanup(probe.Close)

	secretDir := t.TempDir()
	secretPNG := filepath.Join(secretDir, "secret.png")
	secret := solidPNG(t, 40, 40, color.RGBA{R: 0, G: 0, B: 255, A: 255})
	if err := os.WriteFile(secretPNG, secret, 0o644); err != nil {
		t.Fatal(err)
	}
	// A path inside a directory standing in for the opened folder. Relative
	// paths never resolved against the document, and an absolute path worked
	// only through this hole, so it is left out with the rest.
	inside := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(inside, secret, 0o644); err != nil {
		t.Fatal(err)
	}

	plain := "flowchart TD\n  A[Hello] --> B[World]\n"
	t.Run("plain png matches unconfined", func(t *testing.T) {
		pngEqual(t, confinedPNG(t, plain), unconfinedMermaid(t, plain, MermaidPNG))
	})
	t.Run("plain svg matches unconfined", func(t *testing.T) {
		got := confinedSVG(t, plain)
		want := embedMermaidSVG(unconfinedMermaid(t, plain, MermaidSVG))
		if got != want {
			t.Fatalf("svg differs: confined %d bytes, unconfined %d bytes", len(got), len(want))
		}
	})
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(secret)
	dataDiagram := imageShape(dataURL)
	t.Run("data image matches unconfined", func(t *testing.T) {
		pngEqual(t, confinedPNG(t, dataDiagram), unconfinedMermaid(t, dataDiagram, MermaidPNG))
	})
	c4 := "C4Context\n  Person(u, \"User\")\n  System(s, \"Vellum\")\n  Rel(u, s, \"converts\")\n"
	t.Run("c4 matches unconfined", func(t *testing.T) {
		pngEqual(t, confinedPNG(t, c4), unconfinedMermaid(t, c4, MermaidPNG))
	})

	_, caller, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	sample, err := os.ReadFile(filepath.Join(filepath.Dir(caller), "..", "test", "sample.md"))
	if err != nil {
		t.Fatal(err)
	}
	pre := newMermaidPreprocessor(MermaidPNG)
	pre.Extract(string(sample))
	if len(pre.diagrams) == 0 {
		t.Fatal("test/sample.md has no mermaid diagrams")
	}
	for i, d := range pre.diagrams {
		src := d.source
		t.Run(fmt.Sprintf("sample.md diagram %d matches unconfined", i+1), func(t *testing.T) {
			pngEqual(t, confinedPNG(t, src), unconfinedMermaid(t, src, MermaidPNG))
		})
	}

	fileURL := "file://" + secretPNG
	if runtime.GOOS == "windows" && !strings.HasPrefix(secretPNG, "/") {
		fileURL = "file:///" + filepath.ToSlash(secretPNG)
	}
	// A diagram names a file the way a file: page would: on Windows a
	// path's backslashes are escapes in Mermaid's YAML and a leading
	// drive letter reads as a URL scheme, so the path is spelled /C:/….
	secretRef, insideRef := secretPNG, inside
	if runtime.GOOS == "windows" {
		secretRef = "/" + filepath.ToSlash(secretPNG)
		insideRef = "/" + filepath.ToSlash(inside)
	}
	climb := strings.Repeat("../", 24) + strings.TrimPrefix(secretRef, "/")
	// quiet: nothing is fetched and no probe pixel is drawn. Mermaid may
	// drop the reference, or the renderer may answer it with an empty image.
	// keep: the diagram still renders, and nothing is fetched.
	const (
		quiet = "quiet"
		keep  = "keep"
	)
	cases := []struct {
		name string
		src  string
		mode string
	}{
		{"image shape absolute path", imageShape(secretRef), keep},
		{"image shape climbs out of mmdc", imageShape(climb), keep},
		{"image shape protocol-relative", imageShape("//localhost" + secretRef), keep},
		{"image shape file url", imageShape(fileURL), keep},
		{"image shape http", imageShape(probe.URL + "/secret.png"), keep},
		{"html img file", htmlImg(secretRef), quiet},
		{"html img http", htmlImg(probe.URL + "/secret.png"), quiet},
		{"html img inside the opened folder", htmlImg(insideRef), quiet},
		{"image shape inside the opened folder", imageShape(insideRef), quiet},
		{"init loose, img, script, font and stylesheet", looseProbe(probe.URL + "/x"), quiet},
		{"classDef url file", classDef(secretRef), quiet},
		{"style url http", styleURL(probe.URL + "/secret.png"), quiet},
		{"svg image in a label", svgImage(secretRef), quiet},
		{"click http", "flowchart TD\n  A[Hello] --> B[World]\n  click A \"" + probe.URL + "/go\" \"open\"\n", keep},
		{"click file", "flowchart TD\n  A[Hello] --> B[World]\n  click A \"" + fileURL + "\" \"open\"\n", keep},
		{"icon with no pack", "flowchart TD\n  A@{ icon: \"fa:user\", form: \"square\", label: \"User\" }\n", keep},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := hits.Load()
			html, err := renderMermaid(confineCtx(t), tc.src, MermaidPNG)
			if hits.Load() != before {
				t.Fatalf("the diagram made %d request(s) to the probe", hits.Load()-before)
			}
			if tc.mode == keep && err != nil {
				t.Fatal(err)
			}
			if err != nil {
				return
			}
			if n := pureBlueHTML(t, html); n > 0 {
				t.Fatalf("%d pure blue pixels", n)
			}
		})
	}
	if hits.Load() != 0 {
		t.Fatalf("probe saw %d requests", hits.Load())
	}

	// These routes do draw the file when mmdc is unconfined (the f926199
	// command). The confined runs above must stay at zero blue, so the
	// difference is the hook and not a diagram Mermaid never paints.
	t.Run("unconfined html img draws the file", func(t *testing.T) {
		if n := pureBlue(t, unconfinedMermaid(t, htmlImg(secretRef), MermaidPNG)); n == 0 {
			t.Fatal("unconfined mmdc drew no pure blue pixels from the html img")
		}
	})
	t.Run("unconfined image shape draws the file", func(t *testing.T) {
		if n := pureBlue(t, unconfinedMermaid(t, imageShape(secretRef), MermaidPNG)); n == 0 {
			t.Fatal("unconfined mmdc drew no pure blue pixels from the image shape")
		}
	})
}

func imageShape(ref string) string {
	return "flowchart TD\n" +
		`  A@{ img: "` + ref + `", label: "a", pos: "t", w: 60, h: 60, constraint: "on" }` + "\n  A --> B\n"
}

func htmlImg(ref string) string {
	return "flowchart TD\n  A[\"<img src='" + ref + "' width=60 height=60>\"] --> B\n"
}

func looseProbe(ref string) string {
	return "%%{init: {\"securityLevel\": \"loose\", \"theme\": \"base\", \"themeCSS\": \"@import url('" + ref + "'); @font-face { font-family: VellumProbe; src: url('" + ref + "'); } .node rect { fill: url('" + ref + "'); }\"}}%%\n" +
		"flowchart TD\n  A[\"<img src='" + ref + "' width=40 height=40><script>fetch('" + ref + "')</script>\"] --> B\n"
}

func classDef(ref string) string {
	return "flowchart TD\n  A[Hello]\n  classDef evil fill:url(\"" + ref + "\"),stroke:#333\n  class A evil\n"
}

func styleURL(ref string) string {
	return "flowchart TD\n  A[Hello]\n  style A fill:url(\"" + ref + "\")\n"
}

func svgImage(ref string) string {
	return "flowchart TD\n  A[\"<svg xmlns='http://www.w3.org/2000/svg' width='40' height='40'><image href='" + ref + "' width='40' height='40'/></svg>\"] --> B\n"
}

func confineCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func confinedPNG(t *testing.T, src string) []byte {
	t.Helper()
	html, err := renderMermaid(confineCtx(t), src, MermaidPNG)
	if err != nil {
		t.Fatal(err)
	}
	return pngFromHTML(t, html)
}

func confinedSVG(t *testing.T, src string) string {
	t.Helper()
	svg, err := renderMermaid(confineCtx(t), src, MermaidSVG)
	if err != nil {
		t.Fatal(err)
	}
	return svg
}

func pngFromHTML(t *testing.T, html string) []byte {
	t.Helper()
	const marker = `src="data:image/png;base64,`
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("no png in %q", html)
	}
	rest := html[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		t.Fatal("unterminated png")
	}
	raw, err := base64.StdEncoding.DecodeString(rest[:j])
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func pureBlueHTML(t *testing.T, html string) int {
	t.Helper()
	if !strings.Contains(html, "data:image/png;base64,") {
		return 0
	}
	return pureBlue(t, pngFromHTML(t, html))
}

func pureBlue(t *testing.T, raw []byte) int {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r == 0 && g == 0 && bl == 0xffff {
				n++
			}
		}
	}
	return n
}

func pngEqual(t *testing.T, got, want []byte) {
	t.Helper()
	a, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	b, err := png.Decode(bytes.NewReader(want))
	if err != nil {
		t.Fatal(err)
	}
	if a.Bounds() != b.Bounds() {
		t.Fatalf("bounds %v, unconfined %v", a.Bounds(), b.Bounds())
	}
	box := a.Bounds()
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				t.Fatalf("pixel %d,%d confined %v unconfined %v", x, y, a.At(x, y), b.At(x, y))
			}
		}
	}
}

func unconfinedMermaid(t *testing.T, src, format string) []byte {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mmd")
	out := filepath.Join(dir, "out."+format)
	if err := os.WriteFile(in, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"-i", in, "-o", out, "-e", format, "--quiet"}
	if format == MermaidPNG {
		args = append(args, "-s", "2")
	} else {
		// The same id renderMermaid asks for, so the two differ only by
		// the confinement.
		args = append(args, "--svgId", mermaidSVGID(src))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "mmdc", args...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unconfined mmdc: %v\n%s", err, b)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
