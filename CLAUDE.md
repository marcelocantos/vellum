# vellum

Document preparation MCP server — Markdown to PDF via goldmark + WeasyPrint (Prince opt-in).

## Architecture

Go binary with two modes:
- **MCP server** (`vellum` or `vellum --mcp`): stdio-based MCP server exposing document conversion tools
- **CLI** (`vellum convert <input.md>`): direct command-line conversion

### Pipeline

```
Markdown → goldmark (GFM + extensions) → HTML fragment
  → wrap in HTML template (embedded CSS)
  → WeasyPrint (default, BSD-3) or Prince (opt-in, proprietary)
  → PDF
```

### Key packages

| Package | Role |
|---------|------|
| `cmd/vellum/` | CLI entry point |
| `convert/` | Markdown → PDF conversion pipeline; `Backend` interface + WeasyPrint/Prince implementations |
| `convert/extensions/` | Custom goldmark extensions (Mermaid, etc.) |
| `config/` | User configuration loaded from `~/.config/vellum/config.yaml` |
| `mcp/` | MCP server setup and tool registration |
| `embed/` | Embedded assets (CSS, HTML templates) |

### External dependencies

- **WeasyPrint** (default) — HTML → PDF, BSD-3 (must be on PATH; `brew install weasyprint`)
- **Prince** (opt-in via `backend: prince`) — HTML → PDF, proprietary (must be on PATH)
- **mmdc** — Mermaid CLI for diagram rendering (optional, on PATH)

## Delivery

Merged to master.

## TODOs

`docs/TODO.md`
