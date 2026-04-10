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

type mathExpr struct {
	Expr        string `json:"expr"`
	DisplayMode bool   `json:"displayMode"`
}

var (
	// Match $$...$$ blocks (possibly multiline) in markdown source.
	blockMathRe = regexp.MustCompile(`(?m)^\$\$\s*\n([\s\S]+?)\n\$\$\s*$`)
	// Match inline $...$ (no newlines, no leading/trailing space).
	inlineMathRe = regexp.MustCompile(`\$([^\n$]+?)\$`)
)

// mathPreprocessor extracts math expressions from markdown source before
// goldmark processes it, replacing them with HTML-safe placeholders that
// goldmark will pass through unchanged. After rendering, call ReplaceAll
// to swap in KaTeX-rendered HTML.
type mathPreprocessor struct {
	exprs        []mathExpr
	placeholders []string
}

func newMathPreprocessor() *mathPreprocessor {
	return &mathPreprocessor{}
}

func (m *mathPreprocessor) placeholder(expr string, display bool) string {
	idx := len(m.exprs)
	m.exprs = append(m.exprs, mathExpr{Expr: expr, DisplayMode: display})
	// Use a format that goldmark will treat as raw HTML and pass through.
	p := fmt.Sprintf("<!--MATH:%d-->", idx)
	m.placeholders = append(m.placeholders, p)
	return p
}

// Extract finds all math expressions in the markdown source and replaces
// them with HTML comment placeholders that goldmark will ignore.
func (m *mathPreprocessor) Extract(src string) string {
	// Block math first ($$...$$ on own lines).
	src = blockMathRe.ReplaceAllStringFunc(src, func(match string) string {
		inner := blockMathRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		return m.placeholder(strings.TrimSpace(inner[1]), true)
	})

	// Inline math ($...$).
	src = inlineMathRe.ReplaceAllStringFunc(src, func(match string) string {
		inner := inlineMathRe.FindStringSubmatch(match)
		if len(inner) < 2 {
			return match
		}
		return m.placeholder(strings.TrimSpace(inner[1]), false)
	})

	return src
}

// ReplaceAll batch-renders all collected math expressions via KaTeX
// and replaces placeholders in the rendered HTML.
func (m *mathPreprocessor) ReplaceAll(ctx context.Context, html string) (string, error) {
	if len(m.exprs) == 0 {
		return html, nil
	}

	rendered, err := batchKaTeX(ctx, m.exprs)
	if err != nil {
		return "", err
	}

	for i, p := range m.placeholders {
		var wrapped string
		if m.exprs[i].DisplayMode {
			wrapped = `<div class="katex-display">` + rendered[i] + `</div>`
		} else {
			wrapped = rendered[i]
		}
		html = strings.Replace(html, p, wrapped, 1)
	}

	return html, nil
}

func batchKaTeX(ctx context.Context, exprs []mathExpr) ([]string, error) {
	inputJSON, err := json.Marshal(exprs)
	if err != nil {
		return nil, fmt.Errorf("marshalling katex input: %w", err)
	}

	cmd := exec.CommandContext(ctx, "node", "-e", katexRenderScript)
	cmd.Stdin = bytes.NewReader(inputJSON)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("katex render: %w: %s", err, stderr.String())
	}

	var rendered []string
	if err := json.Unmarshal(stdout.Bytes(), &rendered); err != nil {
		return nil, fmt.Errorf("parsing katex output: %w", err)
	}

	if len(rendered) != len(exprs) {
		return nil, fmt.Errorf("katex returned %d results for %d expressions", len(rendered), len(exprs))
	}

	return rendered, nil
}
