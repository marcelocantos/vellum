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
- The Markdown extension syntax vellum recognises (math, Mermaid, `vellum:scale`, `vellum:toc`)
- The set of recognised environment variables
- The runtime dependency expectations (tool names, minimum versions)

The pre-1.0 period exists to get these right. Until 1.0, any of these may
change between minor releases — though in practice we aim to minimise churn.

## Interaction surface catalogue

Snapshot as of **v0.20.0** (`const version` in `cmd/vellum`). Annotations:
**stable** (unlikely to change), **needs review** (functional but may be
refined), **fluid** (actively evolving).

### Go package API

Package paths are under `github.com/marcelocantos/vellum/…`.

**`convert`** — the Markdown → PDF pipeline.

- `func Convert(ctx context.Context, inputPath, outputPath string, opts *Options) error` — **stable** (viewer PDF path still calls this; CLI/MCP PDF go through `Run`)
- `func RenderFile(ctx context.Context, inputPath string, opts *Options) (html string, soft []string, err error)` — **needs review** (second return is non-fatal Mermaid diagnostics; not `*SoftError`)
- `func Render(ctx context.Context, src []byte, opts *Options) (html string, soft []string, err error)` — **needs review** (same)
- `type SoftError struct { Messages []string }` — **needs review** (added in v0.9.0; `Convert` / `Run` wrap `soft` into this after still producing output; CLI non-zero exit / MCP `errors`)
- `type Options struct { CSS string; HeadExtra string; Style *Style; Backend string; MermaidFormat string }` — **needs review** (`MermaidFormat` is `MermaidSVG` default or `MermaidPNG`; PDF sinks force PNG)
- `const MermaidSVG, MermaidPNG` — **needs review**
- `type Style struct { ... }` — **needs review** (14-field customisation surface; `toc` added in 🎯T38; field set likely to grow before 1.0)
- `type Backend interface` — **needs review** (added in v0.4.0; the surface is small but extension shape may evolve)
- `func ResolveBackend(name string) (Backend, error)` — **needs review** (same)
- `const BackendWeasyPrint, BackendPrince, DefaultBackend` — **needs review** (default name may change pre-1.0)
- `type Dep struct { Name, Purpose, Install string }` — **stable**
- `func RequiredDeps(backendName string) []Dep` — **stable** (signature changed in v0.4.0 to take backend name)
- `func CheckDeps(backendName string) error` — **stable** (signature changed in v0.4.0 to take backend name; MCP startup and viewer PDF call it; CLI `convert` / `Run` do not)

**`convert` (router)** — media-orthogonal `Run` (🎯T14).

- `func Run(ctx context.Context, req *Request) (*Result, error)` — **needs review**
- `type Media`, `Endpoint`, `Request`, `Result`, `FilePair` — **needs review**
- `Result.MediaDir`, `Result.Assets` — **needs review** (extracted import media; also on MCP `ConvertOutput`)
- `const MediaFile, MediaContent, MediaClipboard, MediaFileReference` — **needs review**
- `const FormatMarkdown, FormatHTML, FormatRTF, FormatPDF, FormatRich` — **needs review** (`FormatRTF` is inferred from `.rtf`; file sinks currently refuse it — 🎯T25)

**`mcp`** — the MCP server (streamable HTTP preferred; stdio fallback).

- `func Serve(ctx context.Context, version string) error` — **stable** (stdio fallback)
- `func HTTPHandler(version string, baseStyle *convert.Style, baseBackend string) http.Handler` — **needs review** (streamable HTTP; mounted at `/mcp` on the brew-service daemon, 🎯T31)
- **Single tool** `convert` with `from`/`to` media endpoints (+ optional `files` sugar) — **needs review** (replaced the four-tool surface: old `convert_to_clipboard`, `convert_from_clipboard`, `import` removed)
- `type Endpoint`, `FilePair`, `ConvertInput`, `ConvertOutput` — **needs review**

**`clipboard`** — system-clipboard read/write (added in v0.2.0; reads added in v0.5.0; file refs in 🎯T14). macOS `Write` tries AppKit `NSAttributedString` first, then falls back to pandoc HTML→RTF (🎯T23).

