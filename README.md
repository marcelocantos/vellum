# vellum

Document preparation MCP server — converts GitHub-flavoured Markdown to PDF via [goldmark](https://github.com/yuin/goldmark) and [Prince](https://www.princexml.com/).

vellum is primarily a stdio [Model Context Protocol](https://modelcontextprotocol.io/) server, exposing Markdown-to-PDF conversion as a tool for AI agents. It also ships a direct CLI for scripted and interactive use.

vellum is the Go-based successor to [mpe2pdf](https://github.com/marcelocantos/mpe2pdf): leaner, single-binary, MCP-first.

## Status

Pre-1.0 and under active development. Interfaces, flags, and output may change between minor releases. Suitable for personal projects and experimentation; not yet recommended for production pipelines.

## Requirements

vellum shells out to a handful of external tools at conversion time. All must be on `PATH`:

- **[Prince](https://www.princexml.com/download/)** 16.2 or later — HTML to PDF. Proprietary; free for non-commercial use.
- **[Node.js](https://nodejs.org/)** — runtime for KaTeX math rendering.
- **[KaTeX](https://katex.org/)** — `npm install -g katex`.
- **[mermaid-cli](https://github.com/mermaid-js/mermaid-cli)** (`mmdc`) — `brew install mermaid-cli` (or the equivalent on your platform). Required only if your documents contain Mermaid diagrams.

### Chromium for mmdc

`mmdc` uses Puppeteer to drive a headless Chromium. On first run it may fail with a message naming the exact `chrome-headless-shell` version it expects. Install it into the `mermaid-cli` prefix:

```sh
cd /opt/homebrew/Cellar/mermaid-cli/*/libexec
npx puppeteer browsers install chrome-headless-shell@<version>
```

Substitute `<version>` with the value printed in the error message.

## Installation

### Homebrew

```sh
brew install marcelocantos/tap/vellum
```

### go install

```sh
go install github.com/marcelocantos/vellum/cmd/vellum@latest
```

### Binary download

Prebuilt binaries for macOS arm64, Linux x86_64, and Linux arm64 are attached to each [GitHub release](https://github.com/marcelocantos/vellum/releases).

### Quick start for agentic coding tools

If you use an AI coding agent (Claude Code, Cursor, etc.), paste this prompt to install vellum end-to-end:

> Install vellum from https://github.com/marcelocantos/vellum. Run `brew install marcelocantos/tap/vellum`, register it as a stdio MCP server with `claude mcp add --scope user vellum -- vellum --mcp`, then restart this session. After the restart, read the agent guide at `docs/agents-guide.md` in the vellum repo (or run `vellum --help-agent` locally) and confirm the `convert` tool is callable.

## CLI usage

```
Usage: vellum [options] <input.md...>
       vellum --mcp

Options:
  --help       Show help
  --version    Print version
  --mcp        Run as an MCP server on stdio
  -o <path>    Output PDF path (single input file only)
```

Examples:

```sh
vellum report.md                    # writes report.pdf
vellum -o out.pdf report.md         # explicit output path
vellum ch1.md ch2.md ch3.md         # batch conversion
```

With no `-o`, each input file is converted to a sibling `.pdf` with the same base name.

## MCP server

Run vellum as an MCP server over stdio:

```sh
vellum --mcp
```

Configure it in any MCP-capable client (for example, Claude Code's `.mcp.json`):

```json
{
  "mcpServers": {
    "vellum": {
      "command": "vellum",
      "args": ["--mcp"]
    }
  }
}
```

The server exposes a single tool, `convert`, which accepts a batch of file pairs:

```json
{
  "files": [
    { "input": "/absolute/path/to/doc.md", "output": "/absolute/path/to/doc.pdf" }
  ]
}
```

`output` is optional; when omitted, vellum writes alongside the input with the extension replaced by `.pdf`. Paths should be absolute.

## Features

- GitHub-Flavoured Markdown: tables, task lists, strikethrough, autolinks.
- Headings, ordered and unordered lists (nested), task lists, definition lists.
- Syntax highlighting via [chroma](https://github.com/alecthomas/chroma) using the GitHub style, across many languages.
- Long-line code wrapping in rendered code blocks.
- Footnotes in the PHP Markdown Extra style.
- Inline (`$...$`) and block (`$$...$$`) LaTeX math via KaTeX, including multi-line matrices.
- Mermaid diagrams: flowchart, sequence, class, state, Gantt, ER, pie.
- Per-diagram scale hint — place `<!-- vellum:scale 0.6 -->` immediately before a ```` ```mermaid ```` block to apply a `max-width` to the rendered diagram. Useful for keeping a diagram on the same page as its heading.
- YAML front-matter `title` extraction.
- Blockquotes, horizontal rules, images (including base64 data URIs).

## Pipeline

```
Markdown
  → math/mermaid preprocessors
  → goldmark (GFM + extensions)
  → KaTeX (server-side, via Node.js)
  → Mermaid PNG (via mmdc)
  → HTML template with embedded CSS
  → Prince
  → PDF
```

## Agent guide

An agent-facing reference lives at [`docs/agents-guide.md`](docs/agents-guide.md) and is embedded into the `vellum` binary. Coding agents can read it directly or call `vellum --help-agent` to print usage plus the embedded guide.

## License

Apache 2.0 — see [LICENSE](LICENSE). Third-party dependencies are attributed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
