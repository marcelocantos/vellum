// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package clipboard

import (
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/pandoc"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

// TestPandocRichText covers the fallback conversion on its own, without
// a pasteboard. TestWriteSurvivesUnreachableTextKitAgent needs a real
// pasteboard and so is skipped on hosted runners; this one runs
// everywhere, which is what keeps the fallback from rotting in CI
// (🎯T23).
func TestPandocRichText(t *testing.T) {
	testdeps.Need(t, pandoc.Binary)

	const marker = "vellum-pandoc-fallback-marker"
	html := "<html><head><style>body{color:red}</style></head><body><p>Hello <b>" +
		marker + "</b>.</p></body></html>"

	rtf, plain, err := pandocRichText(html)
	if err != nil {
		t.Fatalf("pandocRichText: %v", err)
	}

	rtfStr := string(rtf)
	if !strings.HasPrefix(rtfStr, `{\rtf`) {
		t.Errorf("RTF missing the {\\rtf signature; got %q", firstN(rtf, 60))
	}
	if !strings.Contains(rtfStr, marker) {
		t.Errorf("RTF does not contain marker %q; got %q", marker, firstN(rtf, 200))
	}
	// Structure, not merely text: the fallback earns its place only if
	// the bold run survives.
	if bold := strings.LastIndex(rtfStr[:strings.Index(rtfStr, marker)+1], `\b`); bold < 0 {
		t.Errorf("RTF lost the bold run around %q; got %q", marker, rtfStr)
	}
	// CSS is the documented casualty of this route. Assert the <style>
	// block is not leaked as literal text — dropping the styling is the
	// trade; pasting the stylesheet would be a bug.
	if strings.Contains(rtfStr, "color:red") {
		t.Errorf("RTF leaked the stylesheet as text; got %q", rtfStr)
	}

	plainStr := string(plain)
	if !strings.Contains(plainStr, marker) {
		t.Errorf("plain text does not contain marker %q; got %q", marker, plainStr)
	}
	if strings.Contains(plainStr, `{\rtf`) {
		t.Errorf("plain text leaked raw RTF source; got %q", firstN(plain, 80))
	}
}