- `type Payload struct { HTML string }` — **needs review** (single-field today; RTF + plain text are derived; may grow explicit fields)
- `type Route string` — **needs review** (`RouteAppKit` preferred; `RoutePandoc` fallback drops CSS, keeps structure)
- `const RouteAppKit, RoutePandoc` — **needs review**
- `type WriteReport struct { Route Route; Fallback string }` — **needs review** (`Fallback` is empty on the AppKit path; a non-empty value is a degraded paste that `convert.Run` surfaces as a soft error)
- `func Write(p Payload) (WriteReport, error)` — **needs review** (macOS implementation only; non-macOS returns `ErrUnsupported`)
- `func ReadRTF() ([]byte, error)` — **needs review** (added in v0.5.0; macOS only)
- `func ReadHTML() ([]byte, error)` — **needs review** (same)
- `func ReadRichText() (data []byte, format string, err error)` — **needs review** (RTF preferred, HTML fallback; format is "rtf"/"html"/"")
- `func ReadPDF() ([]byte, error)` — **needs review** (public.pdf / com.adobe.pdf; PowerPoint slide copies)
- `func ReadImportable() (data []byte, format string, err error)` — **needs review** (RTF, then HTML, then PDF)
- `type FileRefPayload struct { Paths []string }` — **needs review** (Finder-style file references)
- `func WriteFileRefs(p FileRefPayload) error` — **needs review** (macOS; pasteboard-owned NSFilenamesPboardType so data survives process exit)
- `func ReadFileRefs() ([]string, error)` — **needs review**
- `const FormatRTF, FormatHTML, FormatPDF` — **needs review**
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

**`importer`** — rich-text → Markdown via pandoc; PDF via Poppler (added in v0.5.0).

- `func ImportFile(ctx context.Context, inputPath string, opts *Options) (Result, error)` — **needs review**
- `func ImportBytes(ctx context.Context, data []byte, opts *Options) (Result, error)` — **needs review** (format required on bytes)
- `func ImportFileMarkdown(ctx context.Context, inputPath, format string) (string, error)` — **needs review** (legacy Markdown-only wrapper)
- `func ImportBytesMarkdown(ctx context.Context, data []byte, format string) (string, error)` — **needs review** (same)
- `type Options struct { Format string; MediaDir string }` — **needs review**
- `type Result struct { Markdown string; MediaDir string; Assets []string }` — **needs review**
- `func CheckDep() error` — **needs review** (lazy pandoc dependency check)
- `var PandocDep struct { Name, Purpose, Install string }` — **needs review**

**`adf`** — Markdown → Atlassian Document Format (library only; not a `convert.Run` sink or MCP `to.format`). Isolated goldmark+GFM parser; no network, no mmdc (🎯T26).

- `func Convert(markdown string, args *ConvertArgs) (*Document, error)` — **needs review**
- `func ConvertJSON(markdown string, args *ConvertArgs) ([]byte, error)` — **needs review**
- `type Document`, `Node`, `Mark`, `ConvertArgs` — **needs review**
- `type ImagePolicy` / `const ImagePolicyExternal, ImagePolicyLink, ImagePolicyFail` — **needs review** (default external; no silent relative→link)
- Math residue: `$…$` / `$$…$$` rewritten to `latex` codeBlock (declared in `adf/doc.go`). Footnotes and definition lists are not specially mapped.

**`internal/pandoc`** — HTML → RTF/plain export helper. Not a public import path. Import (rich text → Markdown) stays in `importer/`. Used by the clipboard pandoc fallback.

- `const Binary = "pandoc"` — **needs review**
- `func Available() error` — **needs review**
- `func HTMLToRTF(html, resourcePath string) ([]byte, error)` — **needs review** (`--standalone`; CSS is ignored)
- `func HTMLToPlain(html string) ([]byte, error)` — **needs review**

**`viewer`** — localhost view server (HTML) + cached PDF open; macOS default Markdown handler (added in v0.6.0; view server in v0.14.0). Cache filename is `sha256(absPath)[:8]+ext` (not mtime). A `.stamp` sidecar records source mtime+size so HTML/PDF re-render in place when Markdown changes (🎯T28). HTML responses inject view chrome at serve time (TOC, figure lightbox, PDF/clipboard/Finder actions under `/_vellum/`); the convert-cache HTML file stays chrome-free (🎯T32). An open view tab watches the source over `/_vellum/watch` (WebSocket, 5-minute heartbeat) and reloads on stamp change (🎯T43). GFM task-list checkboxes can toggle the source via `POST /_vellum/task-toggle` after consent (🎯T34).

