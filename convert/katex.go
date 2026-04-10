// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// katexRenderScript is a Node.js script that reads a JSON array of
// {expr, displayMode} objects from stdin and writes back a JSON array
// of rendered HTML strings. This avoids spawning a process per expression.
// katexRenderScript uses createRequire to resolve katex from the global
// npm prefix, avoiding dependency on the local working directory.
const katexRenderScript = `
const {execSync} = require("child_process");
const {createRequire} = require("module");

// Try to find katex: local, then global npm prefix.
let katex;
try {
  katex = require("katex");
} catch(e) {
  const prefix = execSync("npm prefix -g", {encoding: "utf8"}).trim();
  const req = createRequire(prefix + "/lib/node_modules/katex/package.json");
  katex = req("katex");
}

let input = "";
process.stdin.on("data", d => input += d);
process.stdin.on("end", () => {
  const items = JSON.parse(input);
  const results = items.map(({expr, displayMode}) => {
    try {
      return katex.renderToString(expr, {displayMode, throwOnError: false, output: "html"});
    } catch(e) {
      return '<span class="katex-error">' + expr + '</span>';
    }
  });
  process.stdout.write(JSON.stringify(results));
});
`

var (
	// Match $$...$$ (block) before $...$ (inline) to avoid partial matches.
	blockMathRe  = regexp.MustCompile(`\$\$\s*([\s\S]+?)\s*\$\$`)
	inlineMathRe = regexp.MustCompile(`\$([^\n$]+?)\$`)
)

type mathExpr struct {
	Expr        string `json:"expr"`
	DisplayMode bool   `json:"displayMode"`
}

// renderKaTeX finds all math expressions in html (delimited by $ and $$),
// renders them via KaTeX, and returns the html with rendered math.
func renderKaTeX(ctx context.Context, html string) (string, error) {
	// Collect all math expressions.
	var exprs []mathExpr
	type replacement struct {
		start, end int
		index      int
		display    bool
	}
	var replacements []replacement

	// Block math first ($$...$$).
	for _, loc := range blockMathRe.FindAllStringSubmatchIndex(html, -1) {
		exprs = append(exprs, mathExpr{
			Expr:        strings.TrimSpace(html[loc[2]:loc[3]]),
			DisplayMode: true,
		})
		replacements = append(replacements, replacement{
			start:   loc[0],
			end:     loc[1],
			index:   len(exprs) - 1,
			display: true,
		})
	}

	// Inline math ($...$) — but skip regions already matched as block math.
	for _, loc := range inlineMathRe.FindAllStringSubmatchIndex(html, -1) {
		// Check if this overlaps with any block match.
		overlaps := false
		for _, r := range replacements {
			if loc[0] >= r.start && loc[0] < r.end {
				overlaps = true
				break
			}
		}
		if overlaps {
			continue
		}
		exprs = append(exprs, mathExpr{
			Expr:        html[loc[2]:loc[3]],
			DisplayMode: false,
		})
		replacements = append(replacements, replacement{
			start:   loc[0],
			end:     loc[1],
			index:   len(exprs) - 1,
			display: false,
		})
	}

	if len(exprs) == 0 {
		return html, nil
	}

	// Render all expressions in one batch via Node.js.
	inputJSON, err := json.Marshal(exprs)
	if err != nil {
		return "", fmt.Errorf("marshalling katex input: %w", err)
	}

	cmd := exec.CommandContext(ctx, "node", "-e", katexRenderScript)
	cmd.Stdin = bytes.NewReader(inputJSON)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("katex render: %w: %s", err, stderr.String())
	}

	var rendered []string
	if err := json.Unmarshal(stdout.Bytes(), &rendered); err != nil {
		return "", fmt.Errorf("parsing katex output: %w", err)
	}

	if len(rendered) != len(exprs) {
		return "", fmt.Errorf("katex returned %d results for %d expressions", len(rendered), len(exprs))
	}

	// Apply replacements in reverse order to preserve offsets.
	// Sort by start position descending.
	for i := len(replacements) - 1; i >= 0; i-- {
		r := replacements[i]
		var wrapped string
		if r.display {
			wrapped = `<div class="katex-display">` + rendered[r.index] + `</div>`
		} else {
			wrapped = rendered[r.index]
		}
		html = html[:r.start] + wrapped + html[r.end:]
	}

	return html, nil
}
