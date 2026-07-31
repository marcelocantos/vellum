# vellum agent guide

vellum has two directions:

1. **Markdown → PDF.** GitHub-flavoured Markdown (with KaTeX math and
   Mermaid diagrams) is typeset to PDF via WeasyPrint (default, BSD-3)
   or Prince (opt-in, proprietary).
2. **Rich text → Markdown.** RTF, DOCX, HTML, ODT, EPUB, LaTeX, and
   anything else pandoc supports is converted back to GitHub-Flavoured
   Markdown via pandoc. Source can be a file or the system clipboard.

Both directions run as either a command-line tool or as MCP (Model
Context Protocol) tools over stdio.

## Installation

**Installing vellum is a multi-step process. All of the following steps
must succeed before vellum is usable — do not stop after `brew install`.**

1. **Install the binary.**

   ```sh
   brew install marcelocantos/tap/vellum
   ```

   Or, if Homebrew is not available:

   ```sh
   go install github.com/marcelocantos/vellum/cmd/vellum@latest
   ```

2. **Install the runtime dependencies.** vellum shells out to external
   tools. All must be on `PATH` before the first conversion:

   ```sh
   # Renderer: WeasyPrint (default, BSD-3). Alternatively/additionally
   # install Prince — proprietary, opt-in via --backend prince or config.
   brew install weasyprint
   # Optional: Prince from https://www.princexml.com/download/

   brew install node
   npm install -g katex
   brew install mermaid-cli

   # Pandoc for the inverse direction (rich text → Markdown via
   # rich-text import / clipboard → Markdown via convert).
   brew install pandoc
   ```

   `mmdc` requires a specific Chromium version on first run. If it
   fails with a "Could not find Chrome" message, install the exact
   version it names:

   ```sh
   cd /opt/homebrew/Cellar/mermaid-cli/*/libexec
   npx puppeteer browsers install chrome-headless-shell@<version>
   ```

3. **Register vellum as an MCP server.** For Claude Code, run the
   one-liner below. This writes a user-scope entry to `~/.claude.json`
   so the server is available in every project:

   ```sh
   claude mcp add --scope user vellum -- vellum --mcp
   ```

   For other MCP clients, add this block to the client's MCP config
   (for example, `.mcp.json` in the project root):

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

   vellum is a **stdio** MCP server, not HTTP. It is spawned per
   connection by the MCP client. There is no daemon to start, no port
   to check, and no `brew services` entry. The Homebrew formula
   installs a thin shell wrapper as `vellum` that prepends the
   canonical tool dirs (`#{HOMEBREW_PREFIX}/bin`, `/usr/local/bin`,
   `$HOME/.cargo/bin`, etc.) before exec'ing the real binary, so
   `node`, `mmdc`, and `prince` resolve regardless of how the MCP
   client's environment was set up.

4. **Restart the agent session.** MCP client config changes are only
   picked up on session start. The current session will not see vellum
   until it is restarted.

5. **Verify the install.** After the session restart, confirm that
   vellum is reachable end-to-end:

   - Check the binary: `vellum --version` should print the installed
     version.
   - Check the runtime deps: `vellum --help-agent` prints this guide;
     the first real conversion will fail fast with a readable error if
     any dependency is missing.
   - Call a tool: convert a trivial one-line Markdown string with
     `convert` (`from.media=content`, `to.media=content`) or write a
     one-line `.md` file to PDF (`from.media=file`, `to.media=file`)
     and confirm the structured result. If the call returns an error,
     report the error text verbatim — it almost always names the
     missing dependency or the bad input.

If any of these steps fails, installation is not complete. Report the
failing step and its error to the user — do not proceed as if vellum
is ready.

## Preferred invocation

When running inside an AI agent, prefer the MCP interface over the CLI.
Start vellum with `vellum --mcp` and call the single `convert` tool.
The CLI (`vellum convert --from … --to …`) is fine for interactive use;
MCP returns structured results (paths, content, errors) that are easier
to consume programmatically.

## Tool schema

The MCP server exposes **one** tool: `convert`. Media (transport) is
orthogonal to document format.

### Media

| Media | Meaning |
|-------|---------|
| `file` | Path on disk (read or write) |
| `content` | Inline text in the request/response |
| `clipboard` | System pasteboard rich text (RTF+HTML+plain out; RTF/HTML in) |
| `file_reference` | Finder-style pasteboard file URLs |

### `convert` — unified media conversion

