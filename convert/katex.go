// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/marcelocantos/vellum/internal/mdmath"
)

// katexResolveScript binds `katex` using createRequire against the global
// npm prefix, avoiding dependency on the local working directory. It is
// the prelude of every node script vellum runs against KaTeX, so the
// version the test suite pins the vendored stylesheet to (embed/katex) is
// the one that renders.
const katexResolveScript = `
const {execSync} = require("child_process");
const {createRequire} = require("module");

// Try to find katex: local, then global npm prefix.
let katex;
try {
  katex = require("katex");
} catch(e) {
  // Global modules live under <prefix>/lib/node_modules on Unix and
  // <prefix>/node_modules on Windows (%APPDATA%\\npm).
  const path = require("path");
  const prefix = execSync("npm prefix -g", {encoding: "utf8"}).trim();
  let lastErr = e;
  for (const dir of [path.join(prefix, "lib", "node_modules"), path.join(prefix, "node_modules")]) {
    try {
      katex = createRequire(path.join(dir, "katex", "package.json"))("katex");
      break;
    } catch (e2) {
      lastErr = e2;
    }
  }
  if (!katex) throw lastErr;
}
`

const katexRenderScript = katexResolveScript + `
let input = "";
process.stdin.on("data", d => input += d);
process.stdin.on("end", () => {
  const items = JSON.parse(input);
  const results = items.map(({expr, displayMode}) => {
    try {
      return katex.renderToString(expr, {displayMode, throwOnError: false, output: "html"});
    } catch(e) {
      // The expression is document text: escape it, never splice it in.
      const text = String(expr).replace(/[&<>"']/g, c => ({"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"})[c]);
      return '<span class="katex-error">' + text + '</span>';
    }
  });
  process.stdout.write(JSON.stringify(results));
});
`

type mathExpr struct {
	Expr        string `json:"expr"`
	DisplayMode bool   `json:"displayMode"`
}

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

// placeholder records expr and returns its stand-in. The comment spans
// as many lines as the source it replaces (lines), so every block after
// it keeps its source line number (see SourceLineAttr).
func (m *mathPreprocessor) placeholder(expr string, display bool, lines int) string {
	idx := len(m.exprs)
	m.exprs = append(m.exprs, mathExpr{Expr: expr, DisplayMode: display})
	// Use a format that goldmark will treat as raw HTML and pass through.
	p := fmt.Sprintf("<!--MATH:%d%s-->", idx, strings.Repeat("\n", lines))
	m.placeholders = append(m.placeholders, p)
	return p
}

// Extract finds the math in the markdown source (mdmath.Find: never
// inside code, as CommonMark defines it) and replaces each expression
// with an HTML comment placeholder that goldmark passes through. Display
// math is numbered first, then inline, each in source order.
func (m *mathPreprocessor) Extract(src string) string {
	spans := mdmath.Find(src)
	if len(spans) == 0 {
		return src
	}
	placeholders := make([]string, len(spans))
	for _, display := range []bool{true, false} {
		for i, sp := range spans {
			if sp.Display == display {
				placeholders[i] = m.placeholder(sp.Expr, sp.Display, lineCount(src[sp.Start:sp.End]))
			}
		}
	}
	var b strings.Builder
	last := 0
	for i, sp := range spans {
		b.WriteString(src[last:sp.Start])
		b.WriteString(placeholders[i])
		last = sp.End
	}
	b.WriteString(src[last:])
	return b.String()
}

// ReplaceAll batch-renders all collected math expressions via KaTeX
// and replaces placeholders in the rendered HTML.
func (m *mathPreprocessor) ReplaceAll(ctx context.Context, html string) (string, error) {
	if len(m.exprs) == 0 {
		return html, nil
	}
	results, err := RenderFragments(ctx, m.fragments())
	if err != nil {
		return "", err
	}
	out, _ := ResolveFragments(m.deferAll(html), results)
	return out, nil
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
