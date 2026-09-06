// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"fmt"
	"strings"
	"testing"
)

func TestSubstitutePlaceholders(t *testing.T) {
	values := []string{"zero", "one", "ten", "<!--T:0-->"}
	// Index 3 substitutes text shaped like a placeholder; it must be
	// emitted verbatim, not expanded to "zero".
	cases := []struct{ in, want string }{
		{"a<!--T:0-->b<!--T:1-->c", "azerobonec"},
		{"<!--T:2-->", "ten"},
		{"<!--T:3--> and <!--T:0-->", "<!--T:0--> and zero"},
		{"no placeholders", "no placeholders"},
		{"<!--T:-->", "<!--T:-->"},                     // no digits
		{"<!--T:7-->", "<!--T:7-->"},                   // index without a value
		{"<!--T:1", "<!--T:1"},                         // unterminated
		{"<!--T:1--><!--T:1-->", "oneone"},             // repeated index
		{"<!--T:9999999999-->", "<!--T:9999999999-->"}, // too many digits
		{"<!--U:1-->", "<!--U:1-->"},                   // other tag untouched
	}
	for _, c := range cases {
		if got := substitutePlaceholders(c.in, "T", values); got != c.want {
			t.Errorf("substitutePlaceholders(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The whole point: substitution must not copy the document once per
// placeholder. Allocated bytes for k placeholders in an n-byte document
// must stay within a small constant multiple of n, not k·n.
func TestSubstitutePlaceholders_LinearAllocation(t *testing.T) {
	const k = 2000
	values := make([]string, k)
	var b strings.Builder
	for i := range values {
		values[i] = "v"
		fmt.Fprintf(&b, "%s<!--T:%d-->", strings.Repeat("x", 500), i)
	}
	doc := b.String()
	var out string
	allocs := testing.AllocsPerRun(5, func() { out = substitutePlaceholders(doc, "T", values) })
	if want := strings.Count(out, "v"); want != k {
		t.Fatalf("substituted %d placeholders, want %d", want, k)
	}
	// One Builder buffer plus at most a couple of growth steps.
	const maxAllocs = 8
	if allocs > maxAllocs {
		t.Fatalf("substitutePlaceholders allocated %.0f times for %d placeholders; want <= %d (per-placeholder copying is back)", allocs, k, maxAllocs)
	}
}
