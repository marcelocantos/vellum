// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package adf

import (
	"fmt"
	"regexp"
	"strings"
)

// Math residue: KaTeX-style $…$ / $$…$$ is not rendered to Confluence equation
// macros. extractMathAsLatexFences rewrites those delimiters into fenced
// ```latex / ```math blocks so goldmark emits codeBlock nodes with the
// original expression preserved (not prose with stray dollar signs).

var (
	blockMathRe  = regexp.MustCompile(`(?m)^\$\$\s*\n([\s\S]+?)\n\$\$\s*$`)
	inlineMathRe = regexp.MustCompile(`\$([^\n$]+?)\$`)
	fencedCodeRe = regexp.MustCompile("(?m)^```[\\s\\S]*?^```\\s*$")
	inlineCodeRe = regexp.MustCompile("`[^`\n]+`")
)

// extractMathAsLatexFences protects code, then turns display and inline math
// into fenced latex blocks that Convert maps to ADF codeBlock language=latex.
func extractMathAsLatexFences(src string) string {
	var codeBlocks []string
	src = fencedCodeRe.ReplaceAllStringFunc(src, func(match string) string {
		idx := len(codeBlocks)
		codeBlocks = append(codeBlocks, match)
		return fmt.Sprintf("<!--ADFCODE:%d-->", idx)
	})
	src = inlineCodeRe.ReplaceAllStringFunc(src, func(match string) string {
		idx := len(codeBlocks)
		codeBlocks = append(codeBlocks, match)
		return fmt.Sprintf("<!--ADFCODE:%d-->", idx)
	})

	src = blockMathRe.ReplaceAllStringFunc(src, func(match string) string {
		inner := blockMathRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		expr := strings.TrimSpace(inner[1])
		return "\n```latex\n" + expr + "\n```\n"
	})

	src = inlineMathRe.ReplaceAllStringFunc(src, func(match string) string {
		inner := inlineMathRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		expr := strings.TrimSpace(inner[1])
		// Inline math as its own fenced block keeps language=latex on a
		// codeBlock (block-level residue; Confluence has no inline math mark).
		return "\n```latex\n" + expr + "\n```\n"
	})

	for i, block := range codeBlocks {
		src = strings.ReplaceAll(src, fmt.Sprintf("<!--ADFCODE:%d-->", i), block)
	}
	return src
}
