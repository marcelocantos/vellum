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
	html := []byte(`<h1>Title</h1>
<p>Body with <strong>bold</strong> and <em>italic</em>.</p>
<ul><li>Alpha</li><li>Beta</li></ul>`)

	md, err := ImportBytes(context.Background(), html, "html")
	if err != nil {
		t.Fatalf("ImportBytes: %v", err)
	}
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
	dir := t.TempDir()
	rtfPath := filepath.Join(dir, "doc.rtf")
	// Minimal RTF with a bold and italic run.
	rtf := `{\rtf1\ansi
{\fonttbl{\f0 Helvetica;}}
\f0\fs28 \b VELLUM_RTF_HEADING\b0\par
Body \b emphasis\b0  and \i slant\i0 .\par
}`
	if err := os.WriteFile(rtfPath, []byte(rtf), 0o644); err != nil {
		t.Fatalf("write rtf: %v", err)
	}

	md, err := ImportFile(context.Background(), rtfPath, "")
	if err != nil {
		t.Fatalf("ImportFile: %v", err)
	}
	for _, want := range []string{"VELLUM_RTF_HEADING", "**emphasis**", "*slant*"} {
		if !strings.Contains(md, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, md)
		}
	}
}

func TestImportBytes_RejectsEmptyFormat(t *testing.T) {
	if _, err := ImportBytes(context.Background(), []byte("anything"), ""); err == nil {
		t.Error("expected error for empty format, got nil")
	}
}

func TestImportBytes_RejectsEmptyInput(t *testing.T) {
	if _, err := ImportBytes(context.Background(), nil, "html"); err == nil {
		t.Error("expected error for empty input, got nil")
	}
}
