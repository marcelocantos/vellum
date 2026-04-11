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

// TestConvert_EndToEnd exercises the full Markdown → PDF pipeline. It is
// gated on Prince being on PATH so CI environments without Prince stay
// green.
func TestConvert_EndToEnd(t *testing.T) {
	if _, err := exec.LookPath("prince"); err != nil {
		t.Skip("prince not on PATH")
	}

	dir := t.TempDir()
	in := filepath.Join(dir, "doc.md")
	out := filepath.Join(dir, "doc.pdf")

	// Content is deliberately trivial: no math, no mermaid — so this test
	// only depends on Prince, not Node+katex or mmdc.
	md := "# Hello Vellum\n\nHello **world** from the integration test.\n"
	if err := os.WriteFile(in, []byte(md), 0o644); err != nil {
		t.Fatalf("writing input: %v", err)
	}

	if err := Convert(context.Background(), in, out, nil); err != nil {
		t.Fatalf("Convert: %v", err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("output PDF is empty")
	}

	// Magic bytes check: every PDF starts with "%PDF-".
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

	// Optional: if pdftotext is available, confirm the expected text is
	// actually in the rendered PDF. Skip gracefully when absent.
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
}
