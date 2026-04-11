# Vellum Handover Document

Complete context for picking up vellum development. This document captures
the product vision, architecture, decisions made, pitfalls encountered,
current state, and what's left to build.

## What vellum is

A document preparation tool that converts GitHub-flavoured Markdown to
high-quality PDF. It runs in two modes:

- **CLI**: `vellum [flags] <input.md...>` — direct command-line conversion
- **MCP server**: `vellum --mcp` — stdio-based Model Context Protocol server
  exposing document conversion as a tool for AI agents

Vellum is the Go-based successor to **mpe2pdf**, a Node.js tool that used
[mume](https://github.com/shd101wyy/mume) (the Markdown Preview Enhanced
engine) + Prince. mpe2pdf will be deprecated in favour of vellum.

## Why vellum exists (the origin story)

mpe2pdf worked but had serious drag:

- **mume is heavy**: 329 transitive dependencies, 264 MB in `node_modules`,
  bundles ~8 MB of vendored static assets (mermaid, katex, font-awesome,
  reveal.js, etc.), includes deprecated packages (`request`, old `glob`,
  `har-validator`).
- **mume does far more than needed**: VS Code webview integration,
  notebook/crossnote graph, chrome/puppeteer export, eBook/pandoc exports,
  image uploaders (qiniu, imgur), reveal.js slides, `less` compiler — mpe2pdf
  used 3 mume API calls out of all of that.
- **Node.js ecosystem churn**: `npm install -g mpe2pdf` produced 394 packages
  with deprecation warnings.
- **mpe2pdf was CLI-first**: the user wanted to recast it as a document
  preparation *platform* with MCP-first interface for AI agent integration.

Earlier experiments with pandoc and weasyprint choked on complex content.
Typst is closed-source SaaS (not viable). Headless Chrome via Playwright +
Paged.js is a legitimate alternative but adds ~250 MB of Chromium. The
decision: keep Prince, replace mume with a lean Go pipeline.

## The product vision

**Name**: vellum — fine writing surface; implies quality output
**Owner**: marcelocantos (personal GitHub)
**Repo**: https://github.com/marcelocantos/vellum
**Language**: Go (not Node.js)
**License**: Apache 2.0
**Default branch**: master
**Merge policy**: squash-only (configured on the repo)

Vellum is positioned as a **document preparation MCP server**. The CLI is
still supported (direct invocation is useful), but the primary interaction
model is: AI agents call vellum via MCP to produce polished PDFs from
Markdown they generated.

## Architecture

```
Markdown source
  ↓
  math preprocessor (extract $...$ and $$...$$)
  ↓
  mermaid preprocessor (extract ```mermaid blocks)
  ↓
  goldmark (GFM + footnotes + definition lists + typographer + meta + chroma)
  ↓
  HTML fragment
  ↓
  KaTeX batch render (Node.js script, reinject HTML)
  ↓
  Mermaid render (mmdc → PNG → base64 data URI)
  ↓
  Wrap in HTML template with embedded GitHub CSS + chroma CSS
  ↓
  Prince → PDF
```

### Key design decisions

1. **Source-level preprocessors for math and mermaid**, not goldmark AST
   extensions. Why: goldmark's inline parser consumes escape sequences (`\\`)
   before extensions can see them, which breaks multi-line LaTeX matrices
   when using the Hugo passthrough extension. Source-level extraction with
   HTML comment placeholders (`<!--MATH:0-->`, `<!--MERMAID:0-->`) sidesteps
   this entirely.

2. **Server-side KaTeX rendering via Node.js**, not client-side via Prince's
   JS engine. Why: Prince 16.2's JS engine is old and doesn't support ES6
   classes, so modern KaTeX fails to parse. The batch script resolves
   katex from the global npm prefix, renders all expressions in one pass,
   and returns a JSON array of HTML strings.

3. **Mermaid rendered to PNG, not SVG**. Why: Mermaid SVGs use
   `<foreignObject>` elements for text labels, which Prince doesn't render.
   The result was diagrams with shapes but no text. PNG sidesteps this at
   the cost of scalability, which is acceptable for PDF output.

4. **Mermaid rendered at 2× scale by default** for sharpness, with
   per-diagram scale override via `<!-- vellum:scale N -->` comments.
   Why: PNG at 1× is blurry when embedded in a PDF; 2× gives crisp output.
   Large diagrams can be downscaled with the hint to share pages with their
   heading.

5. **Code blocks protected from math extraction**. The math regex was eating
   `$VAR` in shell scripts. Fix: extract fenced code blocks and inline code
   with temporary placeholders before running math extraction, then restore.

6. **GitHub-style CSS embedded via `go:embed`**, not loaded at runtime.
   Self-contained binary, no filesystem dependencies.

## Current stack

| Component | Package / Tool | Role |
|---|---|---|
| Markdown parser | `github.com/yuin/goldmark` v1.8.2 | CommonMark + GFM |
| Footnotes | `goldmark/extension.Footnote` | PHP Markdown Extra footnotes |
| Definition lists | `goldmark/extension.DefinitionList` | PHP Markdown Extra dlists |
| Front-matter | `github.com/yuin/goldmark-meta` | YAML front-matter (for title) |
| Syntax highlighting | `github.com/yuin/goldmark-highlighting/v2` + chroma v2 | Code highlighting |
| Math (preprocessor) | custom regex + Node.js katex | $ and $$ extraction, batch render |
| Mermaid (preprocessor) | custom regex + mmdc CLI | ```mermaid extraction, PNG render |
| HTML → PDF | Prince 16.2 (shell out) | PDF generation |
| MCP SDK (planned) | `github.com/modelcontextprotocol/go-sdk` | Official Go MCP SDK |

