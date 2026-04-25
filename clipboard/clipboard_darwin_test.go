// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package clipboard

import (
	"bytes"
	"strings"
	"testing"
)

// macOS pasteboard UTI constants. NSPasteboard exposes these as
// NSPasteboardType* constants; we use the same string values the
// system maps them to.
const (
	utiRTF   = "public.rtf"
	utiHTML  = "public.html"
	utiPlain = "public.utf8-plain-text"
)

// TestWriteRoundTrip exercises the macOS NSPasteboard backend end-to-end:
// writes a known HTML fragment, reads each representation back as raw
// pasteboard data via readPasteboardData, and asserts that:
//   - the RTF payload is well-formed (starts with the `{\rtf` signature)
//     and contains the marker word — proving HTML→RTF conversion ran
//     and the data was placed under public.rtf
//   - the HTML payload is present under public.html
//   - the plain-text payload contains the marker and does NOT leak raw
//     RTF source — the failure mode the textutil+osascript path produces
//     when only RTF is set and apps fall back to plain text
func TestWriteRoundTrip(t *testing.T) {
	const marker = "vellum-clipboard-roundtrip-marker"
	html := "<html><body><p><b>" + marker + "</b></p></body></html>"

	if err := Write(Payload{HTML: html}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	rtf := readPasteboardData(utiRTF)
	if !bytes.HasPrefix(rtf, []byte(`{\rtf`)) {
		t.Errorf("RTF payload missing `{\\rtf` signature; got %q", firstN(rtf, 40))
	}
	if !bytes.Contains(rtf, []byte(marker)) {
		t.Errorf("RTF payload does not contain marker %q", marker)
	}

	htmlData := readPasteboardData(utiHTML)
	if len(htmlData) == 0 {
		t.Error("HTML payload missing from pasteboard")
	}

	plain := readPasteboardData(utiPlain)
	if !bytes.Contains(plain, []byte(marker)) {
		t.Errorf("plain payload does not contain marker %q; got %q", marker, plain)
	}
	if strings.Contains(string(plain), `{\rtf`) {
		t.Errorf("plain payload leaked raw RTF source; got %q", firstN(plain, 80))
	}
}

// TestWriteFragmentsHTMLAndStripsLineSeparators covers the two
// regressions that surfaced from the first round of paste-target
// testing:
//   - Slack rejects a full HTML document with <head><style>; the
//     pasteboard's public.html rep must be a body fragment.
//   - VS Code (and other editors) flag U+2028 LINE SEPARATOR / U+2029
//     PARAGRAPH SEPARATOR as "unusual line terminators". The plain-text
//     rep must use ordinary U+000A newlines.
func TestWriteFragmentsHTMLAndStripsLineSeparators(t *testing.T) {
	const marker = "vellum-fragment-marker"
	full := `<!DOCTYPE html><html><head><meta charset="utf-8"><style>body{color:red}</style></head><body><p>` + marker + `</p><p>second paragraph</p></body></html>`

	if err := Write(Payload{HTML: full}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	htmlData := readPasteboardData(utiHTML)
	htmlStr := string(htmlData)
	if !strings.Contains(htmlStr, marker) {
		t.Errorf("HTML rep missing marker; got %q", htmlStr)
	}
	for _, forbidden := range []string{"<head>", "<style>", "<!DOCTYPE", "<html>"} {
		if strings.Contains(htmlStr, forbidden) {
			t.Errorf("HTML rep should be a body fragment, but contains %q; got %q", forbidden, htmlStr)
		}
	}

	plain := string(readPasteboardData(utiPlain))
	if strings.ContainsRune(plain, ' ') {
		t.Errorf("plain-text rep contains U+2028 LINE SEPARATOR; should have been normalised to \\n; got %q", plain)
	}
	if strings.ContainsRune(plain, ' ') {
		t.Errorf("plain-text rep contains U+2029 PARAGRAPH SEPARATOR; should have been normalised to \\n; got %q", plain)
	}
}

func TestWriteEmptyHTMLRejected(t *testing.T) {
	if err := Write(Payload{}); err == nil {
		t.Fatal("expected error for empty payload, got nil")
	}
}

func firstN(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}
