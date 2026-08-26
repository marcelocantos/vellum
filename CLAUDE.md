# vellum

Document preparation MCP server — media-orthogonal conversion (file,
content, clipboard, file_reference) via goldmark + WeasyPrint (Prince
opt-in) and pandoc for rich-text import.

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

### Key packages

| Package | Role |
|---------|------|
| `cmd/vellum/` | CLI entry point |
| `convert/` | Markdown → HTML/PDF pipeline; unified `Run` router; Backend interface. Math and Mermaid are source preprocessors (`convert/katex.go`, `convert/mermaid.go`), not goldmark extensions. |
| `clipboard/` | Rich pasteboard + Finder file references (macOS). `Write` tries AppKit then pandoc HTML→RTF. |
| `importer/` | Rich-text → Markdown via pandoc; PDF via Poppler |
| `adf/` | Markdown → Confluence ADF (library only; not a `convert.Run` sink) |
| `internal/pandoc/` | HTML → RTF/plain export helper (clipboard fallback; not a public API) |
| `config/` | User configuration loaded from `~/.config/vellum/config.yaml` |
| `mcp/` | MCP server (single `convert` tool; streamable HTTP + stdio) |
| `embed/` | Embedded assets (CSS, HTML templates) |
| `internal/testdeps/` | Test gate for external converters (`VELLUM_REQUIRE_DEPS`) |
| `viewer/` | Localhost daemon (HTML view + chrome + `/mcp`); cached PDF open; macOS default .md handler |

### External dependencies

- **WeasyPrint** (default) — HTML → PDF, BSD-3 (must be on PATH; `brew install weasyprint`)
- **Prince** (opt-in via `backend: prince`) — HTML → PDF, proprietary (must be on PATH)
- **mmdc** — Mermaid CLI for diagram rendering (optional, on PATH)
- **pandoc** — rich-text import and clipboard HTML→RTF fallback (lazy; only when needed)
- **poppler** — PDF import (`pdftoppm`, `pdftotext`; lazy)

## Gate

`cv gate` is the definition of green — gofmt, vet, the suite with
`VELLUM_REQUIRE_DEPS=1`, a skip census locked at 0 non-pasteboard
skips (pasteboard skips must name “pasteboard”), and the CLI
contract. CI installs converters and calls `cv gate`; it holds no gate
logic of its own, so a red build reproduces locally with one command.
`cv bullseye` adds a clean-tree check for convergence.

macOS only. The clipboard, Finder file references, and the viewer are
darwin-only, so a Linux runner would exercise a strict subset while
implying a platform vellum does not support.

## Delivery

Merged to master.

## Followable work

Bullseye targets in `bullseye.yaml` (via the bullseye MCP tools) are the sole record of followable work.
