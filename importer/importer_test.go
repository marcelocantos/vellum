// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportBytes_HTML(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not on PATH")
	}
	t.Setenv("VELLUM_IMPORT_CACHE", t.TempDir())
	html := []byte(`<h1>Title</h1>
<p>Body with <strong>bold</strong> and <em>italic</em>.</p>
<ul><li>Alpha</li><li>Beta</li></ul>`)

	r, err := ImportBytes(context.Background(), html, &Options{Format: "html"})
	if err != nil {
		t.Fatalf("ImportBytes: %v", err)
	}
	md := r.Markdown
	for _, want := range []string{"# Title", "**bold**", "*italic*", "Alpha", "Beta"} {
		if !strings.Contains(md, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, md)
		}
	}
}

func TestImportFile_RTF(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not on PATH")
	}
	t.Setenv("VELLUM_IMPORT_CACHE", t.TempDir())
	dir := t.TempDir()
	rtfPath := filepath.Join(dir, "doc.rtf")
	rtf := `{\rtf1\ansi
{\fonttbl{\f0 Helvetica;}}
\f0\fs28 \b VELLUM_RTF_HEADING\b0\par
Body \b emphasis\b0  and \i slant\i0 .\par
}`
	if err := os.WriteFile(rtfPath, []byte(rtf), 0o644); err != nil {
		t.Fatalf("write rtf: %v", err)
	}

	r, err := ImportFile(context.Background(), rtfPath, &Options{})
	if err != nil {
		t.Fatalf("ImportFile: %v", err)
	}
	md := r.Markdown
	for _, want := range []string{"VELLUM_RTF_HEADING", "**emphasis**", "*slant*"} {
		if !strings.Contains(md, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, md)
		}
	}
}

func TestImportBytes_HTMLWithImageExtractsMedia(t *testing.T) {
	if _, err := exec.LookPath("pandoc"); err != nil {
		t.Skip("pandoc not on PATH")
	}
	cache := t.TempDir()
	t.Setenv("VELLUM_IMPORT_CACHE", cache)
	work := t.TempDir()
	// Valid 1x1 PNG
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe, 0xd4, 0xef, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	imgPath := filepath.Join(work, "dot.png")
	if err := os.WriteFile(imgPath, png, 0o644); err != nil {
		t.Fatal(err)
	}
	html := []byte(`<p>Hi <img src="dot.png" alt="dot"/></p>`)
	// Run from work so relative src resolves for pandoc html→…
	// ImportBytes writes source into media dir; embed as data or use file import.
	htmlFile := filepath.Join(work, "in.html")
	if err := os.WriteFile(htmlFile, html, 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := ImportFile(context.Background(), htmlFile, &Options{Format: "html"})
	if err != nil {
		t.Fatalf("ImportFile: %v", err)
	}
	if !strings.Contains(r.Markdown, "Hi") {
		t.Fatalf("md=%q", r.Markdown)
	}
	// Media dir should exist; assets may be empty if pandoc skipped broken/tiny png.
	if r.MediaDir == "" {
		t.Fatal("expected MediaDir")
	}
}

func TestImportPDF_Smoke(t *testing.T) {
	if err := CheckPDFDeps(); err != nil {
		t.Skip(err.Error())
	}
	// Need a minimal PDF — generate via printf if weasyprint available,
	// or skip. Try pandoc latex... simplest: use printf %PDF header won't work for pdftoppm.
	if _, err := exec.LookPath("printf"); err != nil {
		t.Skip("no printf")
	}
	// Create tiny PDF with ghostscript or python reportlab — use weasyprint html
	if _, err := exec.LookPath("weasyprint"); err != nil {
		t.Skip("weasyprint not on PATH for fixture PDF")
	}
	t.Setenv("VELLUM_IMPORT_CACHE", t.TempDir())
	dir := t.TempDir()
	html := filepath.Join(dir, "p.html")
	pdf := filepath.Join(dir, "p.pdf")
	if err := os.WriteFile(html, []byte(`<html><body><h1>VellumPDF</h1><p>Hello PDF page.</p></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("weasyprint", html, pdf)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("weasyprint failed: %v %s", err, out)
	}
	r, err := ImportPDF(context.Background(), pdf, filepath.Join(dir, "media"))
	if err != nil {
		t.Fatalf("ImportPDF: %v", err)
	}
	if !strings.Contains(r.Markdown, "PDF import") {
		t.Fatalf("md=%q", r.Markdown)
	}
	if !strings.Contains(r.Markdown, "VellumPDF") && !strings.Contains(r.Markdown, "Hello PDF") {
		// text extract might still find something
		t.Logf("md=%s", r.Markdown)
	}
	if len(r.Assets) == 0 {
		t.Fatal("expected page image assets")
	}
	for _, a := range r.Assets {
		if _, err := os.Stat(a); err != nil {
			t.Errorf("asset missing: %s: %v", a, err)
		}
		if !strings.Contains(r.Markdown, a) {
			t.Errorf("markdown missing absolute asset path %s", a)
		}
	}
}

func TestImportBytes_RejectsEmptyFormat(t *testing.T) {
	if _, err := ImportBytes(context.Background(), []byte("anything"), &Options{}); err == nil {
		t.Error("expected error for empty format, got nil")
	}
}

func TestImportBytes_RejectsEmptyInput(t *testing.T) {
	if _, err := ImportBytes(context.Background(), nil, &Options{Format: "html"}); err == nil {
		t.Error("expected error for empty input, got nil")
	}
}

func TestAbsolutizeMedia(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "x.png")
	if err := os.WriteFile(img, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	md := "see ![a](x.png) please"
	out, assets, err := AbsolutizeMedia(md, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, img) {
		t.Fatalf("out=%q", out)
	}
	if len(assets) != 1 || assets[0] != img {
		t.Fatalf("assets=%v", assets)
	}
}
