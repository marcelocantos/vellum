// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package adf

import (
	"strings"

	"github.com/marcelocantos/vellum/internal/mdmath"
)

// Math residue: KaTeX-style $…$ / $$…$$ is not rendered to Confluence equation
// macros. extractMathAsLatexFences rewrites those delimiters into fenced
// ```latex / ```math blocks so goldmark emits codeBlock nodes with the
// original expression preserved (not prose with stray dollar signs).

// extractMathAsLatexFences turns display and inline math outside code
// (mdmath.Find) into fenced latex blocks that Convert maps to ADF
// codeBlock language=latex. Inline math as its own fenced block keeps
// language=latex on a codeBlock (block-level residue; Confluence has no
// inline math mark).
func extractMathAsLatexFences(src string) string {
	spans := mdmath.Find(src)
	if len(spans) == 0 {
		return src
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(src[last:sp.Start])
		b.WriteString("\n```latex\n" + sp.Expr + "\n```\n")
		last = sp.End
	}
	b.WriteString(src[last:])
	return b.String()
}