```json
{
  "from": {
    "media": "file|content|clipboard|file_reference",
    "path": "…",
    "paths": ["…"],
    "content": "…",
    "format": "…"
  },
  "to": {
    "media": "file|content|clipboard|file_reference",
    "path": "…",
    "format": "…"
  },
  "files": [{ "input": "…", "output": "…" }],
  "style": { },
  "backend": "weasyprint|prince"
}
```

Provide **either** `from`+`to`, **or** `files` (legacy Markdown→PDF batch
sugar). Formats are inferred aggressively when omitted:

| Situation | Default |
|-----------|---------|
| `from.media=file` | Extension (`.md`→markdown, `.docx`→docx, …) |
| `from.media=content` | `markdown` |
| `from.media=clipboard` | RTF preferred, else HTML |
| `to.media=clipboard` | rich (RTF+HTML+plain) |
| `to.media=content` | `markdown` |
| `to.media=file` from markdown | `pdf` |
| `to.media=file` from rich-text | `markdown` |

**Disallowed (intractable):** `to.media` of `content` or `clipboard`
with `format` `pdf`. Everything else the pipeline can implement is
allowed. `clipboard` and `file_reference` require macOS.

#### Examples

Markdown string → clipboard (typical agent path — **no temp file**):

```json
{
  "name": "convert",
  "arguments": {
    "from": { "media": "content", "content": "## Status\n\n- Item one\n" },
    "to": { "media": "clipboard" }
  }
}
```

Clipboard rich text → Markdown:

```json
{
  "name": "convert",
  "arguments": {
    "from": { "media": "clipboard" },
    "to": { "media": "content" }
  }
}
```

File → PDF:

```json
{
  "name": "convert",
  "arguments": {
    "from": { "media": "file", "path": "/abs/path/to/report.md" },
    "to": { "media": "file", "path": "/abs/path/to/report.pdf" }
  }
}
```

DOCX → Markdown content:

```json
{
  "name": "convert",
  "arguments": {
    "from": { "media": "file", "path": "/abs/path/to/doc.docx" },
    "to": { "media": "content" }
  }
}
```

Markdown → PDF as Finder file reference:

```json
{
  "name": "convert",
  "arguments": {
    "from": { "media": "file", "path": "/abs/in.md" },
    "to": { "media": "file_reference", "path": "/abs/out.pdf" }
  }
}
```

Legacy batch sugar (Markdown → PDF files):

```json
{
  "name": "convert",
  "arguments": {
    "files": [
      { "input": "/abs/a.md" },
      { "input": "/abs/b.md", "output": "/abs/out/b.pdf" }
    ]
  }
}
```

Response shape:

```json
{
  "from_media": "content",
  "to_media": "clipboard",
  "from_format": "markdown",
  "to_format": "rich",
  "paths": [],
  "content": "",
  "errors": []
}
```

When `to.media` is `content`, the Markdown (or HTML) is in `content` and
also in the tool's text message.

Rich-text import paths require `pandoc` on `PATH`. Clipboard writes
commit the pasteboard before return — no race window for paste.

**Migration** from older tool names (removed):

| Old | New |
|-----|-----|
| `convert_to_clipboard({content})` | `from: {media:content, content}, to: {media:clipboard}` |
| `convert_to_clipboard({input})` | `from: {media:file, path}, to: {media:clipboard}` |
| `convert_from_clipboard({})` | `from: {media:clipboard}, to: {media:content}` |
| `import({input})` | `from: {media:file, path}, to: {media:content}` |
| `convert({files:[…]})` | still accepted as sugar; or `from`/`to` file media |

## CLI: convert and sugars

Primary form:

```sh
vellum convert --from <media> --to <media> [path|-] [-o path]
```

Shorthands (expand into the same router):

| Command | Expands to |
|---------|------------|
| `vellum report.md` | `--from file --to file` (PDF) |
| `vellum --to-clipboard report.md` | `--from file --to clipboard` |
| `echo '# Hi' \| vellum --to-clipboard -` | `--from content --to clipboard` |
| `vellum import doc.docx` | `--from file --to content` |
| `vellum import --from-clipboard` | `--from clipboard --to content` |

## CLI: view and install-viewer (macOS)

These are CLI-only (not MCP tools) — humans double-click Markdown; agents
already work with files and `convert`.

- `vellum view <file.md>` / `vellum --open <file.md>` — render to a
  cache location keyed by absolute path + mtime (never next to the
  source) and open the result. **HTML default** (browser, fast); pass
  `--pdf` for Preview-quality typography. Cache health: entries older
  than 7 days are dropped on each view; if the cache still exceeds
  50 MB, oldest entries are evicted until under the cap.
