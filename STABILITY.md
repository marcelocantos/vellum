# STABILITY

Vellum is pre-1.0. This document tracks the project's readiness for 1.0 and
catalogues the interaction surface that will become the backwards-compatibility
baseline at that point.

## Stability commitment

Vellum 1.0 will be a backwards-compatibility contract. After 1.0, breaking
changes to any of the following require a major version bump:

- The public Go API of packages that consumers import
- The CLI flags, subcommands, and stdout/stderr contract
- The MCP server info and the `convert` tool schema
- The Markdown extension syntax vellum recognises (math, Mermaid, `vellum:scale`)
- The set of recognised environment variables
- The runtime dependency expectations (tool names, minimum versions)

The pre-1.0 period exists to get these right. Until 1.0, any of these may
change between minor releases — though in practice we aim to minimise churn.

## Interaction surface catalogue

Snapshot as of **v0.5.0**. Annotations: **stable** (unlikely to change),
**needs review** (functional but may be refined), **fluid** (actively
evolving).

### Go package API

Package paths are under `github.com/marcelocantos/vellum/…`.

**`convert`** — the Markdown → PDF pipeline.

- `func Convert(ctx context.Context, inputPath, outputPath string, opts *Options) error` — **stable**
- `func RenderFile(ctx context.Context, inputPath string, opts *Options) (string, error)` — **needs review** (added in v0.2.0 for clipboard delivery; returns the post-pipeline HTML; consumers other than `convert_to_clipboard` may shake out a more ergonomic shape)
- `func Render(ctx context.Context, src []byte, opts *Options) (string, error)` — **needs review** (same)
- `type Options struct { CSS string; HeadExtra string; Style *Style; Backend string }` — **needs review** (Style + Backend added in v0.4.0; CSS + HeadExtra preserved as escape hatches)
- `type Style struct { ... }` — **needs review** (13-field customisation surface added in v0.4.0; field set likely to grow before 1.0)
- `type Backend interface` — **needs review** (added in v0.4.0; the surface is small but extension shape may evolve)
- `func ResolveBackend(name string) (Backend, error)` — **needs review** (same)
- `const BackendWeasyPrint, BackendPrince, DefaultBackend` — **needs review** (default name may change pre-1.0)
- `type Dep struct { Name, Purpose, Install string }` — **stable**
- `func RequiredDeps(backendName string) []Dep` — **stable** (signature changed in v0.4.0 to take backend name)
- `func CheckDeps(backendName string) error` — **stable** (signature changed in v0.4.0 to take backend name)

**`mcp`** — the stdio MCP server.

- `func Serve(ctx context.Context, version string) error` — **stable**
- `type ConvertFile struct { Input, Output string }` — **stable** (mirrors the JSON schema)
- `type ConvertInput struct { Files []ConvertFile; Style *convert.Style; Backend string }` — **needs review** (Style + Backend added in v0.4.0)
- `type ConvertOutput struct { Converted, Errors []string }` — **stable**
- `type ClipboardInput struct { Input string; Style *convert.Style; Backend string }` — **needs review** (Style + Backend added in v0.4.0; macOS-only tool, may grow fields once non-macOS support is decided)
- `type ClipboardOutput struct { Input string }` — **needs review** (currently echoes the input for confirmation)

**`clipboard`** — system-clipboard read/write (added in v0.2.0; reads added in v0.5.0).

- `type Payload struct { HTML string }` — **needs review** (single-field today; RTF + plain text are derived; may grow explicit fields)
- `func Write(p Payload) error` — **needs review** (macOS implementation only; non-macOS returns `ErrUnsupported`)
- `func ReadRTF() ([]byte, error)` — **needs review** (added in v0.5.0; macOS only)
- `func ReadHTML() ([]byte, error)` — **needs review** (same)
- `func ReadRichText() (data []byte, format string, err error)` — **needs review** (RTF preferred, HTML fallback; format is "rtf"/"html"/"")
- `const FormatRTF, FormatHTML` — **needs review**
- `var ErrUnsupported error` — **stable**

