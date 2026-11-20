// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsolateLocalFiles_NilReaderKeepsTheDocumentBase(t *testing.T) {
	const page = `<!DOCTYPE html><html><head></head><body><img src="pic.png"></body></html>`
	out, dir, err := IsolateLocalFiles(page, "/docs", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != page || dir != "/docs" {
		t.Fatalf("nil reader changed the page or the base: %q %q", out, dir)
	}
}

func TestIsolateLocalFiles_CopiesOnlyWhatTheReaderAllows(t *testing.T) {
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
		`<img src="../secret.png" alt="rel">` +
		`<img src="` + outside + `" alt="abs">` +
		`<img src="file://` + outside + `" alt="file">` +
		`<img src="https://example.com/a.png" alt="remote">` +
		`<a href="../secret.png">link</a>` +
		`<style>body{background:url("../secret.png")}@import "\2e\2e/secret.png";p{background:url("https://example.com/b.png")}</style>` +
		`<div style="background:url(\2e\2e/secret.png)"></div>` +
		`</body></html>`

	out, dir, err := IsolateLocalFiles(page, base, func(p string) ([]byte, error) {
		if filepath.Clean(p) != inside {
			return nil, os.ErrPermission
		}
		return os.ReadFile(p)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if dir == base || !strings.HasPrefix(dir, os.TempDir()) {
		t.Fatalf("resource dir %q is not a fresh jail", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("jail has %d files, want the one inside image", len(entries))
	}
	got, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "INSIDE" {
		t.Fatalf("copied %q", got)
	}
	if !strings.Contains(out, "file:///katex/katex.min.css") {
		t.Fatalf("head stylesheet was rewritten:\n%s", out)
	}
	if !strings.Contains(out, "https://example.com/a.png") || !strings.Contains(out, "https://example.com/b.png") {
		t.Fatalf("remote url was rewritten:\n%s", out)
	}
	if strings.Contains(out, "<base") || strings.Contains(strings.ToLower(out), "<base") {
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
	if !strings.Contains(out, entries[0].Name()) {
		t.Fatalf("inside image was not rewritten to %s:\n%s", entries[0].Name(), out)
	}
}
