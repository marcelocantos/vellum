// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/testdeps"
)

const missingWording = "not found on PATH"

// TestPreflight pins which binaries each request shape demands up front
// (🎯T30.5): PDF sinks need RequiredDeps, pandoc only when a requested
// format goes through it, and nothing at all for Markdown → HTML/text.
func TestPreflight(t *testing.T) {
	md := inputItem{path: filepath.Join("dir", "doc.md")}
	docx := inputItem{path: filepath.Join("dir", "doc.docx")}
	pdf := inputItem{path: filepath.Join("dir", "doc.pdf")}
	file := func(f string) Endpoint { return Endpoint{Media: MediaFile, Format: f} }
	cases := []struct {
		name    string
		stubs   []string
		backend string
		from    Endpoint
		in      inputItem
		to      Endpoint
		want    []string // substrings of the error; nil means no error
		notWant []string
	}{
		{"md→pdf without renderer", []string{"node", "mmdc"}, "", file(""), md, file(FormatPDF),
			[]string{`required dependency "weasyprint" ` + missingWording}, []string{"pandoc"}},
		{"md→pdf inferred without renderer", []string{"node", "mmdc"}, "", file(""), md, file(""),
			[]string{`"weasyprint"`}, nil},
		{"md→pdf all present, no pandoc", []string{"weasyprint", "node", "mmdc"}, "", file(""), md, file(FormatPDF),
			nil, nil},
		{"md→pdf prince backend", []string{"weasyprint", "node", "mmdc"}, BackendPrince, file(""), md, file(FormatPDF),
			[]string{`"prince"`}, []string{"weasyprint"}},
		{"md→pdf nothing on PATH lists all", nil, "", file(""), md, file(FormatPDF),
			[]string{"3 required dependencies " + missingWording, "weasyprint", "node", "mmdc"}, []string{"pandoc"}},
		{"md→html needs nothing", nil, "", file(""), md, file(FormatHTML), nil, nil},
		{"md→content needs nothing", nil, "", file(""), md, Endpoint{Media: MediaContent}, nil, nil},
		{"content→clipboard needs nothing", nil, "", Endpoint{Media: MediaContent}, inputItem{content: "x"},
			Endpoint{Media: MediaClipboard}, nil, nil},
		{"md→pptx needs pandoc only", nil, "", file(""), md, file(FormatPPTX),
			[]string{`required dependency "pandoc" ` + missingWording}, []string{"weasyprint", "mmdc"}},
		{"docx→md needs pandoc", nil, "", file(""), docx, file(""),
			[]string{`"pandoc"`}, []string{"weasyprint"}},
		{"docx→pdf without renderer", []string{"pandoc", "node", "mmdc"}, "", file(""), docx, file(FormatPDF),
			[]string{`"weasyprint"`}, []string{`"pandoc"`}},
		{"pdf import needs no pandoc", nil, "", file(""), pdf, file(""), nil, nil},
		{"clipboard→file infers markdown", nil, "", Endpoint{Media: MediaClipboard}, inputItem{name: "clipboard"},
			Endpoint{Media: MediaFile, Path: "out.md"}, nil, nil},
		{"pdf on a text sink is left to checkDisallowed", nil, "", file(""), md,
			Endpoint{Media: MediaContent, Format: FormatPDF}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testdeps.StubPath(t, tc.stubs...)
			err := preflight(tc.from, tc.to, []inputItem{tc.in}, tc.backend)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("preflight = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("preflight = nil, want error containing %q", tc.want)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
			for _, nw := range tc.notWant {
				if strings.Contains(err.Error(), nw) {
					t.Errorf("error %q should not mention %q", err, nw)
				}
			}
		})
	}
}

// TestRun_PDFFailsFastWithoutBackend is the ENT-005 oracle at the Run
// level: with the renderer missing, Run reports the RequiredDeps
// wording, writes nothing, and runs no converter first — the stub
// pandoc on PATH would fail with an exec error if the DOCX import ran.
func TestRun_PDFFailsFastWithoutBackend(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(mdPath, []byte("Hello **world**.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	docxPath, err := filepath.Abs(filepath.Join("testdata", "corpus", "styled", "styled.docx"))
	if err != nil {
		t.Fatal(err)
	}
	testdeps.StubPath(t, "pandoc", "node", "mmdc")

	for _, in := range []string{mdPath, docxPath} {
		out := filepath.Join(dir, strings.TrimSuffix(filepath.Base(in), filepath.Ext(in))+".pdf")
		res, err := Run(context.Background(), &Request{
			From: Endpoint{Media: MediaFile, Path: in},
			To:   Endpoint{Media: MediaFile, Path: out, Format: FormatPDF},
		})
		if err == nil || !strings.Contains(err.Error(), `required dependency "weasyprint" `+missingWording) {
			t.Fatalf("%s: Run err = %v, want the RequiredDeps wording for weasyprint", filepath.Base(in), err)
		}
		if res != nil {
			t.Errorf("%s: result = %+v, want nil on a preflight failure", filepath.Base(in), res)
		}
		if _, statErr := os.Stat(out); statErr == nil {
			t.Errorf("%s: %s was written despite the missing renderer", filepath.Base(in), out)
		}
	}
}

// TestRun_FilesSugarChecksOnce: a batch with a missing renderer fails
// once, up front, not with the same error repeated per file.
func TestRun_FilesSugarChecksOnce(t *testing.T) {
	dir := t.TempDir()
	var files []FilePair
	for _, n := range []string{"a.md", "b.md"} {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("# "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, FilePair{Input: p})
	}
	testdeps.StubPath(t)
	_, err := Run(context.Background(), &Request{Files: files})
	if err == nil {
		t.Fatal("Run with no renderer on PATH succeeded")
	}
	if n := strings.Count(err.Error(), missingWording); n != 1 {
		t.Errorf("%q appears %d times in %q, want once", missingWording, n, err)
	}
}