**`embed`** — compile-time assets.

- `var GitHubCSS string` — **needs review** (exposed for the CLI binary's own use; may become unexported if no external consumer appears)
- `var HTMLTemplate string` — **needs review** (same)

**`docs`** — embedded documentation.

- `var AgentGuide string` — **stable** (embedded text of `docs/agents-guide.md`)

**`config`** — on-disk configuration (added in v0.4.0).

- `type Config struct { Backend string; Style *convert.Style }` — **needs review**
- `func Path() (string, error)` — **needs review** (XDG-aware path resolution)
- `func Load() (*Config, error)` — **needs review** (missing file returns empty Config, not error)

**`importer`** — rich-text → Markdown via pandoc (added in v0.5.0).

- `func ImportFile(ctx context.Context, inputPath, format string) (string, error)` — **needs review**
- `func ImportBytes(ctx context.Context, data []byte, format string) (string, error)` — **needs review**
- `func CheckDep() error` — **needs review** (lazy pandoc dependency check)
- `var PandocDep struct { Name, Purpose, Install string }` — **needs review**

### CLI surface

Binary: `vellum`.

**Usage**

    vellum [options] <input.md...>
    vellum --mcp

**Flags** (all accepted with single- or double-dash form)

| Flag             | Purpose                                             | Stability |
|------------------|-----------------------------------------------------|-----------|
| `--help`         | Print usage to stdout, exit 0                       | stable    |
| `--help-agent`   | Print usage + embedded agent guide, exit 0          | stable    |
| `--version`      | Print version string to stdout, exit 0              | stable    |
| `--mcp`          | Run as stdio MCP server                             | stable    |
| `--to-clipboard` | Render single input and place RTF + HTML + plain text on clipboard (macOS only) | needs review |
| `-o <path>`      | Output PDF path (single-input only)                 | stable    |
| `--output <path>`| Same as `-o`                                        | stable    |
| `-o=<path>`      | Same as `-o` (inline form)                          | stable    |
| `--output=<path>`| Same as `-o` (inline form)                          | stable    |
| `--backend <name>` | Renderer: `weasyprint` (default) or `prince` (added in v0.4.0) | needs review |
| `--backend=<name>` | Same as `--backend <name>` (inline form)          | needs review |

**Subcommands**

| Subcommand        | Purpose                                              | Stability    |
|-------------------|------------------------------------------------------|--------------|
| `vellum import <file>` | Read rich-text → Markdown (RTF, DOCX, HTML, ODT, EPUB, LaTeX, …, via pandoc). Added in v0.5.0. | needs review |
| `vellum import --from-clipboard` | Read rich-text from system clipboard → Markdown (macOS only). Added in v0.5.0. | needs review |
| `vellum import … -o <path>` | Write the Markdown to a file instead of stdout. Added in v0.5.0. | needs review |
| `vellum import … --from <fmt>` | Override pandoc format autodetection. Added in v0.5.0. | needs review |

**Positional arguments**

- One or more input `.md` files. **stable**.

**Output contract**

- **stdout** (CLI mode): one line per converted file containing the absolute
  output PDF path. No other output. **stable**.
- **stderr** (CLI mode): error messages prefixed `Error: `. Non-zero exit on
  failure. **stable**.
- **stdout** (MCP mode): JSON-RPC 2.0 messages only, as required by the MCP
  stdio transport. **stable**.
- **stderr** (MCP mode): reserved for diagnostics. Currently unused beyond
  errors surfaced by the SDK itself. **stable**.
- **Exit codes**: `0` on success, `1` on any error. **stable**.

### MCP server surface

- Transport: stdio (`mcp.StdioTransport`). **stable**.
- Server info: `{ name: "vellum", version: <build version> }`. **stable**.
- Protocol version: whatever the embedded `github.com/modelcontextprotocol/go-sdk` version negotiates. **needs review** (SDK is pre-1.x on its own track; bumping it may shift the minimum protocol version).

**Tools**: `convert` (Markdown → PDF batch), `convert_to_clipboard`
(single Markdown → system clipboard, macOS only, added in v0.2.0),
`convert_from_clipboard` (clipboard rich text → Markdown, macOS only,
added in v0.5.0), and `import` (rich-text file → Markdown, added in
v0.5.0).

**Tool: `convert`**

- **Description**: "Convert one or more Markdown files to PDF. Input paths must be absolute. Each file is rendered via goldmark (GFM + extensions), with server-side KaTeX math and Mermaid diagrams, then typeset by the selected backend (WeasyPrint by default, Prince opt-in). Returns the list of written PDFs and any errors. Optional 'style' and 'backend' fields override the user's config file for this call only."
- **Input schema**: **stable**

  ```json
  {
    "files": [
      { "input": "<absolute path>", "output": "<absolute path, optional>" }
    ]
  }
  ```

- **Structured output**: **stable**

  ```json
  {
    "converted": ["<absolute path>", …],
    "errors":    ["<absolute path>: <message>", …]
  }
  ```

- **Text content**: human-readable summary ("Converted N file(s): …" / "Errors: …"). **needs review** (format may be tweaked for readability; structured output is the load-bearing part).
- **`isError`**: set to `true` only when every file failed. **stable**.

**Tool: `convert_to_clipboard`** (added in v0.2.0)

- **Description**: "Render a Markdown file and place RTF + HTML + plain text on the system clipboard (macOS only). Returns once the underlying NSPasteboard has confirmed the write."
- **Input schema**: **needs review**

  ```json
  { "input": "<absolute path to .md file>" }
  ```

- **Structured output**: **needs review**

  ```json
  { "input": "<echoed input path>" }
  ```

- **Platform**: macOS only. Non-macOS returns an `unsupported` error.
- **`isError`**: set to `true` on any failure (unsupported platform, missing file, render error, pasteboard write failure). **needs review** (semantics may shift as multi-platform support is decided).

### Markdown syntax extensions

These are the extensions beyond CommonMark that vellum recognises. Anything
not listed here is either GFM (via goldmark's GFM extension) or not supported.

- **GFM** — tables, task lists, strikethrough, autolinks. **stable**.
- **Footnotes** — PHP Markdown Extra style. **stable**.
- **Definition lists** — PHP Markdown Extra style. **stable**.
- **Typographer** — goldmark's smart-punctuation transform. **stable**.
- **YAML front-matter** — `title` field extracted for `<title>`. **stable**.
- **Inline math** — `$...$` rendered via server-side KaTeX. **stable**.
- **Block math** — `$$...$$` rendered via server-side KaTeX. **stable**.
- **Code-block protection** — `$…$` inside fenced code blocks and inline code is not extracted as math. **stable**.
- **Mermaid** — ```` ```mermaid ```` fenced blocks rendered to PNG via `mmdc` at 2× scale. **stable**.
- **`<!-- vellum:scale N -->`** hint — applies a CSS `max-width: N * 100%` to the *next* Mermaid block. `N > 0`. **stable** (the comment syntax is stable; future hints may be added under the same `vellum:` prefix).

### Environment variables

- `VELLUM_DEBUG_HTML=<path>` — if set, vellum writes the post-preprocessing
  HTML to this path before invoking Prince. Intended for development only.
  **needs review** (may be renamed with a `VELLUM_DEBUG_*` namespace if more
  debug hooks are added).

### Embedded assets

- GitHub-style Markdown CSS (chroma-compatible syntax highlighting classes).
  **needs review** (users may reasonably want to override this; the `Options.CSS`
  hook already allows it, but the embedded default may shift).
- HTML wrapper template. **stable** (layout choices are minimal; anything
  surfaced via CSS overrides rather than template edits).

### Runtime dependencies

Required external binaries on `PATH`:

- **One renderer backend** (selectable via config or per-call):
  - `weasyprint` — WeasyPrint 60 or later. **stable** (default).
  - `prince` — Prince 16.2 or later. **stable** (opt-in via `backend: prince`).
- `node` — any recent Node.js. **stable**.
- `mmdc` — mermaid-cli. **stable**.
- `pandoc` — Pandoc 3.x. **needs review** (required for `vellum import` and the `convert_from_clipboard` / `import` MCP tools; lazily checked).

Required Node package (installed globally):

- `katex` — `npm install -g katex`. **stable**.

Bundling strategy may change (see *Out of scope for 1.0*), but the
dependency set itself is considered stable.

## Gaps and prerequisites

Items that must land before 1.0 can be cut.

- **Cross-platform coverage.** The release matrix currently targets
  macOS arm64 and Linux x86_64/arm64. Windows is not tested. Decide
  whether Windows is in-scope for 1.0 or deferred.
- **Runtime dependency installer.** First-time setup of `mmdc` + its
  pinned Chromium is a consistent pain point. A `vellum doctor` or
  equivalent one-shot setup command would move this out of the 1.0
  gap list.
- **Custom CSS API smoke-test.** `Options.CSS` is exposed but not
  exercised by the CLI. Either wire a `--css` flag or remove the
  field; shipping an untested extension point into 1.0 is a trap.
- **`HeadExtra`.** Currently a bare string append. Either keep (and
  document) or replace with a typed options struct. Shipping it as-is
  locks a fragile shape.
- **Error-shape consistency.** Per-file errors in `ConvertOutput.Errors`
  are currently strings ("path: message"). A structured `{path, message}`
  object would be more robust for programmatic consumers.
- **Audit logging.** `docs/audit-log.md` is new; the release skill
  appends to it. Ensure the convention sticks across subsequent releases.
- **Binding / wrapper libraries.** None exist. If any land (e.g., a
  Node/Python wrapper that spawns `vellum --mcp`), their public surface
  must be catalogued here too before 1.0.
- **Concurrent convert.** `convert.Convert` is not explicitly documented
  as safe for concurrent use. In practice each call writes to a distinct
  temp file and a caller-chosen output path, so parallel calls should
  work, but the contract needs to be nailed down.

## Out of scope for 1.0

Features and changes explicitly deferred past 1.0.

- **HTTP MCP transport.** vellum is stdio-only by design. HTTP adds
  port management and daemon lifecycle and brings no new capability;
  punted.
- **Bundled Chromium.** Distributing a Chromium binary alongside
  vellum would solve the `mmdc` setup pain but adds ~200 MB to the
  release and introduces a security-update treadmill. Stays external.
- **Alternative output formats.** Markdown → HTML and Markdown → EPUB
  are plausible but not in scope. 1.0 is Markdown → PDF.
- **Additional renderers.** vellum now supports WeasyPrint (default) and
  Prince (opt-in). No plans to add wkhtmltopdf (deprecated/abandoned) or
  headless Chrome (Chromium footprint is prohibitive for a CLI tool).
- **Plugin architecture.** No goldmark extension plugin API, no custom
  preprocessor registration. Users who need this can fork.
- **Non-squash merges.** Release history is linear by policy. Not a
  stability concern but stated for the avoidance of doubt.

## 1.0 readiness check

Not eligible.

- **Checklist**: not clear (see *Gaps and prerequisites*).
- **Settling threshold**: counting surface items (Go API + CLI flags +
  MCP tool schema + Markdown extensions + env vars ≈ 50 items) →
  3-month minimum settling period. Clock last reset on 2026-04-27 by
  the v0.2.0 surface additions (`convert.Render`/`RenderFile`,
  `clipboard` package, `--to-clipboard`, `convert_to_clipboard` MCP
  tool). v0.3.0 is install-mechanism only (Homebrew formula wrapper)
  — no public-surface change; clock unchanged.
