# vellum agent guide

vellum is a Markdown-to-PDF tool that renders GitHub-flavoured Markdown
(with KaTeX math and Mermaid diagrams) to typeset PDFs via WeasyPrint
(default, BSD-3) or Prince (opt-in, proprietary). It runs either as a
command-line tool or as an MCP (Model Context Protocol) server over stdio.

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
   - Call a tool: convert a trivial one-line Markdown file through the
     `convert` tool and confirm the returned `converted` list contains
     the output path. If the call returns an error in `errors` instead,
     report the error text verbatim to the user — it almost always
     names the missing dependency or the bad input.

If any of these steps fails, installation is not complete. Report the
failing step and its error to the user — do not proceed as if vellum
is ready.

## Preferred invocation

When running inside an AI agent, prefer the MCP interface over the CLI.
Start vellum with `vellum --mcp` and call the `convert` tool. The CLI
(`vellum <input.md>`) is fine for interactive use but the MCP tool
returns structured results (converted paths, per-file errors) that are
easier to consume programmatically.

## Tool schema

The MCP server exposes two tools:

### `convert` — Markdown → PDF (batch)

- `convert({ files: [{ input, output? }], style?, backend? })`
  - `input` — absolute path to a `.md` file (required)
  - `output` — absolute path for the output PDF (optional; defaults to
    the input path with its extension replaced by `.pdf`)
  - `style` — optional style overrides; see [Style overrides](#style-overrides)
  - `backend` — optional renderer override: `"weasyprint"` (default) or
    `"prince"`. Empty falls through to the user's config file

Example call:

```json
{
  "name": "convert",
  "arguments": {
    "files": [
      {"input": "/abs/path/to/report.md"},
      {"input": "/abs/path/to/spec.md", "output": "/abs/path/to/out/spec.pdf"}
    ]
  }
}
```

Response shape:

```json
{
  "converted": ["/abs/path/to/report.pdf", "/abs/path/to/out/spec.pdf"],
  "errors": []
}
```

### `convert_to_clipboard` — Markdown → system clipboard

Renders a single Markdown file and places **RTF + HTML + plain text**
on the system clipboard in a single atomic transaction. Designed for
handing formatted content to rich-text composers (Slack, Mail, …)
without the brittle `textutil` + `osascript` pipeline.

- `convert_to_clipboard({ input, style?, backend? })`
  - `input` — absolute path to a `.md` file (required)
  - `style` — optional style overrides; see [Style overrides](#style-overrides)
  - `backend` — optional renderer override (`"weasyprint"` or `"prince"`)

Example call:

```json
{
  "name": "convert_to_clipboard",
  "arguments": {"input": "/abs/path/to/slack-message.md"}
}
```

The tool returns once the underlying NSPasteboard has confirmed the
write (`changeCount` advanced) — there is no race window where the
agent should `sleep` before paste.

**Why this exists.** Naive shells of `textutil` + `osascript` to put
formatted content on the macOS clipboard have three well-known
failure modes:

1. **Single representation only.** `osascript` sets `«class RTF »`
   and nothing else. Apps that prefer plain text fall back to the
   raw RTF source (`{\rtf1\ansi…`) and paste gibberish.
2. **No commit confirmation.** AppleScript returns before the
   pasteboard daemon has committed the data; combined with Universal
   Clipboard sync, this leaves a 30–60 s window where `Cmd+V`
   silently does nothing or pastes the previous clipboard.
3. **Lossy round-trip.** `pbpaste | pbcopy` strips rich types
   because `pbpaste` defaults to plain text.

Reach for `convert_to_clipboard` when an agent needs to deliver
formatted content for a human to paste — not just to display it. The
tool replaces the four-line `textutil → osascript` recipe with one
call.

**Platform support.** macOS only. Linux is out of scope. Windows is
parked (see 🎯T7.1) — pick it up when there's concrete demand. On
non-macOS platforms the tool returns an `unsupported` error rather
than failing silently.

## Input rules

- Input paths must be absolute. Relative paths are resolved against the
  server's working directory but this is fragile — always pass absolute
  paths.
- Input files should have a `.md` extension.
- The caller decides the output path. If omitted, vellum writes
  `<input>.pdf` next to the input file.
- Multiple files can be converted in a single call; each is processed
  independently and errors are reported per-file.

## Style overrides

Both tools accept an optional `style` object. Each field is a CSS-valued
string; empty fields fall through to the user's config file
(`~/.config/vellum/config.yaml` or `$XDG_CONFIG_HOME/vellum/config.yaml`),
which in turn falls through to vellum's built-in defaults.

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