### External runtime dependencies

- **Prince** — HTML → PDF. Must be on PATH. https://www.princexml.com/
- **Node.js** — required for KaTeX rendering. Must be on PATH.
- **katex** — must be installed globally: `npm install -g katex`
- **mmdc** — Mermaid CLI. Install via `brew install mermaid-cli` or equivalent.
- **Chromium for mmdc** — must be the specific version mmdc's bundled
  puppeteer expects. Install via:
  `cd /opt/homebrew/Cellar/mermaid-cli/*/libexec && npx puppeteer browsers install chrome-headless-shell@131.0.6778.204`
  (adjust version as mermaid-cli updates).

## Directory layout

```
vellum/
├── CLAUDE.md                 # project conventions (minimal)
├── LICENSE                   # Apache 2.0
├── cmd/vellum/main.go        # CLI entry point, flag parsing
├── convert/
│   ├── convert.go            # core pipeline: read → preprocess → goldmark → prince
│   ├── katex.go              # math preprocessor + Node.js batch render
│   └── mermaid.go            # mermaid preprocessor + mmdc invocation
├── embed/
│   ├── embed.go              # go:embed declarations
│   ├── github.css            # GitHub-flavoured markdown CSS + print rules
│   └── template.html         # HTML wrapper template
├── mcp/                      # EMPTY — MCP server not yet implemented
├── test/
│   └── sample.md             # comprehensive feature showcase document
├── docs/
│   └── HANDOVER.md           # this file
├── go.mod
└── go.sum
```

## What's working (verified visually via PDF → PNG audits)

All features exercised by `test/sample.md` and confirmed rendering correctly
in the PDF output:

- Text formatting: bold, italic, strikethrough, inline code, links
- Paragraph reflow (adjacent lines flow as single paragraph — this was
  a pain point in mpe2pdf, fixed early by disabling goldmark's hard-break mode
  which is the default behaviour; mume required explicit config)
- Headings (h1–h6) with border-bottom on h1/h2
- Unordered, ordered, nested lists
- Task lists with checkboxes
- Definition lists
- Tables (simple, aligned, wide with long content)
- Fenced code blocks for JavaScript, Python, Go, Rust, Shell, SQL, YAML
- Syntax highlighting via chroma (GitHub style)
- Code wrapping for long lines (`white-space: pre-wrap`, `overflow: visible`)
- Inline math (`$...$`) — quadratic formula, Euler's identity, etc.
- Block math (`$$...$$`) — Maxwell's equations, Gaussian integral,
  Laplace transform
- **3×3 matrices with LaTeX `\\` row separators** — this was the hardest
  case; the source-level preprocessor makes it work
- Mermaid flowchart
- Mermaid sequence diagram
- Mermaid class diagram
- Mermaid state diagram
- Mermaid Gantt chart
- Mermaid ER diagram
- Mermaid pie chart
- Footnotes (including multi-paragraph with embedded code)
- Images (including inline base64 data URIs)
- Horizontal rules
- Blockquotes (including multi-paragraph)
- Nested structures (code inside lists, blockquotes inside lists)
- HTML entities and special characters
- Front-matter `title` extraction

## What's NOT working / not implemented