- `vellum install-viewer` — write `~/Applications/Vellum Viewer.app`
  (Cocoa document handler, compiled with clang at install time — shell
  scripts cannot receive Launch Services open-document Apple Events),
  register it with Launch Services, and set it as the default Markdown
  handler via `duti` (install with `brew install duti` if missing).
  Requires Xcode CLT. Re-run after `brew upgrade vellum`. Debug log:
  `~/Library/Logs/vellum-viewer.log`.
- `vellum uninstall-viewer` — remove the app bundle. Does not restore
  the previous default handler.

## Input rules

- File paths should be absolute when calling MCP. Relative paths are
  resolved against the server's working directory but this is fragile.
- Prefer `from.media=content` when the agent already has the text —
  **do not write a temporary `.md` file first**.
- Multiple files: `from.paths` with `to.media=file` (or `file_reference`),
  or the `files` sugar for Markdown→PDF.
- `to.media=file` without `to.path` defaults the output next to the
  source (`.pdf` from markdown, `.md` from rich-text).

## Style overrides

`convert` accepts an optional `style` object.
Each field is a CSS-valued string; empty fields fall through to the
user's config file (`~/.config/vellum/config.yaml` or
`$XDG_CONFIG_HOME/vellum/config.yaml`), which in turn falls through to
vellum's built-in defaults.

Fields:

| Field                   | Example       | Notes                                  |
|-------------------------|---------------|----------------------------------------|
| `font_size`             | `12px`        | Body font size                         |
| `line_height`           | `1.4`         | Body line height                       |
| `font_family`           | `Georgia, serif` | Body font family                    |
| `code_font_family`      | `Menlo, monospace` | Applied to `code` and `pre`       |
| `page_size`             | `Letter`      | `@page size`                           |
| `page_margin`           | `1.2cm`       | `@page margin`                         |
| `page_first_top_margin` | `2cm`         | `@page :first margin-top`              |
| `page_numbers`          | `true`        | Page number at bottom-centre (default off) |
| `running_head`          | `true`        | Current H1 text at top-centre (default off) |
| `bookmarks`             | `false`       | PDF outline from H1..H6 (default **on**); set false to suppress |
| `hyphenate`             | `true`        | Auto-hyphenate body text (default off; plug-and-play on WeasyPrint, needs dictionary on Prince) |
| `lang`                  | `en-GB`       | BCP-47 language tag; required for hyphenation |
| `pdfa`                  | `PDF/A-3b`    | Emit PDF/A-compliant archival PDF (default off) |

Example with per-call overrides:

```json
{
  "name": "convert",
  "arguments": {
    "files": [{"input": "/abs/path/to/report.md"}],
    "style": {"font_size": "12px", "page_margin": "1.2cm"}
  }
}
```

Reach for `style` overrides when a specific document needs a different
look than the user's default — e.g., a wide-table document that benefits
from a smaller font, or a print-targeted document on US Letter rather
than A4. For persistent preferences, edit the config file instead.

## Mermaid scale hint

Mermaid diagrams can overflow the PDF page when they are dense. To
scale a specific diagram, add a `vellum:scale` HTML comment immediately
before the fenced code block:

    <!-- vellum:scale 0.75 -->
    ```mermaid
    graph LR
      A --> B --> C
    ```

The scale is a CSS scale factor (1.0 = default). Values below 1.0
shrink the diagram; values above 1.0 enlarge it. Only use this when a
diagram does not fit; most diagrams render correctly at 1.0.

## Security notes

- vellum invokes three external binaries: the renderer (`weasyprint`
  by default, optionally `prince`), `node` (KaTeX math rendering), and
  `mmdc` (Mermaid diagrams). All rendering happens locally; no data is
  sent to external services.
- Neither renderer executes JavaScript from the input HTML during
  typesetting (Prince's JS engine is off by default; WeasyPrint has none).
- KaTeX runs in `throwOnError: false` mode, so malformed math
  expressions render as an error span rather than crashing the build.
- Mermaid and math rendering are performed server-side before Prince
  sees the document, so the final HTML contains only static SVG/HTML.

## Error handling

If a conversion fails, the error message includes the underlying
tool's stderr output (from prince, node, or mmdc). Report it verbatim
to the user — the stderr text is the most useful diagnostic.

Common failure modes:

- A required dependency is missing. vellum checks `prince`, `node`,
  and `mmdc` on PATH at startup and lists any missing tools with
  install instructions.
- The `katex` node package is not installed globally. Fix with
  `npm install -g katex`.
- A Mermaid diagram has invalid syntax. The `mmdc` error text is
  included in the returned error.
- Prince cannot fit content onto a page. This usually means an oversized
  image, table, or Mermaid diagram — try the `vellum:scale` hint.
