# vellum

Document preparation MCP server — media-orthogonal conversion (file,
content, clipboard, file_reference) via goldmark + WeasyPrint (Prince
opt-in), pandoc for rich-text import and PowerPoint (pptx) output.

## Architecture

Go binary with two modes:
- **MCP server** (HTTP on the brew-service daemon at `/mcp`; `vellum --mcp` stdio fallback): single `convert` tool with `from`/`to` media
- **CLI** (`vellum convert --from … --to …`; sugars for bare `.md`, `--to-clipboard`, `import`)

### Pipeline

```
from media → normalise (pandoc import and/or math/mermaid preprocessors + goldmark HTML)
  → to media (file / content / clipboard rich / file_reference)
```

Markdown → PDF path still: source preprocessors → goldmark → HTML template → WeasyPrint/Prince.

Markdown → pptx path: Mermaid preprocessor (PNG, inlined as images) → pandoc gfm reader → pptx writer with `--reference-doc` = the selected template (`convert/pptx.go`). Built-in template is `embed/reference.pptx`, a pinned copy of pandoc's default.

### Key packages

| Package | Role |
|---------|------|
| `cmd/vellum/` | CLI entry point |
| `convert/` | Markdown → HTML/PDF pipeline; unified `Run` router; Backend interface. Math and Mermaid are source preprocessors (`convert/katex.go`, `convert/mermaid.go`), not goldmark extensions. `convert/pptx.go` is the pandoc-backed PowerPoint sink and template resolution. |
| `clipboard/` | Rich pasteboard + Finder file references (macOS). `Write` tries AppKit then pandoc HTML→RTF. |
| `importer/` | Rich-text → Markdown via pandoc; PDF via Poppler |
| `adf/` | Markdown → Confluence ADF (library only; not a `convert.Run` sink) |
| `internal/pandoc/` | pandoc export helpers: HTML → RTF/plain (clipboard fallback) and Markdown → file (pptx); not a public API |
| `internal/xdg/` | Per-user config directory (`$XDG_CONFIG_HOME/vellum` or `~/.config/vellum`) shared by `config/` and template lookup |
| `config/` | User configuration loaded from `~/.config/vellum/config.yaml` |
| `mcp/` | MCP server (single `convert` tool; streamable HTTP + stdio) |
| `embed/` | Embedded assets (CSS, HTML template, built-in pptx reference deck, KaTeX stylesheet + fonts under `katex/`, refreshed by `scripts/vendor-katex.sh`) |
| `internal/testdeps/` | Test gate for external converters (`VELLUM_REQUIRE_DEPS`) |
| `viewer/` | Localhost daemon (HTML view + chrome + `/mcp`); cached PDF open; macOS default .md handler |

### External dependencies

- **WeasyPrint** (default) — HTML → PDF, BSD-3 (must be on PATH; `brew install weasyprint`)
- **Prince** (opt-in via `backend: prince`) — HTML → PDF, proprietary (must be on PATH)
- **node + katex** — KaTeX math HTML (`npm install -g katex`); the stylesheet and fonts are embedded, never fetched, and `TestKaTeXVersionMatchesRenderer` pins the vendored copy to the installed package
- **mmdc** — Mermaid CLI for diagram rendering (optional, on PATH)
- **pandoc** — rich-text import, pptx output, and clipboard HTML→RTF fallback (lazy; only when needed)
- **poppler** — PDF import (`pdftoppm`, `pdftotext`; lazy)

## Gate

`cv gate` is the definition of green — gofmt, vet, the suite with
`VELLUM_REQUIRE_DEPS=1`, a skip census locked at 0 non-pasteboard
skips (pasteboard skips must name “pasteboard”), and the CLI
contract. The gate runs locally on Colossus (macOS); there is no
GitHub Actions. `cv gate` is the only oracle, so there is no remote
build to reproduce: a red gate is the failure, one command away.
`cv bullseye` adds a clean-tree check for convergence.

macOS only. The clipboard, Finder file references, and the viewer are
darwin-only, so a Linux runner would exercise a strict subset while
implying a platform vellum does not support.

## Gates

profile: base
override:
  - pr-workflow: skip

Direct push to `master` is the default delivery path (Colossus gated-push).
`cv gate` is the local oracle; keep `tests-exist`. `/push` must not open a
PR for this repo unless the user explicitly asks for one on a non-default
branch. Never auto-merge remote PRs.

## Delivery

Pushed to master (Colossus gated-push; no mandatory GitHub PR).

## Followable work

Bullseye targets in `bullseye.yaml` (via the bullseye MCP tools) are the sole record of followable work.
