// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/testdeps"
)

// TestPDFWrite_IsOneImplementation is the ENT-004 ratchet: viewer PDF
// (Convert) and the file sink (writeFileOutput) must share the Mermaid
// PNG choice and the backend call. A private copy of either in Convert
// or writeFileOutput is the duplication the target forbids.
func TestPDFWrite_IsOneImplementation(t *testing.T) {
	convertBody := funcBody(t, "convert.go", "Convert")
	pdfCase := pdfCaseBody(t, funcBody(t, "run.go", "writeFileOutput"))
	for _, name := range []string{"ResolveBackend", "backend.Render", "MermaidPNG"} {
		if strings.Contains(convertBody, name) {
			t.Errorf("Convert contains %s; PDF writing is still its own path", name)
		}
		if strings.Contains(pdfCase, name) {
			t.Errorf("writeFileOutput PDF case contains %s; PDF writing is still inline", name)
		}
	}
	pdfBody := funcBody(t, "convert.go", "WritePDF")
	htmlBody := funcBody(t, "convert.go", "renderPDFHTML")
	if !strings.Contains(pdfBody, "ResolveBackend") || !strings.Contains(pdfBody, "backend.Render") {
		t.Error("WritePDF must be the only backend.Render call for PDF")
	}
	if !strings.Contains(htmlBody, "MermaidPNG") {
		t.Error("renderPDFHTML must force Mermaid PNG for every PDF")
	}
	if strings.Contains(pdfBody, "MermaidPNG") {
		t.Error("Mermaid format drifted back into WritePDF")
	}
}

func TestConvert_CreatesOutputDirectoryLikeFileSink(t *testing.T) {
	testdeps.Need(t, "weasyprint")
	dir := t.TempDir()
	in := filepath.Join(dir, "note.md")
	if err := os.WriteFile(in, []byte("# Shared PDF\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "nested", "out", "note.pdf")
	if err := Convert(context.Background(), in, out, &Options{Style: &Style{PDFA: "PDF/A-1b"}}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() == 0 {
		t.Fatal("PDF is empty")
	}
}

// pdfCaseBody is the FormatPDF arm of writeFileOutput. RTF also embeds
// PNG Mermaid, and that is a different sink.
func pdfCaseBody(t *testing.T, fn string) string {
	t.Helper()
	const marker = "toFmt == FormatPDF"
	i := strings.Index(fn, marker)
	if i < 0 {
		t.Fatal("writeFileOutput has no PDF case")
	}
	rest := fn[i:]
	if j := strings.Index(rest, "\n\tcase "); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

func funcBody(t *testing.T, filename, name string) string {
	t.Helper()
	src, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != name || fn.Body == nil {
			continue
		}
		return string(src[fn.Body.Pos()-1 : fn.Body.End()-1])
	}
	t.Fatalf("%s has no function %s", filename, name)
	return ""
}
