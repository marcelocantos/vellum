// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInlineResources_NilArgsDropEverythingButData(t *testing.T) {
	const page = `<!DOCTYPE html><html><head></head><body>` +
		`<img src="pic.png" alt="rel"><img src="https://example.com/a.png" alt="remote">` +
		`<img src="data:image/png;base64,AAAA" alt="data"></body></html>`
	out, err := InlineResources(context.Background(), page, "/docs", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "pic.png") || strings.Contains(out, "example.com") {
		t.Fatalf("a reference with nowhere to come from survived:\n%s", out)
	}
	if !strings.Contains(out, "data:image/png;base64,AAAA") {
		t.Fatalf("data: URL was removed:\n%s", out)
	}
}

func TestInlineResources_InlinesOnlyWhatReadAndFetchAllow(t *testing.T) {
	base := t.TempDir()
	inside := filepath.Join(base, "pic.png")
	if err := os.WriteFile(inside, []byte("INSIDE"), 0o644); err != nil {
		t.Fatal(err)
	}
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.png")
	if err := os.WriteFile(outside, []byte("SECRET-BYTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	page := `<!DOCTYPE html><html><head>` +
		`<link rel="stylesheet" href="file:///katex/katex.min.css">` +
		`</head><body>` +
		`<base href="file:///etc/">` +
		`<img src="pic.png" alt="in">` +
		`<img src="pic.png" alt="again">` +
		`<img src="../secret.png" alt="rel">` +
		`<img src="` + outside + `" alt="abs">` +
		`<img src="file://` + outside + `" alt="file">` +
		`<img src="https://example.com/a.png" alt="remote">` +
		`<img src="https://example.com/refused.png" alt="refused">` +
		`<img src="ftp://example.com/x.png" alt="ftp">` +
		`<a href="../secret.png">link</a>` +
		`<svg><image href="../secret.png"/><use xlink:href="../secret.png#x"/></svg>` +
		`<object data="../secret.png"></object>` +
		`<style>body{background:url("../secret.png")}@import "\2e\2e/secret.png";p{background:url("https://example.com/b.png")}</style>` +
		`<div style="background:url(\2e\2e/secret.png)"></div>` +
		`</body></html>`

	var reads, fetches []string
	out, err := InlineResources(context.Background(), page, base, &InlineArgs{
		Read: func(p string) ([]byte, error) {
			reads = append(reads, p)
			if filepath.Clean(p) != inside {
				return nil, os.ErrPermission
			}
			return os.ReadFile(p)
		},
		Fetch: func(_ context.Context, u string) ([]byte, string, error) {
			fetches = append(fetches, u)
			if strings.HasSuffix(u, "refused.png") {
				return nil, "", errors.New("refused")
			}
			return []byte("REMOTE:" + u), "image/png; charset=binary", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	inData := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("INSIDE"))
	if strings.Count(out, inData) != 2 {
		t.Fatalf("inside image was not inlined twice:\n%s", out)
	}
	if n := strings.Count(strings.Join(reads, "\n"), inside); n != 1 {
		t.Fatalf("inside image read %d times, want 1 (memoised)", n)
	}
	remote := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("REMOTE:https://example.com/a.png"))
	if !strings.Contains(out, remote) {
		t.Fatalf("remote image was not inlined:\n%s", out)
	}
	if !strings.Contains(out, base64.StdEncoding.EncodeToString([]byte("REMOTE:https://example.com/b.png"))) {
		t.Fatalf("remote css url was not inlined:\n%s", out)
	}
	if strings.Contains(out, "example.com") || strings.Contains(out, "ftp:") {
		t.Fatalf("a remote reference survived as a URL:\n%s", out)
	}
	if !strings.Contains(out, "file:///katex/katex.min.css") {
		t.Fatalf("head was rewritten:\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "<base") {
		t.Fatalf("base element survived:\n%s", out)
	}
	if strings.Contains(out, outside) || strings.Contains(out, "SECRET") {
		t.Fatalf("outside path survived:\n%s", out)
	}
	if strings.Contains(out, `\2e`) {
		t.Fatalf("css escape survived:\n%s", out)
	}
	// A link is not a resource the PDF engine fetches. Every other
	// mention of the outside file has to be gone.
	const link = `href="../secret.png"`
	if strings.Count(out, "secret.png") != 1 || !strings.Contains(out, link) {
		t.Fatalf("outside reference was not confined to the document link:\n%s", out)
	}
	for _, f := range fetches {
		if !strings.HasPrefix(f, "https://example.com/") {
			t.Fatalf("fetched %q", f)
		}
	}
}

func TestInlineResources_LinkedStylesheetDoesNotReadOutside(t *testing.T) {
	root := t.TempDir()
	doc := filepath.Join(root, "doc")
	if err := os.Mkdir(doc, 0o755); err != nil {
		t.Fatal(err)
	}
	css := []byte(`body{background:url("../secret.png")}p{background:url("dot.png")}b{background:url("https://cdn.example/a.png")}`)
	if err := os.WriteFile(filepath.Join(doc, "site.css"), css, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(doc, "dot.png"), []byte("DOT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.png"), []byte("SECRET-BYTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := newDocFiles(doc)
	var fetches []string
	page := `<!DOCTYPE html><html><head></head><body><link rel="stylesheet" href="site.css"></body></html>`
	out, err := InlineResources(context.Background(), page, doc, &InlineArgs{
		Read: files.read,
		Fetch: func(_ context.Context, u string) ([]byte, string, error) {
			fetches = append(fetches, u)
			return []byte("REMOTE"), "image/png", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	const marker = `href="data:text/css;base64,`
	i := strings.Index(out, marker)
	if i < 0 {
		t.Fatalf("stylesheet was not inlined:\n%s", out)
	}
	rest := out[i+len(marker):]
	j := strings.Index(rest, `"`)
	raw, err := base64.StdEncoding.DecodeString(rest[:j])
	if err != nil {
		t.Fatal(err)
	}
	sheet := string(raw)
	if strings.Contains(sheet, "secret") || strings.Contains(sheet, "SECRET") {
		t.Fatalf("stylesheet still names the outside file:\n%s", sheet)
	}
	if !strings.Contains(sheet, base64.StdEncoding.EncodeToString([]byte("DOT"))) {
		t.Fatalf("stylesheet lost its own image:\n%s", sheet)
	}
	if len(fetches) != 1 || fetches[0] != "https://cdn.example/a.png" {
		t.Fatalf("fetches %v", fetches)
	}
	if !strings.Contains(sheet, base64.StdEncoding.EncodeToString([]byte("REMOTE"))) {
		t.Fatalf("stylesheet lost its remote image:\n%s", sheet)
	}
}

// TestInlineResources_DriveLetterPathIsALocalFile is the Windows failure of
// TestInlineResources_LinkedStylesheetDoesNotReadOutside: a stylesheet
// url() resolved against the stylesheet's directory is an absolute path
// with a drive letter, and url.Parse takes that letter as a scheme.
func TestInlineResources_DriveLetterPathIsALocalFile(t *testing.T) {
	const dot = "DOT"
	want := base64.StdEncoding.EncodeToString([]byte(dot))
	// An img src reaches decide unchanged. A CSS url() does not: a
	// backslash there is an escape, and the Windows failure is the path
	// resolveCSSRef already built, which decide sees as a Go string.
	for _, drive := range []string{`C:\doc\dot.png`, `C:/doc/dot.png`} {
		t.Run(drive, func(t *testing.T) {
			var reads []string
			page := `<!DOCTYPE html><html><head></head><body><img src="` + drive + `"></body></html>`
			out, err := InlineResources(context.Background(), page, t.TempDir(), &InlineArgs{
				Read: func(abs string) ([]byte, error) {
					reads = append(reads, abs)
					if filepath.Clean(abs) != filepath.Clean(drive) {
						return nil, errors.New("unexpected path")
					}
					return []byte(dot), nil
				},
				Fetch: func(context.Context, string) ([]byte, string, error) {
					t.Fatal("drive path was fetched as a URL")
					return nil, "", nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, want) {
				t.Fatalf("drive path was not inlined (reads %v):\n%s", reads, out)
			}
		})
	}
	// The letter is a path only with a separator. C:name stays dropped,
	// and a real URL is not read as a file.
	page := `<!DOCTYPE html><html><head></head><body>` +
		`<style>p{background:url("C:not-a-path")}b{background:url("https://cdn.example/a.png")}</style>` +
		`</body></html>`
	var fetched []string
	out, err := InlineResources(context.Background(), page, t.TempDir(), &InlineArgs{
		Read: func(string) ([]byte, error) {
			t.Fatal("a non-path was read as a file")
			return nil, nil
		},
		Fetch: func(_ context.Context, u string) ([]byte, string, error) {
			fetched = append(fetched, u)
			return []byte("REMOTE"), "image/png", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "not-a-path") {
		t.Fatalf("drive-relative name survived:\n%s", out)
	}
	if len(fetched) != 1 || fetched[0] != "https://cdn.example/a.png" {
		t.Fatalf("fetches %v", fetched)
	}
}

func TestDocFiles_DrivePathOutsideTheFolderIsRefused(t *testing.T) {
	files := newDocFiles(t.TempDir())
	_, err := files.read(`C:\doc\dot.png`)
	if !errors.Is(err, errOutsideDocument) {
		t.Fatalf("read = %v, want outside the document folder", err)
	}
}

func TestDataURL_StripsParameters(t *testing.T) {
	if got := dataURL("Image/SVG+XML; charset=utf-8", []byte("<svg/>")); got != "data:image/svg+xml;base64,PHN2Zy8+" {
		t.Fatalf("dataURL = %q", got)
	}
	if got := dataURL("image/png,evil", []byte("x")); !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("dataURL = %q", got)
	}
}

func TestKaTeXInlineHead_FetchesNothing(t *testing.T) {
	head, err := katexInlineHead()
	if err != nil {
		t.Fatal(err)
	}
	refs, err := katexFontRefs()
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(head, "url(\"data:font/woff2;base64,"); n < len(refs) {
		t.Fatalf("inlined %d fonts, want at least %d", n, len(refs))
	}
	for _, r := range refs {
		if strings.Contains(head, filepath.Base(r)) {
			t.Fatalf("font %s is still named by path", r)
		}
	}
}
