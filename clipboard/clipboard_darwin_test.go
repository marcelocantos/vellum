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
