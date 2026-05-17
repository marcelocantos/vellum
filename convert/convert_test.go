// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// backends returns the set of backends whose underlying binary is present
// on PATH. Tests that hit the renderer should iterate this list so they
// run against each available backend, and skip the per-backend subtest
// when the binary isn't installed (keeps CI green without all engines).
func backends(t *testing.T) []string {
	t.Helper()
	all := []string{BackendWeasyPrint, BackendPrince}
	var ok []string
	for _, b := range all {
		dep := mustBackend(t, b).Dep()
		if _, err := exec.LookPath(dep.Name); err == nil {
			ok = append(ok, b)
		}
	}
	if len(ok) == 0 {
		t.Skip("no renderer backend available on PATH (need weasyprint or prince)")
	}
	return ok
}

func mustBackend(t *testing.T, name string) Backend {
	t.Helper()
	b, err := ResolveBackend(name)
	if err != nil {
		t.Fatalf("ResolveBackend(%q): %v", name, err)
	}
	return b
}

// TestConvert_EndToEnd exercises the full Markdown → PDF pipeline once per
// available backend. Both renderers accept the same HTML input so the test
// body is identical across backends.
func TestConvert_EndToEnd(t *testing.T) {
	for _, backend := range backends(t) {
		t.Run(backend, func(t *testing.T) {
			dir := t.TempDir()
			in := filepath.Join(dir, "doc.md")
			out := filepath.Join(dir, "doc.pdf")

			// Content is deliberately trivial: no math, no mermaid — only
			// the renderer is exercised, not Node+katex or mmdc.
			md := "# Hello Vellum\n\nHello **world** from the integration test.\n"
			if err := os.WriteFile(in, []byte(md), 0o644); err != nil {
				t.Fatalf("writing input: %v", err)
			}

			opts := &Options{Backend: backend}
			if err := Convert(context.Background(), in, out, opts); err != nil {
				t.Fatalf("Convert: %v", err)
			}

			info, err := os.Stat(out)
			if err != nil {
				t.Fatalf("stat output: %v", err)
			}
			if info.Size() == 0 {
				t.Errorf("output PDF is empty")
			}

			f, err := os.Open(out)
			if err != nil {
				t.Fatalf("open output: %v", err)
			}
			defer f.Close()
			head := make([]byte, 5)
			if _, err := f.Read(head); err != nil {
				t.Fatalf("read header: %v", err)
			}
			if string(head) != "%PDF-" {
				t.Errorf("header = %q, want %q", string(head), "%PDF-")
			}

			if _, err := exec.LookPath("pdftotext"); err != nil {
				t.Logf("pdftotext not on PATH; skipping textual content check")
				return
			}
			txt := filepath.Join(dir, "doc.txt")
			cmd := exec.Command("pdftotext", out, txt)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("pdftotext: %v: %s", err, string(out))
			}
			body, err := os.ReadFile(txt)
			if err != nil {
				t.Fatalf("read text: %v", err)
			}
			text := string(body)
			for _, want := range []string{"Hello Vellum", "Hello", "world"} {
				if !strings.Contains(text, want) {
					t.Errorf("extracted text missing %q; got:\n%s", want, text)
				}
			}
		})
	}
}

// TestConvert_RelativeImagePath verifies that a Markdown image reference
// using a path relative to the input file resolves correctly. Without the
// --baseurl / --base-url fix, the renderer would resolve against /tmp
// (where the assembled HTML lives) and silently fail to load the image.
func TestConvert_RelativeImagePath(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not on PATH; cannot verify SVG text extraction")
	}
	for _, backend := range backends(t) {
		t.Run(backend, func(t *testing.T) {
			dir := t.TempDir()
			in := filepath.Join(dir, "doc.md")
			out := filepath.Join(dir, "doc.pdf")
			svg := filepath.Join(dir, "logo.svg")

			const marker = "VELLUM_SVG_MARKER"
			svgBody := `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="200" height="60" viewBox="0 0 200 60">
  <rect width="200" height="60" fill="#eef"/>
  <text x="10" y="40" font-family="sans-serif" font-size="24" fill="black">` + marker + `</text>
</svg>`
			if err := os.WriteFile(svg, []byte(svgBody), 0o644); err != nil {
				t.Fatalf("writing svg: %v", err)
			}

			md := "# Doc\n\n![logo](logo.svg)\n"
			if err := os.WriteFile(in, []byte(md), 0o644); err != nil {
				t.Fatalf("writing markdown: %v", err)
			}

			opts := &Options{Backend: backend}
			if err := Convert(context.Background(), in, out, opts); err != nil {
				t.Fatalf("Convert: %v", err)
			}

			txt := filepath.Join(dir, "doc.txt")
			cmd := exec.Command("pdftotext", out, txt)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("pdftotext: %v: %s", err, string(out))
			}
			body, err := os.ReadFile(txt)
			if err != nil {
				t.Fatalf("read text: %v", err)
			}
			if !strings.Contains(string(body), marker) {
				t.Errorf("SVG text marker %q missing from PDF — relative image did not resolve.\nExtracted text:\n%s", marker, string(body))
			}
		})
	}
}
