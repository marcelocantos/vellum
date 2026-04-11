// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// placeholderRe matches the HTML comment placeholders injected by the math
// preprocessor, e.g. <!--MATH:0-->.
var placeholderRe = regexp.MustCompile(`<!--MATH:(\d+)-->`)

func TestMathPreprocessor_InlineMath(t *testing.T) {
	m := newMathPreprocessor()
	out := m.Extract("A quadratic: $x^2 + y^2 = z^2$ done.")

	if len(m.exprs) != 1 {
		t.Fatalf("expected 1 expr, got %d: %#v", len(m.exprs), m.exprs)
	}
	if m.exprs[0].DisplayMode {
		t.Errorf("inline math should not be display mode")
	}
	if got := m.exprs[0].Expr; got != "x^2 + y^2 = z^2" {
		t.Errorf("expression = %q, want %q", got, "x^2 + y^2 = z^2")
	}
	if !strings.Contains(out, "<!--MATH:0-->") {
		t.Errorf("output missing placeholder: %q", out)
	}
	if strings.Contains(out, "$") {
		t.Errorf("output still contains $: %q", out)
	}
}

func TestMathPreprocessor_BlockMath(t *testing.T) {
	m := newMathPreprocessor()
	src := "Before.\n\n$$\na = b + c\n$$\n\nAfter."
	out := m.Extract(src)

	if len(m.exprs) != 1 {
		t.Fatalf("expected 1 expr, got %d: %#v", len(m.exprs), m.exprs)
	}
	if !m.exprs[0].DisplayMode {
		t.Errorf("block math should be display mode")
	}
	if got := m.exprs[0].Expr; got != "a = b + c" {
		t.Errorf("expression = %q, want %q", got, "a = b + c")
	}
	if !strings.Contains(out, "<!--MATH:0-->") {
		t.Errorf("output missing placeholder: %q", out)
	}
}

func TestMathPreprocessor_MultilineMatrix(t *testing.T) {
	// The canonical hard case: a multi-line matrix with \\ row separators.
	// Source-level preprocessing must capture this verbatim so goldmark
	// never sees (and mangles) the backslashes.
	m := newMathPreprocessor()
	matrix := `\begin{pmatrix} a & b \\ c & d \end{pmatrix}`
	src := "A matrix:\n\n$$\n" + matrix + "\n$$\n\nDone."
	out := m.Extract(src)

	if len(m.exprs) != 1 {
		t.Fatalf("expected 1 expr, got %d: %#v", len(m.exprs), m.exprs)
	}
	if !m.exprs[0].DisplayMode {
		t.Errorf("expected display mode")
	}
	if got := m.exprs[0].Expr; got != matrix {
		t.Errorf("matrix expression mangled:\n  got:  %q\n  want: %q", got, matrix)
	}
	if !strings.Contains(m.exprs[0].Expr, `\\`) {
		t.Errorf("row separators lost: %q", m.exprs[0].Expr)
	}
	if !strings.Contains(out, "<!--MATH:0-->") {
		t.Errorf("output missing placeholder: %q", out)
	}
}

func TestMathPreprocessor_FencedCodeProtected(t *testing.T) {
	m := newMathPreprocessor()
	src := "Some shell:\n\n```bash\necho $VAR\nexport $PATH\n```\n\nDone."
	out := m.Extract(src)

	if len(m.exprs) != 0 {
		t.Errorf("math regex should not touch fenced code: %#v", m.exprs)
	}
	if !strings.Contains(out, "echo $VAR") {
		t.Errorf("fenced code body lost: %q", out)
	}
	if !strings.Contains(out, "export $PATH") {
		t.Errorf("fenced code body lost: %q", out)
	}
}

func TestMathPreprocessor_InlineCodeProtected(t *testing.T) {
	m := newMathPreprocessor()
	src := "Set `$VAR` and then compute $y = 2x$."
	out := m.Extract(src)

	if len(m.exprs) != 1 {
		t.Fatalf("expected exactly 1 math expr (not the code one), got %d: %#v",
			len(m.exprs), m.exprs)
	}
	if m.exprs[0].Expr != "y = 2x" {
		t.Errorf("expression = %q, want %q", m.exprs[0].Expr, "y = 2x")
	}
	if !strings.Contains(out, "`$VAR`") {
		t.Errorf("inline code `$VAR` was not preserved: %q", out)
	}
}

func TestMathPreprocessor_MixedInlineAndBlock(t *testing.T) {
	m := newMathPreprocessor()
	src := "Inline $a+b$ then block:\n\n$$\nx = y\n$$\n\nAnd another $c$."
	out := m.Extract(src)

	if len(m.exprs) != 3 {
		t.Fatalf("expected 3 exprs, got %d: %#v", len(m.exprs), m.exprs)
	}

	// Block math is extracted first, then inline. Ordering within the exprs
	// slice is implementation detail; gather by expression text instead.
	byExpr := map[string]mathExpr{}
	for _, e := range m.exprs {
		byExpr[e.Expr] = e
	}
	if e, ok := byExpr["a+b"]; !ok || e.DisplayMode {
		t.Errorf("inline a+b missing or wrong mode: %+v", e)
	}
	if e, ok := byExpr["x = y"]; !ok || !e.DisplayMode {
		t.Errorf("block x=y missing or wrong mode: %+v", e)
	}
	if e, ok := byExpr["c"]; !ok || e.DisplayMode {
		t.Errorf("inline c missing or wrong mode: %+v", e)
	}

	count := len(placeholderRe.FindAllString(out, -1))
	if count != 3 {
		t.Errorf("expected 3 placeholders in output, got %d: %q", count, out)
	}
}

func TestMathPreprocessor_ExtractRestoreRoundTrip(t *testing.T) {
	// Simulate the end-to-end shape without actually calling KaTeX:
	// after Extract, the caller would hand `processed` to goldmark, then
	// substitute rendered HTML into each <!--MATH:N--> placeholder. We
	// mimic that substitution here and assert it ends up where expected.
	m := newMathPreprocessor()
	src := "Inline $a$ and block:\n\n$$\nb\n$$\n\nTrailing $c$."
	processed := m.Extract(src)

	// Fake rendered HTML per expression.
	fakes := make([]string, len(m.exprs))
	for i, e := range m.exprs {
		fakes[i] = fmt.Sprintf("<span class=\"k-%d\">%s</span>", i, e.Expr)
	}

	// Substitute placeholders (mirrors the real ReplaceAll inner loop).
	out := processed
	for i, p := range m.placeholders {
		out = strings.Replace(out, p, fakes[i], 1)
	}

	if strings.Contains(out, "<!--MATH:") {
		t.Errorf("unreplaced placeholder remains: %q", out)
	}
	if !strings.Contains(out, "Inline ") || !strings.Contains(out, " and block:") {
		t.Errorf("surrounding text lost: %q", out)
	}
	if !strings.Contains(out, "Trailing ") {
		t.Errorf("trailing text lost: %q", out)
	}
	// Each fake must appear exactly once.
	for _, f := range fakes {
		if n := strings.Count(out, f); n != 1 {
			t.Errorf("fake %q count = %d, want 1 in %q", f, n, out)
		}
	}
}

func TestMathPreprocessor_NoMath(t *testing.T) {
	m := newMathPreprocessor()
	src := "Just plain text with no dollar signs.\n\nAnd a second paragraph."
	out := m.Extract(src)
	if out != src {
		t.Errorf("Extract mutated dollar-free source:\n  in:  %q\n  out: %q", src, out)
	}
	if len(m.exprs) != 0 {
		t.Errorf("expected no exprs, got %d", len(m.exprs))
	}
}