- `func View(ctx context.Context, inputPath string, opts *ViewOptions) (string, error)` — **needs review** (HTML opens `http://127.0.0.1:18742/…` view-server URL; PDF still opens a cache file)
- `type ViewOptions struct { Format Format; Style *convert.Style; Backend string; Open func(string) error; CacheDir string; MaxBytes int64; MaxAge time.Duration; Now func() time.Time; ViewBaseURL string; SkipEnsureServer bool }` — **needs review**
- `type Server struct { Addr string; CacheDir string; Style *convert.Style; Backend string; … }` — **needs review** (added in v0.14.0)
- `func (*Server) ListenAndServe(ctx context.Context) error` — **needs review** (loopback-only bind)
- `func (*Server) Handler() http.Handler` — **needs review** (Markdown GET injects chrome; `/_vellum/pdf` GET, `/_vellum/clipboard` POST, `/_vellum/reveal` POST, `/_vellum/watch` GET/WebSocket, `/_vellum/task-toggle` POST)
- `func ViewURL(origin, absPath string) string` — **needs review**
- `func EnsureViewServer(origin string) error` — **needs review**
- `func ProbeViewServer(origin string) error` — **needs review**
- `const DefaultViewAddr, HealthPath` — **needs review** (`127.0.0.1:18742`, `/healthz`)
- `const FormatHTML, FormatPDF` — **needs review**
- `const CacheMaxBytes, CacheMaxAge` — **needs review** (50 MiB / 7 days defaults)
- `func InstallViewer(opts *InstallOptions) (appPath string, err error)` — **needs review** (macOS only; v0.7.0 compiles a Cocoa document-handler binary at install time — shell-script CFBundleExecutable cannot receive Launch Services open-document Apple Events)
- `func UninstallViewer(opts *InstallOptions) error` — **needs review** (macOS only)
- `const BundleID, AppName` — **needs review**

### CLI surface

Binary: `vellum`.

**Usage**

    vellum [options] <input.md...>
    vellum convert --from <media> --to <media> [path|-]
    vellum --mcp

**Flags** (all accepted with single- or double-dash form)

| Flag             | Purpose                                             | Stability |
|------------------|-----------------------------------------------------|-----------|
| `--help`         | Print usage to stdout, exit 0                       | stable    |
| `--help-agent`   | Print usage + embedded agent guide, exit 0          | stable    |
| `--version`      | Print version string to stdout, exit 0              | stable    |
| `--mcp`          | Run as stdio MCP server (fallback; HTTP on the brew-service daemon is preferred) | stable    |
| `--to-clipboard` | Sugar: file or stdin (`-`) → clipboard rich (macOS) | needs review |
| `--open`         | Open via localhost view server (alias for `view`). Added in v0.6.0; server URL behaviour in v0.14.0. | needs review |
| `--pdf`          | With `--open`/`view`: high-fidelity PDF instead of HTML. Added in v0.6.0. | needs review |
| `-o <path>`      | Output path (single-input only)                     | stable    |
| `--output <path>`| Same as `-o`                                        | stable    |
| `-o=<path>`      | Same as `-o` (inline form)                          | stable    |
| `--output=<path>`| Same as `-o` (inline form)                          | stable    |
| `--backend <name>` | Renderer: `weasyprint` (default) or `prince` (added in v0.4.0) | needs review |
| `--backend=<name>` | Same as `--backend <name>` (inline form)          | needs review |

**Subcommands**

| Subcommand        | Purpose                                              | Stability    |
|-------------------|------------------------------------------------------|--------------|
| `vellum convert --from/--to` | Media-orthogonal conversion (file, content, clipboard, file_reference). Added in v0.8.0. | needs review |
| `vellum import <file>` | Sugar: rich-text → Markdown (via `convert`). Added in v0.5.0. | needs review |
| `vellum import --from-clipboard` | Sugar: clipboard rich → Markdown (macOS). Added in v0.5.0. | needs review |
| `vellum import … -o <path>` | Write the Markdown to a file instead of stdout. | needs review |
| `vellum import … --from <fmt>` | Override pandoc format autodetection. | needs review |
| `vellum view <file>` | Open via localhost view server (HTML → `http://127.0.0.1:18742/…`) or PDF cache file. Added in v0.6.0; server behaviour in v0.14.0. | needs review |
| `vellum serve-view` | Run the localhost daemon: Markdown view + streamable HTTP MCP at `/mcp` (loopback only). Added in v0.14.0; MCP mount in 🎯T31. | needs review |
| `vellum install-viewer` | Install Vellum Viewer.app as default .md handler (macOS). Added in v0.6.0. | needs review |
| `vellum uninstall-viewer` | Remove Vellum Viewer.app. Added in v0.6.0. | needs review |

**Positional arguments**

- One or more input `.md` files (bare form sugar for file→PDF). **stable**.

**Output contract**

- **stdout** (CLI mode, file sinks): one line per written path. Content sinks print the text body. **needs review** (content path added with v0.8.0).
- **stderr** (CLI mode): error messages prefixed `Error: `; status lines for clipboard sinks. Non-zero exit on failure. **needs review**.
- **stdout** (MCP stdio mode): JSON-RPC 2.0 messages only, as required by the MCP
  stdio transport. **stable**.
- **HTTP** (MCP streamable HTTP at `/mcp` on `serve-view`): JSON-RPC 2.0 over
  the MCP streamable HTTP transport. **needs review** (🎯T31).
- **stderr** (MCP mode): reserved for diagnostics. Currently unused beyond
  errors surfaced by the SDK itself. **stable**.
