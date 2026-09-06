// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import "strings"

// The comment terminator that closes every placeholder.
const placeholderClose = "-->"

// placeholderMaxDigits bounds the index parse so a hostile or accidental
// run of digits cannot overflow int; no document has a billion diagrams.
const placeholderMaxDigits = 9

// substitutePlaceholders replaces every `<!--tag:N-->` in s with
// values[N] in one left-to-right pass and returns the result.
//
// The preprocessors used to substitute with one strings.Replace per
// placeholder, which copies the whole document each time: a document
// with k placeholders cost O(k·n) bytes. Measured 2026-09-06 on the
// perf fixtures, that was 260 MB of copying for a 1 MiB prose document
// with 200 inline-code spans, and 6.5 GB for 2,400 math expressions.
// Substituted text is never rescanned, so a value that happens to
// contain a placeholder-shaped comment (a code block documenting this
// very mechanism) is emitted verbatim rather than expanded.
//
// A comment that looks like a placeholder but carries no valid index
// (no digits, too many digits, or an index with no value) is copied
// through unchanged.
func substitutePlaceholders(s, tag string, values []string) string {
	open := "<!--" + tag + ":"
	var b strings.Builder
	b.Grow(len(s))
	for {
		i := strings.Index(s, open)
		if i < 0 {
			break
		}
		j := i + len(open)
		n, digits := 0, 0
		for j < len(s) && s[j] >= '0' && s[j] <= '9' && digits < placeholderMaxDigits {
			n = n*10 + int(s[j]-'0')
			j++
			digits++
		}
		if digits == 0 || !strings.HasPrefix(s[j:], placeholderClose) || n >= len(values) {
			b.WriteString(s[:i+len(open)])
			s = s[i+len(open):]
			continue
		}
		b.WriteString(s[:i])
		b.WriteString(values[n])
		s = s[j+len(placeholderClose):]
	}
	b.WriteString(s)
	return b.String()
}
