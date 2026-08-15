// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package clipboard

import "testing"

// firstN truncates payload dumps in failure messages: an RTF document
// runs to kilobytes and the interesting part is always the head.
func firstN(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}

func TestHTMLBodyFragment(t *testing.T) {
	for _, c := range []struct {
		name string
		in   string
		want string
	}{
		{"full document", `<html><head><style>p{color:red}</style></head><body><p>hi</p></body></html>`, `<p>hi</p>`},
		{"body with attributes", `<body class="x"><p>hi</p></body>`, `<p>hi</p>`},
		{"already a fragment", `<p>hi</p>`, `<p>hi</p>`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := htmlBodyFragment(c.in); got != c.want {
				t.Errorf("htmlBodyFragment(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