- **Exit codes**: `0` on success, `1` on any error. **stable**.

### MCP server surface

- Transport: stdio (`mcp.StdioTransport`). **stable**.
- Server info: `{ name: "vellum", version: <build version> }`. **stable**.
- Protocol version: whatever the embedded `github.com/modelcontextprotocol/go-sdk` version negotiates. **needs review** (SDK is pre-1.x on its own track; bumping it may shift the minimum protocol version).

**Tools**: single `convert` (media-orthogonal `from`/`to`; 🎯T14).
Removed: `convert_to_clipboard`, `convert_from_clipboard`, `import`.

**Tool: `convert`**

- **Description**: media-orthogonal convert across file, content,
  clipboard, and file_reference; formats inferred; content/clipboard
  PDF sinks refused. Optional `files` sugar for Markdown→PDF batch.
  Optional `style` / `backend`. **needs review**
- **Input schema**: **needs review**

  ```json
  {
    "from": { "media": "file|content|clipboard|file_reference", "path?": "…", "paths?": [], "content?": "…", "format?": "…" },
    "to":   { "media": "file|content|clipboard|file_reference", "path?": "…", "format?": "…" },
    "files": [{ "input": "…", "output?": "…" }],
    "style?": {},
    "backend?": "weasyprint|prince"
  }
  ```

- **Structured output**: **needs review**

  ```json
  {
    "from_media": "…",
    "to_media": "…",
    "from_format": "…",
    "to_format": "…",
    "paths": ["…"],
    "content": "…",
    "errors": ["…"],
    "media_dir": "…",
    "assets": ["…"]
  }
  ```

  `media_dir` / `assets` are set on rich-text and PDF import. **needs review**

- **Text content**: Markdown/HTML body when `to.media=content`; otherwise a short summary. **needs review**.
- **`isError`**: set to `true` when the conversion fails (or when a batch has no successful paths and only errors). **needs review**.
- **Platform**: `clipboard` and `file_reference` media require macOS; other platforms return unsupported.

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
- **Mermaid** — ```` ```mermaid ```` fenced blocks via `mmdc`. HTML/view embed **SVG** (vector). PDF keeps **PNG** at 2× scale because Mermaid SVG `foreignObject` labels do not paint in Prince. **stable** (dual path, 🎯T17).
- **`<!-- vellum:scale N -->`** hint — applies a CSS `max-width: N * 100%` to the *next* Mermaid block. `N > 0`. **stable** (the comment syntax is stable; future hints may be added under the same `vellum:` prefix).
- **`<!-- vellum:toc -->`** hint — replaced with a static Contents list (nested heading links, depth h1–h3). Same effect as `style.toc: true` except placement follows the hint. **needs review** (🎯T38).

### Environment variables

- `VELLUM_VIEW_ADDR=<host:port>` — loopback listen address for `vellum serve-view`
  (default `127.0.0.1:18742`). Also the MCP HTTP origin (`/mcp` on the same
  listener). **needs review** (added in v0.14.0; MCP mount 🎯T31).
- `VELLUM_DEBUG_HTML=<path>` — if set, vellum writes the assembled HTML to
  this path before invoking the PDF backend (WeasyPrint default, or Prince).
  Intended for development only. Does not document the KaTeX CSS CDN link
  injected into that HTML. **needs review** (may be renamed with a
  `VELLUM_DEBUG_*` namespace if more debug hooks are added).

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
- `pandoc` — Pandoc 3.x. **needs review** (rich-text import: RTF/DOCX/HTML/… with `--extract-media`; lazily checked).
- `pdftoppm` / `pdftotext` — Poppler. **needs review** (PDF import: page images + text; lazily checked).

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
  Node/Python wrapper that speaks streamable HTTP at `/mcp` or spawns
  `vellum --mcp`), their public surface must be catalogued here too before 1.0.
- **Concurrent convert.** `convert.Convert` is not explicitly documented
  as safe for concurrent use. In practice each call writes to a distinct
  temp file and a caller-chosen output path, so parallel calls should
  work, but the contract needs to be nailed down.

## Out of scope for 1.0

Features and changes explicitly deferred past 1.0.

- **Bundled Chromium.** Distributing a Chromium binary alongside
  vellum would solve the `mmdc` setup pain but adds ~200 MB to the
  release and introduces a security-update treadmill. Stays external.
- **Further writers.** Markdown, HTML, PDF, and clipboard rich already
  ship. File RTF (🎯T25), EPUB, and ADF-as-a-`Run` sink (🎯T27) are not
  1.0 commitments.
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
  3-month minimum settling period (historical note; 1.0 shakeout for
  this project is the 1-month rule in the release skill). Clock last
  reset on 2026-07-31 by v0.8.0 (media-orthogonal `convert` MCP/CLI
  surface; removed three MCP tools).
