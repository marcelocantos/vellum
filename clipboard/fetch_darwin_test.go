// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package clipboard

import (
	"strings"
	"testing"
)

// TestAppKitRichTextFetchesNothing covers the preferred clipboard route:
// NSAttributedString's HTML importer neither fetches the http resources
// nor reads the file: stylesheet the HTML names. Where the importer is
// unavailable (no TextKit agent) it fetches nothing either, so the check
// holds without it.
func TestAppKitRichTextFetchesNothing(t *testing.T) {
	f := newConverterFixture(t)
	rtf, _, err := appKitRichText(f.html)
	if got := f.requests(); len(got) != 0 {
		t.Errorf("AppKit fetched %v", got)
	}
	if err != nil {
		t.Logf("AppKit route unavailable here (%v); it fetched nothing", err)
		return
	}
	if strings.Contains(string(rtf), `\red255\green0\blue0`) {
		t.Errorf("AppKit applied the file: stylesheet:\n%s", rtf)
	}
}
