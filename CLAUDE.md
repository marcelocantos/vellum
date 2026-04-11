# vellum

Document preparation MCP server — Markdown to PDF via goldmark + Prince.

## Architecture

Go binary with two modes:
- **MCP server** (`vellum` or `vellum --mcp`): stdio-based MCP server exposing document conversion tools
- **CLI** (`vellum convert <input.md>`): direct command-line conversion

### Pipeline

```
Markdown → goldmark (GFM + extensions) → HTML fragment
  → wrap in HTML template (embedded CSS) → Prince → PDF
```

### Key packages

| Package | Role |
|---------|------|
| `cmd/vellum/` | CLI entry point |
| `convert/` | Markdown → PDF conversion pipeline |
| `convert/extensions/` | Custom goldmark extensions (Mermaid, etc.) |
| `mcp/` | MCP server setup and tool registration |
| `embed/` | Embedded assets (CSS, HTML templates) |

### External dependencies

- **Prince** — HTML → PDF (must be on PATH)
- **mmdc** — Mermaid CLI for diagram SVG rendering (optional, on PATH)

## Delivery

Merged to master.

## TODOs

`docs/TODO.md`
