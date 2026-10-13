// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package clipboard

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/testdeps"
)

func TestWindowsWriteReadRichText(t *testing.T) {
	testdeps.Need(t, "pandoc")
	html := `<html><head><style>p{color:red}</style></head><body><h1>Title</h1><p>Hello <b>bold</b> wörld</p></body></html>`
	rep, err := Write(Payload{HTML: html})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Route != RoutePandoc || rep.Fallback != "" {
		t.Fatalf("report %+v", rep)
	}
	h, err := ReadHTML()
	if err != nil || !strings.Contains(string(h), "<b>bold</b>") || strings.Contains(string(h), "<style>") {
		t.Fatalf("ReadHTML = %q, %v", h, err)
	}
	r, err := ReadRTF()
	if err != nil || !bytes.HasPrefix(r, []byte(`{\rtf`)) || !bytes.Contains(r, []byte("bold")) {
		t.Fatalf("ReadRTF = %q, %v", firstN(r, 80), err)
	}
	txt, err := readClipboard(formatText)
	if err != nil || !strings.Contains(string(txt), "Hello bold wörld") {
		t.Fatalf("text = %q, %v", txt, err)
	}
	data, format, err := ReadImportable()
	if err != nil || format != FormatRTF || len(data) == 0 {
		t.Fatalf("ReadImportable = %d bytes, %q, %v", len(data), format, err)
	}
}

func TestWindowsFileRefsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a b.pdf")
	c := filepath.Join(dir, "ünï.md")
	for _, p := range []string{a, c} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteFileRefs(FileRefPayload{Paths: []string{a, c}}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFileRefs()
	if err != nil || !reflect.DeepEqual(got, []string{a, c}) {
		t.Fatalf("ReadFileRefs = %v, %v", got, err)
	}
	// A file-ref write replaces the rich formats.
	if h, _ := ReadHTML(); h != nil {
		t.Fatalf("stale HTML after file-ref write: %q", firstN(h, 60))
	}
}

func TestWindowsReadPDF(t *testing.T) {
	pdf := []byte("%PDF-1.7\n%fake\n")
	if err := writeFormats([]clipItem{{fmtPDF, pdf}}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPDF()
	if err != nil || !bytes.HasPrefix(got, pdf) {
		t.Fatalf("ReadPDF = %q, %v", got, err)
	}
	data, format, err := ReadImportable()
	if err != nil || format != FormatPDF || !bytes.HasPrefix(data, pdf) {
		t.Fatalf("ReadImportable = %q, %q, %v", firstN(data, 20), format, err)
	}
}

func TestParseDropFilesANSI(t *testing.T) {
	b := make([]byte, dropFilesSize)
	b[0] = dropFilesSize
	b = append(b, []byte("C:\\x.txt\x00C:\\y.txt\x00\x00")...)
	if got := parseDropFiles(b); !reflect.DeepEqual(got, []string{`C:\x.txt`, `C:\y.txt`}) {
		t.Fatalf("got %v", got)
	}
}
