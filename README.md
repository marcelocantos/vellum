# vellum

Document preparation MCP server — converts GitHub-flavoured Markdown to PDF via [goldmark](https://github.com/yuin/goldmark) and [WeasyPrint](https://www.courtbouillon.org/weasyprint) (or [Prince](https://www.princexml.com/) opt-in), and the inverse: rich-text formats (RTF, DOCX, HTML, ODT, EPUB, …) back to Markdown via [pandoc](https://pandoc.org/).

vellum is primarily a stdio [Model Context Protocol](https://modelcontextprotocol.io/) server, exposing both conversion directions as a single media-orthogonal `convert` tool for AI agents. It also ships a direct CLI for scripted and interactive use.

On macOS, `vellum install-viewer` registers **Vellum Viewer** as the default `.md` handler so double-clicking Markdown opens a rendered view (HTML by default). Details: [macOS Markdown viewer](#macos-markdown-viewer).

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
- **[pandoc](https://pandoc.org/)** — `brew install pandoc`. Required only for rich-text import paths (`from` clipboard or non-Markdown files).

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

> Install vellum from https://github.com/marcelocantos/vellum. Run `brew install marcelocantos/tap/vellum`, register it as a stdio MCP server (`vellum --mcp`), then let me know so I can restart the session. After restart, run `vellum --help-agent` and confirm the `convert` tool is callable.

## CLI usage

```
Usage: vellum [options] <input.md...>
       vellum --mcp
       vellum import [options] <file>
       vellum convert --from <media> --to <media> [path|-]
       vellum view [options] <file.md>
       vellum install-viewer | uninstall-viewer

Options:
  --help              Show help
  --help-agent        Show help plus the embedded agent guide
  --version           Print version
  --mcp               Run as an MCP server on stdio
  --to-clipboard      Sugar: file|stdin → clipboard (macOS)
  --open              Render to cache and open (alias for `view`)
  -o <path>           Output path (single input file only)
  --backend <name>    Renderer backend: "weasyprint" (default) or "prince"

Subcommands:
  convert             Media-orthogonal conversion (file, content, clipboard,
                      file_reference). See `vellum convert --help`.
  import              Alias: rich-text → Markdown. See `vellum import --help`.
  view                Render Markdown to a cache location and open it
                      (HTML default; --pdf for PDF fidelity)
  install-viewer      Install Vellum Viewer.app as the default .md handler
  uninstall-viewer    Remove Vellum Viewer.app
```

Examples:

```sh
vellum report.md                       # writes report.pdf
vellum convert --from file --to clipboard report.md
echo '# Hi' | vellum convert --from content --to clipboard
vellum convert --from clipboard --to content
vellum convert --from file --to content notes.docx
vellum import doc.docx                 # sugar → Markdown on stdout
vellum view notes.md                   # open rendered HTML in the browser
vellum install-viewer                  # double-click .md → rendered view
```

### macOS Markdown viewer

`vellum view` / `vellum --open` renders to a **cache** keyed by source path
+ mtime (never littering a PDF next to the source) and opens the result.
HTML is the default (fast, no WeasyPrint needed for a casual read); pass
`--pdf` for full typography in Preview. The cache is pruned on each view:
entries older than 7 days are dropped, then oldest entries are evicted
until total size is under 50 MB.

`vellum install-viewer` generates `~/Applications/Vellum Viewer.app`,
registers it with Launch Services, and (with [`duti`](https://github.com/moretension/duti) on `PATH`) sets it as the default handler for Markdown. The app executable is a small Cocoa binary (compiled with clang at install time) that receives Launch Services open-document Apple Events and runs `vellum --open` — a shell-script launcher cannot receive those events. Requires Xcode Command Line Tools. Uninstall with `vellum uninstall-viewer`. Debug log: `~/Library/Logs/vellum-viewer.log`.

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

The server exposes a **single** tool, `convert`, with media-orthogonal
`from` / `to` (media: `file`, `content`, `clipboard`, `file_reference`).
Formats are inferred when omitted. Optional `style` and `backend` overlay
config for that call. See `docs/agents-guide.md` or `vellum --help-agent`.

```json
{
  "from": { "media": "content", "content": "# Hi\n" },
  "to": { "media": "clipboard" }
}
```

Legacy batch sugar still works for Markdown → PDF:

```json
{
  "files": [
    { "input": "/absolute/path/to/doc.md", "output": "/absolute/path/to/doc.pdf" }
  ]
}
```

Rich-text import paths require `pandoc` on `PATH`. `clipboard` and
`file_reference` are macOS-only.

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
