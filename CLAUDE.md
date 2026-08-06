# vellum

Document preparation MCP server — media-orthogonal conversion (file,
content, clipboard, file_reference) via goldmark + WeasyPrint (Prince
opt-in) and pandoc for rich-text import.

## Architecture

Go binary with two modes:
- **MCP server** (`vellum --mcp`): single `convert` tool with `from`/`to` media
- **CLI** (`vellum convert --from … --to …`; sugars for bare `.md`, `--to-clipboard`, `import`)

### Pipeline

```
from media → normalise (pandoc import and/or goldmark HTML)
  → to media (file / content / clipboard rich / file_reference)
```

Markdown → PDF path still: goldmark → HTML template → WeasyPrint/Prince.

### Key packages

| Package | Role |
|---------|------|
| `cmd/vellum/` | CLI entry point |
| `convert/` | Markdown → HTML/PDF pipeline; unified `Run` router; Backend interface |
| `convert/extensions/` | Custom goldmark extensions (Mermaid, etc.) |
| `clipboard/` | Rich pasteboard + Finder file references (macOS) |
| `importer/` | Rich-text → Markdown via pandoc |
| `config/` | User configuration loaded from `~/.config/vellum/config.yaml` |
| `mcp/` | MCP server (single `convert` tool) |
| `embed/` | Embedded assets (CSS, HTML templates) |
| `internal/testdeps/` | Test gate for external converters (`VELLUM_REQUIRE_DEPS`) |
| `viewer/` | Cached render + open; macOS default .md handler |

### External dependencies

- **WeasyPrint** (default) — HTML → PDF, BSD-3 (must be on PATH; `brew install weasyprint`)
- **Prince** (opt-in via `backend: prince`) — HTML → PDF, proprietary (must be on PATH)
- **mmdc** — Mermaid CLI for diagram rendering (optional, on PATH)
- **pandoc** — rich-text import / clipboard → Markdown (lazy; only when needed)

## Gate

`cv gate` is the definition of green — gofmt, vet, the suite with
`VELLUM_REQUIRE_DEPS=1`, a skip census locked at 0, and the CLI
contract. CI installs converters and calls `cv gate`; it holds no gate
logic of its own, so a red build reproduces locally with one command.
`cv bullseye` adds a clean-tree check for convergence.

macOS only. The clipboard, Finder file references, and the viewer are
darwin-only, so a Linux runner would exercise a strict subset while
implying a platform vellum does not support.

## Delivery

Merged to master.

## TODOs

`docs/TODO.md`