### Critical (must have for 1.0)

1. **MCP server mode** — `--mcp` flag is stubbed in `cmd/vellum/main.go`
   with `return fmt.Errorf("MCP server not yet implemented")`. The `mcp/`
   directory exists but is empty. Implementation plan:
   - Use `github.com/modelcontextprotocol/go-sdk` v1.5.0+ (official SDK,
     maintained by Anthropic + Google). API: `mcp.NewServer()` →
     `mcp.AddTool()` → `server.Run(StdioTransport{})`.
   - Expose a `convert` tool with schema matching mpe2pdf's:
     `{ files: [{ input: string, output?: string }] }`
   - Reuse `convert.Convert()` from `convert/convert.go` — it already
     takes a `context.Context` and paths.
   - Remember: stdio MCP servers must write logs to stderr only; stdout
     is reserved for JSON-RPC.

2. **Tests** — no Go test suite exists. Minimum viable:
   - Unit tests for `mathPreprocessor.Extract` and `mermaidPreprocessor.Extract`
     (these are pure string transformations, easy to test)
   - Integration test that runs `convert.Convert()` on a small markdown
     file and verifies the PDF is produced and non-empty
   - PDF content verification: use `pdftotext` (or `github.com/ledongthuc/pdf`)
     to extract text and assert expected strings are present.
   - CLI tests for `--help`, `--version`, missing-input errors.
   - Skip MCP server tests until the server is implemented.

3. **README.md** — missing. Must cover:
   - What vellum is
   - Installation (binary download, Homebrew, `go install`)
   - Prerequisites (Prince, Node.js + katex, mermaid-cli + Chromium)
   - Quick start with CLI
   - MCP server configuration (JSON block for Claude Code `.mcp.json`)
   - Feature list with examples
   - Licence

4. **Agent guide** — `agents-guide.md` missing. Should be embedded via
   `go:embed` and served by a `--help-agent` flag. Content should cover:
   - What vellum does
   - How agents should invoke it (MCP preferred, CLI fallback)
   - Tool schema and example call
   - Restrictions: input must be absolute path, expect `.md` extension
   - The `<!-- vellum:scale N -->` hint for oversized mermaid diagrams
   - Security note (no `--no-scripts` flag yet; see gaps below)

5. **`--help-agent` flag** — wire this up to print usage + embedded
   agent guide to stdout.

### Important (should have for 1.0)

6. **STABILITY.md** — not yet created. Use mpe2pdf's as a starting point
   (see `/Users/marcelo/work/github.com/marcelocantos/mpe2pdf/STABILITY.md`)
   and update the surface catalogue to reflect vellum's actual CLI +
   MCP schema. Track:
   - CLI flags: `--help`, `--help-agent`, `--mcp`, `--version`, `-o`/`--output`
   - MCP server info and tool schema
   - Output behaviour (stdout = PDF path, stderr = errors, exit codes)
   - `<!-- vellum:scale N -->` as a stable markdown extension
   - Embedded CSS as a "stable default but may change" surface

7. **CI workflow** — `.github/workflows/release.yml` to build binaries
   on release events. See mpe2pdf's release history and
   `~/.claude/skills/release/` for the standard pattern:
   - Build matrix: macOS arm64, Linux x86_64, Linux arm64
   - Package as `vellum-<version>-<os>-<arch>.tar.gz`
   - Upload to the existing GitHub release (created by `gh release create`
     locally, NOT by CI)
   - Follow-up job: homebrew-releaser to update `marcelocantos/homebrew-tap`

8. **Homebrew formula** via homebrew-releaser. Secret `HOMEBREW_TAP_TOKEN`
   must be set on the repo (retrieve from 1Password:
   `op read "op://Personal/GitHub Homebrew Tap PAT/token"`).
   Known gotchas (learned from mpe2pdf):
   - `skip_checksum: true` is required when the token is tap-scoped only
   - The repo MUST have a description set (already done via `gh repo create`)
   - Set `version: ${{ github.event.release.tag_name }}` explicitly;
     auto-detection breaks on platform-specific URL suffixes

9. **Runtime dependency checks** — `vellum` should fail fast with a clear
   error message if `prince`, `node`, `katex`, or `mmdc` is missing from
   PATH. Currently you get a confusing error mid-pipeline.

10. **`--no-scripts` or equivalent safety flag** — Prince runs JavaScript
    by default when `--javascript` is passed, but vellum doesn't currently
    pass that flag, so scripts in the HTML are NOT executed. Worth
    documenting this explicitly. Mermaid and KaTeX both run server-side,
    so client-side JS in Prince is not needed.

