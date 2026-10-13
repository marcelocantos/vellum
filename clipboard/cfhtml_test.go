// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package clipboard

import (
	"fmt"
	"strings"
	"testing"
)

func TestCFHTMLRoundTrip(t *testing.T) {
	frag := "<p>héllo <b>wörld</b></p>"
	enc := encodeCFHTML(frag)
	if !strings.HasPrefix(string(enc), "Version:0.9\r\n") {
		t.Fatalf("header: %q", firstN(enc, 40))
	}
	doc := string(decodeCFHTML(enc))
	if !strings.HasPrefix(doc, "<html>") || !strings.HasSuffix(doc, "</html>") || !strings.Contains(doc, frag) {
		t.Fatalf("decoded %q", doc)
	}
	off := cfHTMLOffsetRe.FindAllStringSubmatch(string(enc), -1)
	if len(off) != 4 {
		t.Fatalf("offsets %v", off)
	}
}

func TestDecodeCFHTMLFallbacks(t *testing.T) {
	head := "Version:0.9\r\nStartHTML:-1\r\nEndHTML:-1\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n"
	n := len(fmt.Sprintf(head, 0, 0))
	raw := fmt.Sprintf(head, n, n+len("<p>x</p>")) + "<p>x</p>"
	if got := string(decodeCFHTML([]byte(raw))); got != "<p>x</p>" {
		t.Fatalf("fragment fallback: %q", got)
	}
	if got := string(decodeCFHTML([]byte("<p>plain</p>"))); got != "<p>plain</p>" {
		t.Fatalf("raw fallback: %q", got)
	}
}
