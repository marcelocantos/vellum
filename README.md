# vellum

Document preparation MCP server — converts GitHub-flavoured Markdown to PDF via [goldmark](https://github.com/yuin/goldmark) and [WeasyPrint](https://www.courtbouillon.org/weasyprint) (or [Prince](https://www.princexml.com/) opt-in), and the inverse: rich-text formats (RTF, DOCX, HTML, ODT, EPUB, …) back to Markdown via [pandoc](https://pandoc.org/).

vellum is primarily an HTTP [Model Context Protocol](https://modelcontextprotocol.io/) server (streamable HTTP at `/mcp` on the brew-service daemon), exposing both conversion directions as a single media-orthogonal `convert` tool for AI agents. It also ships a direct CLI for scripted and interactive use, and a stdio MCP fallback (`vellum --mcp`).

On macOS, `vellum install-viewer` registers **Vellum Viewer** as the default `.md` handler so double-clicking Markdown opens a rendered view (HTML by default). Details: [macOS Markdown viewer](#macos-markdown-viewer).

vellum is the Go-based successor to [mpe2pdf](https://github.com/marcelocantos/mpe2pdf): leaner, single-binary, MCP-first.

## Status

Pre-1.0 and under active development. Interfaces, flags, and output may change between minor releases. Suitable for personal projects and experimentation; not yet recommended for production pipelines.

## Requirements

vellum shells out to external tools at conversion time. Each must be on `PATH` when that path is used:

- **One of two renderer backends:**
  - **[WeasyPrint](https://www.courtbouillon.org/weasyprint)** 60 or later — **default**. BSD-3 licensed, open-source, no commercial entanglement. Install with `brew install weasyprint` (or `pipx install weasyprint`).
  - **[Prince](https://www.princexml.com/download/)** 16.2 or later — opt-in. Proprietary, free for non-commercial use with a first-page watermark; commercial license required for commercial use. Marginally better typography (kerning, optical sizing) and ~6× faster on typical documents.
- **[Node.js](https://nodejs.org/)** — runtime for KaTeX math rendering.
- **[KaTeX](https://katex.org/)** — `npm install -g katex`.
- **[mermaid-cli](https://github.com/mermaid-js/mermaid-cli)** (`mmdc`) — `brew install mermaid-cli` (or the equivalent on your platform). Required only if your documents contain Mermaid diagrams.
- **[pandoc](https://pandoc.org/)** — `brew install pandoc`. Required for rich-text import (RTF, DOCX, HTML, …) with image extraction.
- **[poppler](https://poppler.freedesktop.org/)** — `brew install poppler` (`pdftoppm`, `pdftotext`). Required for PDF import (page images + text).

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

Installing vellum is a multi-step process. Do not stop after `brew install`.

1. **Install the binary** (Homebrew preferred):

```sh
brew install marcelocantos/tap/vellum
```

   Alternatives: `go install github.com/marcelocantos/vellum/cmd/vellum@latest`,
   or a prebuilt binary from the [GitHub releases](https://github.com/marcelocantos/vellum/releases)
   (macOS arm64, Linux x86_64, Linux arm64).

2. **Install runtime dependencies** (WeasyPrint, node, KaTeX, mermaid-cli,
   pandoc, poppler — see [Requirements](#requirements)). The formula pulls
   most of these; `npm install -g katex` is still a separate step.

3. **Start the daemon:**

```sh
brew services start vellum
```

   Confirm it is listening with `lsof -iTCP:18742 -sTCP:LISTEN`. Do **not**
   probe `/mcp` with bare `curl` — MCP only accepts POST with a JSON-RPC
   body, so a plain GET looks like “server not ready”. Non-Homebrew
   installs can run `vellum serve-view` in the foreground.

4. **Register HTTP MCP** at `http://127.0.0.1:18742/mcp` and **restart the
   agent session**. `vellum --mcp` is a stdio fallback only.

   Grok Build:

   ```sh
   grok mcp add --transport http vellum http://localhost:18742/mcp
   ```

   Claude Code:

   ```sh
   claude mcp add --scope user --transport http vellum http://127.0.0.1:18742/mcp
   ```

### Quick start for agentic coding tools

If you use an AI coding agent (Claude Code, Cursor, etc.), paste this prompt to install vellum end-to-end:

> Install vellum from https://github.com/marcelocantos/vellum. This is a multi-step install — do not stop after brew install. Run `brew install marcelocantos/tap/vellum`, `npm install -g katex` if needed, start the service (`brew services start vellum`), confirm it is listening with `lsof -iTCP:18742 -sTCP:LISTEN` (do not curl /mcp), register it as an HTTP MCP server at `http://127.0.0.1:18742/mcp`, then let me know so I can restart the session. After restart, run `vellum --help-agent` and confirm the `convert` tool is callable.

## CLI usage

```
Usage: vellum [options] <input.md...>
       vellum --mcp
       vellum import [options] <file>
       vellum convert --from <media> --to <media> [path|-]
       vellum view [options] <file.md>
       vellum serve-view [--addr host:port]
       vellum install-viewer | uninstall-viewer

Options:
  --help              Show help
  --help-agent        Show help plus the embedded agent guide
  --version           Print version
  --mcp               Run as an MCP server on stdio (fallback)
  --to-clipboard      Sugar: file|stdin → clipboard (macOS)
  --open              Open via the localhost view server (alias for `view`)
  -o <path>           Output path (single input file only)
  --backend <name>    Renderer backend: "weasyprint" (default) or "prince"

Subcommands:
  convert             Media-orthogonal conversion (file, content, clipboard,
                      file_reference). See `vellum convert --help`.
  import              Alias: rich-text → Markdown. See `vellum import --help`.
  view                Open Markdown via the localhost view server
                      (HTML default; --pdf for PDF fidelity)
  serve-view          Run the localhost daemon: Markdown view + HTTP MCP at /mcp
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
brew services start vellum             # view + HTTP MCP daemon (127.0.0.1:18742)
vellum view notes.md                   # open rendered HTML in the browser
vellum install-viewer                  # double-click .md → rendered view
```

### macOS Markdown viewer

`vellum view` / `vellum --open` open Markdown as an **`http://127.0.0.1:18742/…`
URL** on the localhost view server (not `file://`), so in-page `.md` links
stay in the browser and the open tab updates when the source file changes
(WebSocket watch with a 5-minute heartbeat; scroll is restored best-effort
to the same heading). HTML is the default (fast, no WeasyPrint needed for a
casual read); pass `--pdf` for full typography in Preview via a cache file.

Start the daemon with Homebrew (preferred) or in the foreground:

```sh
brew services start vellum
# or: vellum serve-view
```

Each GET converts **one** Markdown path (no link-graph crawl). Relative
`.md`/`.markdown` links are rewritten to same-origin URLs. The served page
adds view chrome (not written into the convert cache): a heading table of
contents with expand/collapse and a « / » sidebar toggle, a toolbar to
download PDF, copy the rendered document to the clipboard, or reveal the
source in Finder, a theme control (dark / system / light, persisted in
the browser), dark-mode article styling with syntax highlighting, and
a full-viewport zoom/pan viewer for images and SVG (including Mermaid),
and GFM task-list checkboxes that edit the source file after consent
(once per session or permanently) when you click or Space the box, not
the label text, without reloading the page. The same process hosts
streamable HTTP MCP at `/mcp`. The server binds loopback only (override
with `--addr` / `VELLUM_VIEW_ADDR`). Cache health: entries older than 7
days are dropped, then oldest entries are evicted until total size is
under 50 MB.

`vellum install-viewer` generates `~/Applications/Vellum Viewer.app`,
registers it with Launch Services, and (with [`duti`](https://github.com/moretension/duti) on `PATH`) sets it as the default handler for Markdown. The app executable is a small Cocoa binary (compiled with clang at install time) that receives Launch Services open-document Apple Events and runs `vellum --open` — a shell-script launcher cannot receive those events. Requires Xcode Command Line Tools. Uninstall with `vellum uninstall-viewer`. Debug log: `~/Library/Logs/vellum-viewer.log`.

With no `-o`, each input file is converted to a sibling `.pdf` with the same base name.

## MCP server

The brew-service daemon hosts streamable HTTP MCP at
`http://127.0.0.1:18742/mcp` (same process as the Markdown view server).
Start it with `brew services start vellum` (or `vellum serve-view`).

```json
{
  "mcpServers": {
    "vellum": {
      "transport": "http",
      "url": "http://127.0.0.1:18742/mcp"
    }
  }
}
```

`vellum --mcp` remains as a stdio fallback for clients that cannot speak
HTTP. Prefer the HTTP registration so each agent session does not spawn
another process.

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
  toc: true
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
| `toc`                   | `false`          | When `true`, injects a static Contents list (nested heading links, depth h1–h3) at the front of the document. PDF adds dotted leaders and page numbers via paged-media CSS. A `<!-- vellum:toc -->` hint in the Markdown also requests a Contents block and wins for placement. No JavaScript. |
| `hyphenate`             | `false`          | Enable automatic word hyphenation. Works out-of-the-box on WeasyPrint (Pyphen is bundled); on Prince it requires installing a hyphenation dictionary separately |
| `lang`                  | `""`             | Document language as a BCP-47 tag (e.g. `en`, `en-GB`, `de`). Lands on `<html lang="…">`. Required for hyphenation; defaults to `en` when `hyphenate: true` and `lang` is empty |
| `pdfa`                  | `""`             | PDF/A archival profile (e.g. `PDF/A-1b`, `PDF/A-3b`). Empty produces standard PDF. WeasyPrint also accepts PDF/X and PDF/UA variants here |

CSS-valued fields take any valid CSS for their property; values are interpolated as-is. Boolean fields take YAML true/false. Per-call values from MCP tools take precedence over the config file, which takes precedence over the built-in defaults.

## Why vellum

Most Markdown converters assume a human at a keyboard. vellum assumes an
agent at an API — and then makes the human case better anyway.

**Agent-native, not agent-adapted.** vellum is a Model Context Protocol
server first and a CLI second. One `convert` tool covers every direction,
so an agent learns a single call shape instead of a flag vocabulary. No
wrapper scripts, no temp-file choreography, no parsing another tool's
stdout.

**The clipboard is a first-class medium, in both directions.** Convert
*to* the pasteboard as rich text and paste formatted output straight into
Mail, Slack, or Pages. Convert *from* the pasteboard — copy a formatted
region out of Word and get Markdown back. On macOS, copy files in Finder
and convert them by reference, or hand the output back as a Finder file
reference. Every other tool in this space treats conversion as strictly
file-to-file.

**Diagrams and math that survive the trip to print.** Mermaid and LaTeX
work out of the box with no filter configuration. Diagrams are rendered
per destination: crisp **SVG** for HTML and on-screen viewing, **PNG at
2×** for PDF, because Mermaid's SVG `foreignObject` labels do not paint
in print engines. Getting that right by hand is a genuinely annoying
afternoon.

**Print-quality PDF with nothing to configure.** A tuned stylesheet ships
in the binary — GitHub-style syntax highlighting, footnotes, wrapped code
blocks, page-break control. Override any of it with your own CSS when you
want to.

**One static binary.** No Haskell runtime, no `node_modules`, no bundled
Chromium. Install it with `brew install`, or drop the binary on a box.

**Native on the desktop too.** `vellum install-viewer` makes rendered
Markdown the default double-click behaviour on macOS, with a path-stable
render cache that reloads in the same browser tab.

## How vellum compares

| | **vellum** | **pandoc** | **Node HTML→PDF**<br><sub>md-to-pdf, markdown-pdf</sub> | **GUI editors**<br><sub>Typora, Marked 2</sub> |
|---|---|---|---|---|
| MCP server for agents | **Yes** — single `convert` tool | No | No | No |
| Clipboard as a source | **Yes** — rich text in | No | No | Paste-only |
| Clipboard as a sink | **Yes** — rich text out | No | No | Copy-as-HTML |
| Finder file references | **Yes** (macOS) | No | No | No |
| Mermaid diagrams | **Built in** | Via external filter | Via plugin | Usually built in |
| Diagram format per sink | **SVG screen / PNG print** | Manual | Manual | Fixed |
| LaTeX math | **Built in** (KaTeX) | Yes | Varies | Yes |
| CSS-styled PDF | **Zero config** | Yes, with `--pdf-engine=weasyprint` and your own CSS | Yes (Chromium) | Yes |
| Scriptable / headless | Yes | Yes | Yes | No |
| Default `.md` handler | **Yes** (macOS) | No | No | Yes |
| Runtime footprint | Single static binary | Single binary | Node + Chromium | Desktop app |
| Input formats | 8 | **51** | 1 | 1–2 |
| Output formats | 4 <sub>(see roadmap)</sub> | **76** | 1–2 | Several |

**Where pandoc wins, and it isn't close: breadth.** 51 readers and 76
writers against vellum's 8 and 4. If you need DocBook, JATS, or Texinfo,
use pandoc — vellum literally shells out to it for rich-text import.
vellum's claim is a different one: the conversions people actually run all
day, wired into agents and the desktop, with diagrams and math that work
without a configuration session.

### Roadmap: output format expansion

vellum currently writes Markdown, HTML, PDF, and rich text. Because
pandoc is already a dependency, adding a writer arm exposes its full
catalogue — docx, odt, rtf, epub, pptx, latex, typst, rst, org, asciidoc,
ipynb and more — closing the loop with the existing DOCX and RTF import.
This is **not yet released**; the table above reflects what ships today.

## Markdown feature support

- GitHub-Flavoured Markdown: tables, task lists, strikethrough, autolinks.
- Headings, ordered and unordered lists (nested), task lists, definition lists.
- Syntax highlighting via [chroma](https://github.com/alecthomas/chroma) using the GitHub style, across many languages.
- Long-line code wrapping in rendered code blocks.
- Footnotes in the PHP Markdown Extra style.
- Inline (`$...$`) and block (`$$...$$`) LaTeX math via KaTeX, including multi-line matrices.
- Mermaid diagrams: flowchart, sequence, class, state, Gantt, ER, pie. HTML/view/content paths embed **SVG** (vector); PDF conversion keeps **PNG at 2×** because Mermaid SVG `foreignObject` labels do not paint in Prince.
- Per-diagram scale hint — place `<!-- vellum:scale 0.6 -->` immediately before a ```` ```mermaid ```` block to apply a `max-width` to the rendered diagram. Useful for keeping a diagram on the same page as its heading.
- Optional table of contents — `style.toc: true` or a `<!-- vellum:toc -->` hint injects a static Contents list (nested heading links, no JavaScript). PDF adds dotted leaders and page numbers.
- YAML front-matter `title` extraction.
- Blockquotes, horizontal rules, images (including base64 data URIs).

## Pipeline

```
Markdown
  → math/mermaid preprocessors
  → goldmark (GFM + extensions)
  → KaTeX (server-side, via Node.js)
  → Mermaid via mmdc: SVG (HTML/view/content) or PNG @2× (PDF)
  → HTML template with embedded CSS
  → WeasyPrint (default) or Prince (opt-in) when targeting PDF
  → PDF (or HTML / view / clipboard sink)
```

## Agent guide

An agent-facing reference lives at [`docs/agents-guide.md`](docs/agents-guide.md) and is embedded into the `vellum` binary. Coding agents can read it directly or call `vellum --help-agent` to print usage plus the embedded guide.

## License

Apache 2.0 — see [LICENSE](LICENSE). Third-party dependencies are attributed in [NOTICE](NOTICE).