### Nice to have

11. **Watch mode** — `vellum --watch file.md` that re-renders on file changes.
12. **Custom CSS** — `vellum --css custom.css file.md` to override the
    embedded GitHub style.
13. **Output format** — currently only PDF; HTML output would be trivially
    easy (skip the Prince step).
14. **Bundled KaTeX** — currently requires `npm install -g katex`; bundling
    the KaTeX JS into the Go binary via `go:embed` and executing it via
    `node -e` would eliminate one install step.
15. **Plantuml support** — via `plantuml` CLI invocation, same pattern as
    mermaid. Mentioned in mpe2pdf but not widely used.
16. **Bundled Chromium for mmdc** — currently requires manual install of
    a specific Chrome version. Could be automated via a `vellum doctor`
    command.
17. **Better error messages** — mmdc failures currently fall back to
    `<pre class="mermaid-error">` but the error isn't surfaced to the user.

## Key technical decisions & why they matter

### Why not use the Hugo passthrough goldmark extension?

We tried. The Hugo passthrough extension preserves `$` and `$$` delimited
content verbatim so it survives goldmark's inline parser. It works for
simple cases. **It breaks on multi-line math with backslashes** because
goldmark's inline parser processes `\\` as a hard line break *before* the
passthrough transformer runs, fracturing the block. Test case: any 2×2 or
larger LaTeX matrix with `\\` row separators fails.

The source-level preprocessor approach (extract with regex, replace with
HTML comment placeholders, let goldmark render everything else, then
restore with KaTeX-rendered HTML) works for all LaTeX constructs we've
tried. It's less "pure" but more robust.

### Why not render Mermaid as SVG?

SVG would scale better than PNG and produce smaller files. BUT: mermaid
generates SVGs with `<foreignObject>` elements containing HTML `<div>`s
for text labels (for proper multi-line wrapping and HTML entity support).
**Prince doesn't render `<foreignObject>` content reliably** — the result
was diagrams with shapes and arrows but no text labels. We tried setting
`htmlLabels: false` in mermaid config, but edge labels still use
foreignObject and other diagram types (class, ER) break with it disabled.

PNG at 2× scale sidesteps this entirely. The tradeoff: larger output files.
For a document with 8 mermaid diagrams at 2× scale, the PDF grows by
~1-2 MB.

### Why rely on external `mmdc` instead of rendering in-process?

No Go-native mermaid renderer exists. The Go ecosystem has
`go.abhg.dev/goldmark/mermaid` but it either injects JavaScript (useless
for Prince) or shells out to mmdc anyway. In-process rendering would require
embedding a JS engine (v8go or goja) plus bundling mermaid's source, which
is huge. Shelling out to `mmdc` is pragmatic.

### Why KaTeX via Node.js instead of Go-native?

Two Go-native options exist:
- `github.com/Wyatt915/goldmark-treeblood` — pure Go, LaTeX → MathML.
  Works for simple math but doesn't handle all LaTeX constructs.
- `github.com/FurqanSoftware/goldmark-katex` — shells out to KaTeX CLI.

KaTeX via Node.js is the most battle-tested path for LaTeX → HTML. The
batch approach (collect all expressions, render in one Node invocation)
keeps the overhead to one process spawn per document, not per expression.

### The `<!-- vellum:scale N -->` convention

Mermaid diagrams can be too tall to fit on a page alongside their heading,
causing awkward page breaks where the heading sits alone at the bottom of
one page and the diagram fills the next. Auto-scaling isn't feasible
because we don't know the page layout at render time.

Solution: an HTML comment hint before the mermaid block applies a CSS
scale factor (`max-width: N%`) to the rendered diagram. `<!-- vellum:scale
0.6 -->` means "render at 60% of container width". This lets users trade
diagram readability for page layout on a case-by-case basis.

The regex for this is in `convert/mermaid.go`:
```go
var mermaidBlockRe = regexp.MustCompile(
  "(?m)(?:^<!--\\s*vellum:scale\\s+([0-9.]+)\\s*-->\\s*\n)?" +
  "^```mermaid\\s*\n([\\s\\S]+?)^```\\s*$")
```

Future: consider a more general `<!-- vellum:... -->` syntax for other
per-block hints (alignment, caption, etc.) if needed.

## Pitfalls & gotchas

