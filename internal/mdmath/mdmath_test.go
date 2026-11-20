// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mdmath

import (
	"reflect"
	"testing"
)

// Every form of code CommonMark has keeps its dollars.
func TestFind_NoMathInCode(t *testing.T) {
	for _, src := range []string{
		"```\n$E = mc^2$\n```\n",
		" ```\n $E = mc^2$\n ```\n",
		"   ```\n$E = mc^2$\n   ```\n",
		"~~~\n$E = mc^2$\n~~~\n",
		"  ~~~ text\n$a$ and $b$\n  ~~~\n",
		"````\n```\n$a$\n```\n````\n",
		"``` $info$\nx\n```\n",
		"    price is $5 and $10\n",
		"para\n\n\tcost $5 and $10\n",
		"``$x$``\n",
		"`` a ` $b$ ``\n",
		"- item\n\n      $x$ in list code\n",
		"> ```\n> $x$\n> ```\n",
		"1. ```\n   $x$\n   ```\n",
		"```\n$$\nx\n$$\n```\n",
		"~~~\nunclosed fence $x$\n",
	} {
		if spans := Find(src); len(spans) != 0 {
			t.Errorf("%q: math found in code: %+v", src, spans)
		}
	}
}

func TestFind_MathOutsideCode(t *testing.T) {
	cases := []struct {
		src  string
		want []Span
	}{
		{"a $x$ b\n", []Span{{2, 5, "x", false}}},
		{"`$5` then $y$\n", []Span{{10, 13, "y", false}}},
		{"``$5`` $y$\n", []Span{{7, 10, "y", false}}},
		{"$a `b` c$\n", []Span{{0, 9, "a `b` c", false}}},
		{"```\n$$\n```\n\n$$\nx = 1\n$$\n", []Span{{12, 23, "x = 1", true}}},
		{"    $code$\n\n$$\nx\n$$\n\n$y$\n", []Span{{12, 19, "x", true}, {21, 24, "y", false}}},
		// Not display math (text after the closing $$), so the inline rule
		// reads it as the old regular expression did.
		{"$$\na\n$$ and $b$\n", []Span{{6, 13, "and", false}}},
		{"cost $5 and $10\n", []Span{{5, 13, "5 and", false}}},
	}
	for _, c := range cases {
		if got := Find(c.src); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q:\n got %+v\nwant %+v", c.src, got, c.want)
		}
	}
}
