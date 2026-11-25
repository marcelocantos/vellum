# vellum

Document preparation MCP server — converts GitHub-flavoured Markdown to PDF via [goldmark](https://github.com/yuin/goldmark) and [WeasyPrint](https://www.courtbouillon.org/weasyprint) (or [Prince](https://www.princexml.com/) opt-in), to PowerPoint decks via [pandoc](https://pandoc.org/), and the inverse: rich-text formats (RTF, DOCX, HTML, ODT, EPUB, …) back to Markdown via pandoc.

vellum is primarily an HTTP [Model Context Protocol](https://modelcontextprotocol.io/) server (streamable HTTP at `/mcp` on the brew-service daemon), exposing both conversion directions as a single media-orthogonal `convert` tool for AI agents. It also ships a direct CLI for scripted and interactive use, and a stdio MCP fallback (`vellum --mcp`).

vellum runs on macOS (arm64) and Windows (arm64, amd64). On macOS, `vellum install-viewer` registers **Vellum Viewer** as the default `.md` handler so double-clicking Markdown opens a rendered view (HTML by default). Details: [macOS Markdown viewer](#macos-markdown-viewer).

vellum is the Go-based successor to [mpe2pdf](https://github.com/marcelocantos/mpe2pdf): leaner, single-binary, MCP-first.

## Status

Pre-1.0 and under active development. Interfaces, flags, and output may change between minor releases. Suitable for personal projects and experimentation; not yet recommended for production pipelines.

## Requirements

vellum shells out to external tools at conversion time. Each must be on `PATH` when that path is used:

- **One of two renderer backends:**
  - **[WeasyPrint](https://www.courtbouillon.org/weasyprint)** 60 or later — **default**. BSD-3 licensed, open-source, no commercial entanglement. Install with `brew install weasyprint` (or `pipx install weasyprint`).
  - **[Prince](https://www.princexml.com/download/)** 16.2 or later — opt-in. Proprietary, free for non-commercial use with a first-page watermark; commercial license required for commercial use. Marginally better typography (kerning, optical sizing) and ~6× faster on typical documents.
- **[Node.js](https://nodejs.org/)** — runtime for KaTeX math rendering.
- **[KaTeX](https://katex.org/)** — `npm install -g katex`. Vellum embeds the matching stylesheet and fonts (`scripts/vendor-katex.sh` refreshes them after an upgrade), so rendering never fetches from a CDN.
- **[mermaid-cli](https://github.com/mermaid-js/mermaid-cli)** (`mmdc`) — `brew install mermaid-cli` (or the equivalent on your platform). Required only if your documents contain Mermaid diagrams.
- **[pandoc](https://pandoc.org/)** — `brew install pandoc`. Required for rich-text import (RTF, DOCX, HTML, …) with image extraction and for RTF and PowerPoint file output.
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
   (macOS arm64; Windows arm64/amd64 zips).

   **Windows:** use scoop (`scoop bucket add vellum https://github.com/marcelocantos/vellum`
   then `scoop install vellum/vellum`) or unzip `vellum-<version>-windows-<arch>.zip`
   from the releases page onto `PATH`. Dependencies: `winget install
   JohnMacFarlane.Pandoc OpenJS.NodeJS.LTS MSYS2.MSYS2`; in MSYS2,
   `pacman -S mingw-w64-clang-aarch64-pango mingw-w64-clang-aarch64-poppler`
   (`ucrt-x86_64` on x64) and put its `bin` on `PATH`; `pip install weasyprint`
   with `WEASYPRINT_DLL_DIRECTORIES` set to that `bin`; `npm install -g katex
   @mermaid-js/mermaid-cli`. Start the daemon with `vellum install-service`
   (a per-user logon Scheduled Task; `vellum uninstall-service` removes it).

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
  --to-clipboard      Sugar: file|stdin → clipboard (macOS, Windows)
  --open              Open via the localhost view server (alias for `view`)
  -o <path>           Output path (single input file only); a .pptx
                      extension writes a PowerPoint deck instead of PDF
  --backend <name>    Renderer backend: "weasyprint" (default) or "prince"
  --template <t>      PowerPoint reference deck for .pptx output

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
vellum deck.md -o deck.pptx            # writes a PowerPoint deck
vellum report.md -o report.rtf         # writes a Rich Text Format file
vellum convert --from file --to file deck.md --to-format pptx --template brand.pptx
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
stay in the browser. The open tab watches the source (WebSocket, 5-minute
heartbeat) and reloads after a change has been quiet for a moment. A rename
redirects the tab; a deletion leaves the rendered page up and reports it.
A missing file stays on a waiting page until the file appears. Scroll is
restored best-effort to the same heading, and a focused task checkbox is
restored after reload.
HTML is the default (fast, no WeasyPrint needed for a casual read); pass
`--pdf` for full typography in Preview via a cache file.

Start the daemon with Homebrew (preferred) or in the foreground:

```sh
brew services start vellum
# or: vellum serve-view
```

Each GET converts **one** Markdown path (no link-graph crawl). Relative
`.md`/`.markdown` links are rewritten to same-origin URLs. The served page
adds view chrome (not written into the convert cache): one top bar with a
link to the homepage (recent documents and the Markdown files beside
them), a toggle for the contents panel, the file's name (the document's
title once its heading has scrolled away; click it to return to the top)
and icon buttons to edit the source, copy to the clipboard, download a
PDF and reveal in Finder (Finder's own icon, or a folder on Windows);
a contents panel that marks the section being read, with expand/collapse,
and on a narrow window opens over the page;
a theme control (dark / system / light, persisted in the browser) with
syntax highlighting and Mermaid diagrams recoloured for the dark theme;
prose set in a 760px column, with tables that use the full panel width,
columns sized to their content (wide tables scroll within the panel) and
headings that stay visible until the table scrolls away; SVG (Mermaid
diagrams, and an SVG image that stands alone in a paragraph) drawn as wide
as the panel allows and no larger than its text warrants (see
[SVG sizing in the viewer](#svg-sizing-in-the-viewer)); and a full-viewport
zoom/pan viewer for images and SVG (including Mermaid), and GFM
task-list checkboxes that edit the source file after consent
(once per session or permanently) when you click or Space the box, not
the label text. A toggle names the exact task (its place and source
line) and the version of the file the page shows; if the file has
changed since, nothing is written, the box springs back and the page
says so (reload to see the new version). Task-like lines in code,
diagrams and math are never touched, and tasks in block quotes and
loose lists can be ticked. The toggle adopts the new source stamp without reloading;
an external file-change reload keeps keyboard focus on that box. The pencil
(or the `e` key) opens the Markdown source in a modal CodeMirror 6
editor: GFM syntax highlighting with fenced code highlighted in its own
language, list continuation, search and replace, folding and multiple
cursors, in the viewer's light or dark theme. It opens scrolled to what
the page was showing at the top (rendered blocks carry their source
lines, and positions in between are interpolated, so this holds inside
code blocks and long paragraphs too). Text selected on the page is
selected in the source, matched word for word through the Markdown
around it (emphasis, link URLs, entities, tags, list and task markers);
⌘S / Ctrl+S saves. A save names the version it
edited, so a change made on disk meanwhile is never overwritten
silently: a clean buffer follows the outside edit, and a dirty one asks
whether to overwrite or reload. Esc or Done closes it, asking first
about unsaved changes. The page then shows what the editor was showing,
refreshed if anything was saved, or exactly where it was if the editor
was not scrolled. The editor is embedded in the binary and loads nothing from the
network.

A document you open may not be one you wrote (a cloned repository's
README, a download), so the viewer never runs anything a document
carries. Its HTML is cut down to an allow-list, much like what GitHub
shows for a README: text formatting, headings, lists, tables, images,
links, `<details>`, `<kbd>`, `<sup>`/`<sub>` and the like. Script,
event handlers, `<style>` and `style=` (except table alignment),
`<iframe>`, `<object>`, `<embed>`, forms, `<math>`, `<video>`/
`<audio>`, `<meta>` and `<base>` are removed; links keep only `http`,
`https`, `mailto`, `tel`, `file` and relative URLs, images only
`http`, `https`, relative and `data:image/…`. An `<svg>` written in
the document stays an inline drawing, with its text, links, styles,
gradients, animation and `foreignObject` labels; only its script, event
handlers, `javascript:` links, animation that rewrites a link, and form
controls are removed. Its `<style>` applies to the page, as on any web
page. A Content-Security-Policy
lets only the viewer's own scripts run, so a gap in that list is still
not enough to run anything. An HTML or SVG file a document links to
opens sandboxed: it shows, but its script does not run. Mermaid and
KaTeX output is generated by vellum, not taken from the document, and
still renders. `vellum convert` and MCP outputs keep a document's HTML
as written.

The viewer serves only the folders you open documents from. Opening a
Markdown document from outside the browser page (`vellum view`,
double-clicking it in Finder, `open` on its URL, typing or bookmarking
that URL) serves the git work tree it is in, or else its folder. Opening
any other kind of file does not; a document directly in your home folder (or
a filesystem root) serves only itself. Links, images and requests a page
makes reach only what is already served: anything else, including a
symlink that points out of its folder, is refused with 403, and a page
cannot open a new folder. Reloading a tab that was reached by a link
counts as that link again, so if it is refused, enter its URL in the
address bar instead. The homepage lists only served documents.

To skip that, list folders the viewer always serves in `config.yaml`
(see [Style customisation](#style-customisation) for where it lives):

```yaml
viewer:
  folders:
    - ~/work        # this folder and everything beneath it
    # - /           # everything: turns the rule off
```

Nothing in a listed folder needs opening first: links between
repositories, images, reloads and non-Markdown files all work. A folder
that does not exist on this machine is skipped, so one file can serve
several machines. Restart the viewer after editing it
(`brew services restart vellum`). The cost: any document you view can
link to, and show images from, any file in a listed folder. Documents
still cannot run script.

Served folders are remembered (the newest 64) so open tabs survive a
restart of the server. The same process hosts
streamable HTTP MCP at `/mcp`. The server binds loopback only (override
with `--addr` / `VELLUM_VIEW_ADDR`). Cache health: entries older than 7
days are dropped, then oldest entries are evicted until total size is
under 50 MB. First paint waits on the article text only: the page is
converted without running `mmdc` or KaTeX, so each Mermaid diagram and
math expression arrives as a placeholder (an animated box, or the raw
TeX), and images carry `loading="lazy"`. The page fetches each
placeholder from `/_vellum/fragment` when it scrolls near the viewport
(or on click); the server renders it then, caching the result by content
hash so the same diagram or expression is never rendered twice, and
renders a document's math in one `node` batch. A fragment whose render
fails shows its source, the error, and a Retry button; a fetch that the
daemon did not answer retries by itself. Copy and PDF still convert the
whole document. A revisit reserves each cached diagram's aspect ratio
so nothing below it shifts.

`vellum install-viewer` generates `~/Applications/Vellum Viewer.app`,
registers it with Launch Services, and (with [`duti`](https://github.com/moretension/duti) on `PATH`) sets it as the default handler for Markdown. The app executable is a small Cocoa binary (compiled with clang at install time) that receives Launch Services open-document Apple Events and runs `vellum --open` — a shell-script launcher cannot receive those events. Requires Xcode Command Line Tools. Uninstall with `vellum uninstall-viewer`. Debug log: `~/Library/Logs/vellum-viewer.log`.

With no `-o`, each input file is converted to a sibling `.pdf` with the same base name.

### Inside T3 Code

In the plugin build of [T3 Code](https://github.com/marcelocantos/t3code),
vellum takes over Markdown file links in a chat:

- Clicked at the machine that hosts the thread, the link opens the
  document in your browser, as `vellum view` does.
- Clicked anywhere else (a phone, another machine), it opens the document
  as a card inside T3. The file is read, edited and saved on the machine
  that hosts the thread.

`vellum serve-view` advertises itself to T3 while it runs. Enable
**Vellum** once in T3 Code under **Settings → Plugins**. The card on a
click from elsewhere, and no card when you only point at a link, need a
T3 build with document cards (after 0.7.0); an older build opens the file
its own way instead.

The card shows the same page as the browser, without the actions that
would act on the host instead of your device (copy, PDF, show in Finder).
Inside the card:

- Links to other Markdown files open in the card. Links to other sites
  and other files do not open.
- Images are shown when they are PNG, JPEG, GIF, WebP, AVIF, BMP or ICO
  files in a served folder. SVG files are shown too in a T3 build with
  document cards, where the gateway keeps a file's script from running.
  Images from the web are not shown.
- A save larger than 1 MiB is refused by T3's gateway.

T3 reaches the viewer on a second loopback listener, which defaults to the
view port plus one on macOS and is off elsewhere. `--t3-addr host:port` or
`VELLUM_T3_ADDR` sets it; `off` disables it. That listener serves only
what a card needs: no `/mcp`, no clipboard, no homepage. A request to it
never makes the viewer serve a new folder; only T3 relaying your click on
(or pointer over) a link does.

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
Formats are inferred when omitted. Optional `style`, `backend` and
`template` overlay config for that call. See `docs/agents-guide.md` or
`vellum --help-agent`.

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

Rich-text import and RTF and PowerPoint file output require `pandoc` on `PATH`.
`clipboard` and `file_reference` work on macOS and Windows (CF_HTML/RTF/text and CF_HDROP file lists); other platforms return a clear error.

## PowerPoint output

A `.pptx` output path (or `--to-format pptx` / `to.format: pptx`) writes
a PowerPoint deck through pandoc's pptx writer. The Markdown is carved
into slides by pandoc's rules:

- A YAML title block (`title`, `subtitle`, `author`, `date`) becomes the
  title slide.
- The slide level is the highest heading level followed directly by
  content. Headings at that level start a slide; headings above it
  become section slides; headings below it are headings within a slide.
  A horizontal rule always starts a new slide.
- Lists, tables, images, code blocks, inline and display math (as native
  Office math), footnotes and links carry through. A `::: notes … :::`
  div becomes speaker notes; `::: incremental` / `::: nonincremental`
  and `::: columns` with `::: column` children work as in pandoc.
- Mermaid blocks are rendered to PNG by `mmdc` and placed as pictures.
  A diagram that fails to render stays on its slide as a code block and
  is reported as a soft error.

Templates control the look. When nothing else is selected, the installed
pandoc supplies its default reference deck (including its user-data overrides).
Vellum does not bundle a reference deck; default styling can change with pandoc
upgrades. To use another:

```sh
vellum deck.md -o deck.pptx --template ~/decks/brand.pptx   # a path
vellum deck.md -o deck.pptx --template brand                # ~/.config/vellum/templates/brand.pptx
vellum deck.md -o deck.pptx --template default              # pandoc's default deck
```

The MCP `convert` tool takes the same value as `template`, and
`config.yaml` sets a default with `template:`. A template is any
`.pptx` (or `.potx`) whose slide master has the layouts pandoc looks for
— Title Slide, Section Header, Title and Content, Two Content,
Comparison, Content with Caption, Blank. Save a deck from PowerPoint
with those layouts styled the way you want and point `--template` at it.

pptx is output-only: vellum does not import PowerPoint decks (pandoc has
no pptx reader). Like PDF, pptx cannot go to the `content` or
`clipboard` sinks.

## Style customisation

vellum reads optional defaults from `~/.config/vellum/config.yaml` (or `$XDG_CONFIG_HOME/vellum/config.yaml` if set). The file is optional; if absent, vellum's built-in defaults apply. MCP tool calls can supply a `style` object (and a `backend` string) that overlays each field on top of the config file for that single call.

Example `config.yaml`:

```yaml
backend: weasyprint     # or "prince"; default is weasyprint
template: brand         # pptx reference deck: a path, a name under
                        # ~/.config/vellum/templates/, or "default"

viewer:
  folders:              # folders the viewer always serves (optional);
    - ~/work            # see "macOS Markdown viewer" above

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
| File references (Finder / Explorer) | **Yes** (macOS, Windows) | No | No | No |
| Mermaid diagrams | **Built in** | Via external filter | Via plugin | Usually built in |
| Diagram format per sink | **SVG screen / PNG print** | Manual | Manual | Fixed |
| LaTeX math | **Built in** (KaTeX) | Yes | Varies | Yes |
| CSS-styled PDF | **Zero config** | Yes, with `--pdf-engine=weasyprint` and your own CSS | Yes (Chromium) | Yes |
| Scriptable / headless | Yes | Yes | Yes | No |
| Default `.md` handler | **Yes** (macOS) | No | No | Yes |
| Runtime footprint | Single static binary | Single binary | Node + Chromium | Desktop app |
| Input formats | 8 | **51** | 1 | 1–2 |
| Output formats | 6 <sub>(see roadmap)</sub> | **76** | 1–2 | Several |

**Where pandoc wins, and it isn't close: breadth.** 51 readers and 76
writers against vellum's 8 and 6. If you need DocBook, JATS, or Texinfo,
use pandoc — vellum literally shells out to it for rich-text import.
vellum's claim is a different one: the conversions people actually run all
day, wired into agents and the desktop, with diagrams and math that work
without a configuration session.

### Roadmap: output format expansion

vellum writes Markdown, HTML, PDF, RTF, PowerPoint, and rich clipboard text.
The pandoc export path could add writers for docx, odt, epub, latex, typst,
rst, org, asciidoc, and ipynb.

## SVG sizing in the viewer

A wide diagram squeezed into the 760px text column loses its labels, and a
small one stretched across the window balloons. The viewer sizes each SVG
from its own text:

- It is drawn at its natural width, or wider until its small print reaches
  12px or its typical text reaches 16px (the size of the prose), whichever
  comes first. A drawing whose text is already that large, or that has no
  text, is never drawn larger than natural.
- It is never wider than the panel less its gutters. One wider than the
  text column is centred on the column, as a wide table is.
- "Small print" is the size that all but a tenth of the drawing's
  characters are at least as large as, so a stray watermark does not decide
  the size; "typical" is the median character.

This applies to Mermaid diagrams and to any SVG that stands alone, with
nothing beside it in its block:

- a drawing written into the document as `<svg>` markup, on lines of its
  own or alone in a paragraph;
- an SVG image (`![…](drawing.svg)` on a line of its own), including one
  wrapped in a link (`[![…](drawing.svg)](drawing.svg)`), inside raw HTML
  such as `<figure>`, `<div align="center">` or `<picture>`, or given as a
  `data:image/svg+xml` URL;
- an image from the web, when the viewer itself can fetch it and it is an
  SVG. The page does not wait for it: the viewer measures it after the
  page loads, remembers the result for an hour, and writes it into later
  pages. An image the viewer cannot fetch, such as one behind a sign-in
  only the browser has, keeps its size.

Left as they were: an image or drawing inside a sentence, an image given a
`width` or `height`, a drawing given a width in its `style` or as a
percentage, a diagram with a `vellum:scale` hint, an SVG with no
`viewBox`, and print and PDF output. A style that only keeps the drawing
inside its container does not count as sizing it: `max-width: 100%` (or
more, or `none`) and `width` or `height` of `auto`, as in the common
`max-width: 100%; height: auto`. Such a drawing grows like one with no
style. `test/svg-fit/svg-fit.md` shows each
case.

The text sizes are read on the server, without a browser, from
`font-size` attributes, inline styles and simple `<style>` rules, with
transforms and the `viewBox` scale applied. A size it cannot read (one
set by a pseudo-class rule or `calc()`) is skipped.

## Markdown feature support

- GitHub-Flavoured Markdown: tables, task lists, strikethrough, autolinks.
- Headings, ordered and unordered lists (nested), task lists, definition lists.
- Syntax highlighting via [chroma](https://github.com/alecthomas/chroma) using the GitHub style, across many languages.
- Long-line code wrapping in rendered code blocks.
- Footnotes in the PHP Markdown Extra style.
- Inline (`$...$`) and block (`$$...$$`) LaTeX math via KaTeX, including multi-line matrices. KaTeX's stylesheet and fonts ship inside vellum: the viewer serves them, and PDF and HTML output link vellum's copy under the user cache directory (`~/Library/Caches/vellum/katex/<version>/` on macOS), so math renders with no network. HTML that contains no math omits the stylesheet.
- Mermaid diagrams: flowchart, sequence, class, state, Gantt, ER, pie. HTML/view/content paths embed **SVG** (vector); PDF conversion keeps **PNG at 2×** because Mermaid SVG `foreignObject` labels do not paint in Prince.
- Per-diagram scale hint — place `<!-- vellum:scale 0.6 -->` immediately before a ```` ```mermaid ```` block to apply a `max-width` to the rendered diagram. Useful for keeping a diagram on the same page as its heading.
- Optional table of contents — `style.toc: true` or a `<!-- vellum:toc -->` hint injects a static Contents list (nested heading links, no JavaScript). PDF adds dotted leaders and page numbers.
- YAML front-matter `title` extraction.
- Blockquotes, horizontal rules, images (including base64 data URIs).
- PowerPoint output (`.pptx`) via pandoc — see [PowerPoint output](#powerpoint-output).

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

Markdown
  → Mermaid via mmdc (PNG) inlined as images
  → pandoc (gfm reader → pptx writer, --reference-doc = template)
  → .pptx
```

## Agent guide

An agent-facing reference lives at [`docs/agents-guide.md`](docs/agents-guide.md) and is embedded into the `vellum` binary. Coding agents can read it directly or call `vellum --help-agent` to print usage plus the embedded guide.

## License

Apache 2.0 — see [LICENSE](LICENSE). Third-party dependencies are attributed in [NOTICE](NOTICE).