### Go regex quirks

- Go's `regexp` package doesn't support backreferences (`\1`), so you can't
  match "opening fence must match closing fence" for variable-length fences.
  Current approach: hardcode ` ``` ` only. Tilde fences (`~~~`) are not
  supported; this is a gap.

### Prince's JS engine

- Prince 16.2 JavaScript is old (pre-ES6 classes). **Do not try to run
  modern JS in it.** KaTeX, Mermaid, Chart.js all fail. Render everything
  server-side and hand Prince static HTML+CSS+images.

### Prince's CSS support

- `vh`, `vw` units are NOT supported in print context. Use `mm`, `cm`, `in`,
  or `pt`.
- `overflow: auto` on `<pre>` doesn't enable wrapping in print. Must use
  `white-space: pre-wrap` and `word-wrap: break-word`.
- `break-inside: avoid` works on `<pre>` and `<table>` to prevent splitting.

### mmdc's Chrome version pinning

- mmdc bundles a specific puppeteer version which requires a specific
  Chromium version. When Homebrew updates mermaid-cli, the cached Chrome
  may no longer match, producing confusing "Could not find Chrome (ver.
  X.Y.Z)" errors. Fix: re-run
  `npx puppeteer browsers install chrome-headless-shell@<version>`
  with the version mmdc wants.
- First-time setup requires manual Chrome install. This is a bad onboarding
  experience and should be addressed in a `vellum doctor` command or install
  script.

### Shell flag parsing

- Go's `flag` package stops at the first non-flag argument, so
  `vellum file.md -o out.pdf` fails. Vellum uses manual arg parsing in
  `cmd/vellum/main.go` to allow flags anywhere. If you refactor this,
  make sure the `run()` function preserves this behaviour.

### Mermaid SVG text rendering

- If you ever revisit SVG output: the issue is `<foreignObject>`. The
  workarounds I know of are: (a) PNG rendering (current approach), (b)
  post-process the SVG to convert foreignObject content to native
  `<text>` elements (complex, hard to get right), or (c) use a different
  diagram library. There's no simple CSS fix.

## Runtime dependency setup (for a new machine)

```bash
# 1. Prince (proprietary, non-commercial free)
# Download from https://www.princexml.com/download/
# Prince 16.2+ required

# 2. Node.js
brew install node

# 3. KaTeX (global npm)
npm install -g katex

# 4. Mermaid CLI
brew install mermaid-cli

