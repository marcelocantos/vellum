# vellum

Document preparation MCP server — converts GitHub-flavoured Markdown to PDF via [goldmark](https://github.com/yuin/goldmark) and [Prince](https://www.princexml.com/).

vellum is primarily a stdio [Model Context Protocol](https://modelcontextprotocol.io/) server, exposing Markdown-to-PDF conversion as a tool for AI agents. It also ships a direct CLI for scripted and interactive use.

vellum is the Go-based successor to [mpe2pdf](https://github.com/marcelocantos/mpe2pdf): leaner, single-binary, MCP-first.

## Status

Pre-1.0 and under active development. Interfaces, flags, and output may change between minor releases. Suitable for personal projects and experimentation; not yet recommended for production pipelines.

## Requirements

vellum shells out to external tools at conversion time. All must be on `PATH`:

- **One of two renderer backends:**
  - **[WeasyPrint](https://www.courtbouillon.org/weasyprint)** 60 or later — **default**. BSD-3 licensed, open-source, no commercial entanglement. Install with `brew install weasyprint` (or `pipx install weasyprint`).
  - **[Prince](https://www.princexml.com/download/)** 16.2 or later — opt-in. Proprietary, free for non-commercial use with a first-page watermark; commercial license required for commercial use. Marginally better typography (kerning, optical sizing) and ~6× faster on typical documents.
- **[Node.js](https://nodejs.org/)** — runtime for KaTeX math rendering.
- **[KaTeX](https://katex.org/)** — `npm install -g katex`.
- **[mermaid-cli](https://github.com/mermaid-js/mermaid-cli)** (`mmdc`) — `brew install mermaid-cli` (or the equivalent on your platform). Required only if your documents contain Mermaid diagrams.

### Switching to Prince

vellum uses WeasyPrint by default. To opt into Prince either set `backend: prince` in your config file (see [Style customisation](#style-customisation) for the file location), pass `--backend prince` on the CLI, or supply `"backend": "prince"` in an MCP tool call.

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
  --help              Show help
  --help-agent        Show help plus the embedded agent guide
  --version           Print version
  --mcp               Run as an MCP server on stdio
  --to-clipboard      Render and place RTF + HTML + plain text on the system
                      clipboard (single input file; macOS only)
  -o <path>           Output PDF path (single input file only)
  --backend <name>    Renderer backend: "weasyprint" (default) or "prince"
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

The `convert` and `convert_to_clipboard` tools also accept an optional `style` object whose fields overlay the user's config-file defaults for that call only. See [Style customisation](#style-customisation) below for the field list.

## Style customisation

vellum reads optional defaults from `~/.config/vellum/config.yaml` (or `$XDG_CONFIG_HOME/vellum/config.yaml` if set). The file is optional; if absent, vellum's built-in defaults apply. MCP tool calls can supply a `style` object (and a `backend` string) that overlays each field on top of the config file for that single call.

Example `config.yaml`:

```yaml
backend: weasyprint     # or "prince"; default is weasyprint

style:
  font_size: 14px
  line_height: 1.4
  font_family: "Georgia, serif"
  code_font_family: "Menlo, monospace"
  page_size: A4
  page_margin: 1cm
  page_first_top_margin: 1.5cm
  page_numbers: true
  running_head: true
  bookmarks: true
  hyphenate: true
  lang: en
  pdfa: PDF/A-3b
```

| Field                   | Default          | Notes                                     |
|-------------------------|------------------|-------------------------------------------|
| `font_size`             | `14px`           | Body font size (any CSS length)           |
| `line_height`           | `1.5`            | Body line height                          |
| `font_family`           | system sans      | Body font-family (CSS value, e.g. `Georgia, serif`) |
| `code_font_family`      | system monospace | Applied to `code` and `pre`               |
| `page_size`             | `A4`             | `@page size` (e.g. `A4`, `Letter`)        |
| `page_margin`           | `1cm`            | `@page margin`                            |
| `page_first_top_margin` | `1.5cm`          | `@page :first margin-top`                 |
| `page_numbers`          | `false`          | When `true`, prints the page number at the bottom-centre of every page |
| `running_head`          | `false`          | When `true`, prints the most-recent `<h1>` text at the top-centre of every page |
| `bookmarks`             | `true`           | When `true`, emits a PDF outline (sidebar in PDF readers) from `<h1>`–`<h6>`. Set to `false` to suppress |
| `hyphenate`             | `false`          | Enable automatic word hyphenation. Works out-of-the-box on WeasyPrint (Pyphen is bundled); on Prince it requires installing a hyphenation dictionary separately |
| `lang`                  | `""`             | Document language as a BCP-47 tag (e.g. `en`, `en-GB`, `de`). Lands on `<html lang="…">`. Required for hyphenation; defaults to `en` when `hyphenate: true` and `lang` is empty |
| `pdfa`                  | `""`             | PDF/A archival profile (e.g. `PDF/A-1b`, `PDF/A-3b`). Empty produces standard PDF. WeasyPrint also accepts PDF/X and PDF/UA variants here |

CSS-valued fields take any valid CSS for their property; values are interpolated as-is. Boolean fields take YAML true/false. Per-call values from MCP tools take precedence over the config file, which takes precedence over the built-in defaults.

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
  → WeasyPrint (default) or Prince (opt-in)
  → PDF
```

## Agent guide

An agent-facing reference lives at [`docs/agents-guide.md`](docs/agents-guide.md) and is embedded into the `vellum` binary. Coding agents can read it directly or call `vellum --help-agent` to print usage plus the embedded guide.

## License

Apache 2.0 — see [LICENSE](LICENSE). Third-party dependencies are attributed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