# 5. Chromium for mermaid (version must match mmdc's bundled puppeteer)
cd /opt/homebrew/Cellar/mermaid-cli/*/libexec
npx puppeteer browsers install chrome-headless-shell@131.0.6778.204
# ^^ version number will drift; check mmdc error message for the expected version

# 6. Verify
prince --version
node --version
katex --version || echo "katex installed at $(which katex)"
mmdc --version
```

## Development workflow

```bash
cd ~/work/github.com/marcelocantos/vellum

# Build
go build ./cmd/vellum/

# Convert sample document
./vellum test/sample.md -o /tmp/vellum-test.pdf

# Debug: save intermediate HTML
VELLUM_DEBUG_HTML=/tmp/vellum-debug.html ./vellum test/sample.md -o /tmp/out.pdf

# Visual audit: convert PDF pages to PNG for inspection
pdftoppm -png -r 150 /tmp/vellum-test.pdf /tmp/vellum-page
# then Read /tmp/vellum-page-01.png etc.
```

The `VELLUM_DEBUG_HTML` env var is a development escape hatch that writes
the intermediate HTML (after all preprocessing and goldmark rendering)
before Prince runs. Useful for debugging placeholder replacement issues
and CSS problems.

## Test document (`test/sample.md`)

This file is the regression test for vellum. It exercises every rendering
feature and should be the first thing you run after any pipeline change.
The current version uses the `<!-- vellum:scale 0.6 -->` hint on the
flowchart to keep it on the same page as its heading.

When adding new features, extend this document with an example. When fixing
bugs, add a minimal repro to this document or create a small separate
document in `test/` if the full sample would obscure the issue.

## Source references

### mpe2pdf (deprecated, reference only)

`/Users/marcelo/work/github.com/marcelocantos/mpe2pdf/` contains:

- `STABILITY.md` — interaction surface catalogue, good starting point
  for vellum's STABILITY.md
- `README.md` — structure and content that should be adapted for vellum
- `agents-guide.md` — structure for vellum's agent guide
- `CLAUDE.md` — project conventions, delivery definition, TODO file location
- `test/sample.md` — the original comprehensive test document, copied
  to vellum and still evolving
- `mpe2pdf.mjs` — the Node.js implementation, useful reference for
  what mume's pipeline looked like
- `docs/audit-log.md` — history of releases and audits

### External documentation

- goldmark: https://github.com/yuin/goldmark
- chroma styles: https://github.com/alecthomas/chroma/tree/master/styles
- Prince user guide: https://www.princexml.com/doc/
- KaTeX supported functions: https://katex.org/docs/supported.html
- Mermaid syntax: https://mermaid.js.org/
- MCP Go SDK: https://github.com/modelcontextprotocol/go-sdk
- Official MCP spec: https://modelcontextprotocol.io/

## Conversation history highlights

Key decisions and pivots made during the development session:

1. **Initial attempt**: tried the Hugo passthrough goldmark extension for
   math. Worked for simple cases, failed on matrices. Switched to
   source-level preprocessing.

2. **Prince JS probe**: tested whether Prince could run KaTeX via CDN.
   Confirmed inline JS works, CDN fetching works, but KaTeX's modern JS
   fails to parse due to ES6 classes. Pivoted to server-side rendering.

3. **Mermaid SVG vs PNG**: started with SVG, saw text labels missing,
   diagnosed as foreignObject issue, tried `htmlLabels: false` (partial
   fix), pivoted to PNG at 2× scale.

4. **Chrome install issue**: first mermaid run failed because Chrome
   wasn't installed for puppeteer. Installed the wrong version first
   (146 vs 131 that mmdc expected). Documented the version pin problem.

5. **Math-in-code bug**: shell scripts with `$VAR` had their variables
   replaced with `<!--MATH:N-->` placeholders. Fixed by protecting
   fenced code blocks and inline code from the math regex via a
   temp-placeholder dance.

6. **Code wrapping**: `white-space: pre-wrap` on `pre code` wasn't enough —
   Prince needed `white-space: pre-wrap` on `pre` itself and
   `overflow: visible` instead of `overflow: auto`.

7. **Mermaid scale hint**: added `<!-- vellum:scale N -->` after the user
   pointed out that a large flowchart was pushed to its own page with
   the heading orphaned. This is the cleanest solution we could find that
   avoided either global downscaling or auto-layout.

## Suggested next session order

1. **README.md** — unblocks everything else; users need onboarding docs
2. **MCP server** — the stated product vision; highest-value feature
3. **Runtime dependency checks** — `vellum doctor` or fail-fast checks
4. **agents-guide.md + `--help-agent`** — makes vellum agent-friendly
5. **Tests** — unit tests for preprocessors, integration test for pipeline
6. **STABILITY.md** — document the surface before first release
7. **CI + release workflow** — follow mpe2pdf's pattern
8. **First release (v0.1.0)** — use `/release` skill once the above are done
9. **Deprecate mpe2pdf** — add a note to mpe2pdf's README pointing to vellum,
   stop publishing to npm

## Open questions for the owner

- **Binary distribution**: Homebrew tap is the default for CLI binaries.
  Should vellum also be on npm (since its runtime deps are Node.js-based)?
  A `npm install -g vellum` would bundle the Go binary + auto-install the
  Node deps. Pro: frictionless for JS ecosystem users. Con: binds vellum
  to the very ecosystem we're moving away from.

- **Bundled vs external Node deps**: currently KaTeX and mermaid-cli must
  be installed separately. Could bundle KaTeX source into the Go binary
  and execute via `node -e`, eliminating one manual step. Mermaid-cli is
  harder because of Chromium.

- **MCP transport**: stdio only, or also HTTP? mpe2pdf was stdio-only.
  HTTP would let vellum run as a daemon for higher throughput but adds
  complexity (port management, lifecycle, service definitions).

- **"Document preparation" scope**: is this strictly Markdown → PDF, or
  should it eventually handle other formats (DOCX via pandoc-shelling,
  HTML output, EPUB)? The name "vellum" doesn't commit to any particular
  output format.

- **Stability timeline**: mpe2pdf's STABILITY.md noted a "settling period"
  before 1.0. Does vellum inherit that or reset it? Practical question:
  when can we commit to an API contract?

---

**Last updated**: 2026-04-11
**Current vellum version**: unreleased (pre-0.1.0)
**Current commit**: `396c418` (Add vellum:scale hint for mermaid diagrams, fix code wrapping)
