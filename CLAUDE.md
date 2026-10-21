# vellum

Document preparation MCP server — media-orthogonal conversion (file,
content, clipboard, file_reference) via goldmark + WeasyPrint (Prince
opt-in), pandoc for rich-text import and PowerPoint (pptx) output.

## Architecture

Go binary with two modes:
- **MCP server** (HTTP on the brew-service daemon at `/mcp`; `vellum --mcp` stdio fallback): single `convert` tool with `from`/`to` media
- **CLI** (`vellum convert --from … --to …`; sugars for bare `.md`, `--to-clipboard`, `import`)

### Pipeline

```
from media → normalise (pandoc import and/or math/mermaid preprocessors + goldmark HTML)
  → to media (file / content / clipboard rich / file_reference)
```

Markdown → PDF path still: source preprocessors → goldmark → HTML template → WeasyPrint/Prince.

Markdown → pptx path: Mermaid preprocessor (PNG, inlined as images) → pandoc gfm reader → pptx writer with `--reference-doc` = the selected template (`convert/pptx.go`). Built-in template is `embed/reference.pptx`, a pinned copy of pandoc's default.

### Key packages

| Package | Role |
|---------|------|
| `cmd/vellum/` | CLI entry point |
| `convert/` | Markdown → HTML/PDF pipeline; unified `Run` router; Backend interface. Math and Mermaid are source preprocessors (`convert/katex.go`, `convert/mermaid.go`), not goldmark extensions. `convert/pptx.go` is the pandoc-backed PowerPoint sink and template resolution. |
| `clipboard/` | Rich clipboard + file references. macOS: NSPasteboard via cgo, `Write` tries AppKit then pandoc HTML→RTF. Windows: pure-Go user32 (`clipboard_windows.go`): CF_HTML + RTF (pandoc) + Unicode text, PDF read, CF_HDROP file lists. |
| `importer/` | Rich-text → Markdown via pandoc; PDF via Poppler |
| `adf/` | Markdown → Confluence ADF (library only; not a `convert.Run` sink) |
| `internal/pandoc/` | pandoc export helpers: HTML → RTF/plain (clipboard fallback) and Markdown → file (pptx); not a public API |
| `internal/xdg/` | Per-user config directory (`$XDG_CONFIG_HOME/vellum`, else `~/.config/vellum`; `%APPDATA%\vellum` on Windows) shared by `config/` and template lookup |
| `internal/fileurl/`, `internal/shim/`, `internal/hint/` | Portability helpers: drive-letter-safe `file://` URLs, running npm `.cmd` shims (mmdc) as `node <script>` on Windows, per-OS install/service hints |
| `config/` | User configuration loaded from `~/.config/vellum/config.yaml` |
| `mcp/` | MCP server (single `convert` tool; streamable HTTP + stdio) |
| `embed/` | Embedded assets (CSS, HTML template, built-in pptx reference deck, KaTeX stylesheet + fonts under `katex/`, refreshed by `scripts/vendor-katex.sh`) |
| `internal/testdeps/` | Test gate for external converters (`VELLUM_REQUIRE_DEPS`) |
| `viewer/` | Localhost daemon (HTML view + chrome + `/mcp`); cached PDF open; macOS default .md handler (`install-viewer`); Windows logon Scheduled Task (`install-service`) |

### External dependencies

- **WeasyPrint** (default) — HTML → PDF, BSD-3 (must be on PATH; `brew install weasyprint`)
- **Prince** (opt-in via `backend: prince`) — HTML → PDF, proprietary (must be on PATH)
- **node + katex** — KaTeX math HTML (`npm install -g katex`); the stylesheet and fonts are embedded, never fetched, and `TestKaTeXVersionMatchesRenderer` pins the vendored copy to the installed package
- **mmdc** — Mermaid CLI for diagram rendering (optional, on PATH)
- **pandoc** — rich-text import, pptx output, and clipboard HTML→RTF fallback (lazy; only when needed)
- **poppler** — PDF import (`pdftoppm`, `pdftotext`; lazy)

## Gate

`cv gate` is the definition of green — gofmt, vet, the suite with
`VELLUM_REQUIRE_DEPS=1`, a skip census locked at 0 non-pasteboard
skips (pasteboard skips must name “pasteboard”), and the CLI
contract. The gate runs locally on Colossus (macOS); there is no
GitHub Actions. `cv gate` is the only oracle, so there is no remote
build to reproduce: a red gate is the failure, one command away.
`cv bullseye` adds a clean-tree check for convergence.

Platforms: macOS (darwin-arm64, the gate host) and Windows (arm64 and
amd64). Windows is proven on the Parallels "Windows 11" arm64 VM on
Colossus (`ssh hms-vm`): before a Windows-affecting change lands, run
`VELLUM_REQUIRE_DEPS=1 go test ./...` there with 0 skips. The Windows
toolchain is Go, pandoc and node (winget), WeasyPrint (pip) with Pango
and poppler from MSYS2 clangarm64 (`WEASYPRINT_DLL_DIRECTORIES`), and
`npm install -g katex@<embed version> @mermaid-js/mermaid-cli`. Linux is
unsupported (no clipboard, no viewer integration); don't add a Linux
runner or release asset. Not on Windows: `install-viewer` (default .md
handler).

## Release

Minor releases only (no patch on top of the latest). On Colossus:

1. Bump `const version` in `cmd/vellum/main.go`, the STABILITY snapshot
   line, and add `.release-notes-v<ver>.md`; `cv gate`; push master.
2. `cv release-dist`, then `scripts/release-publish.sh --skip-brew`
   (GitHub release, tap via tapper, `bucket/vellum.json`). Commit the
   scoop manifest after `cv gate`; push. (`cv release` does the same
   but runs `brew upgrade` itself — dry-run first when other formulae
   may upgrade.)
3. `brew upgrade marcelocantos/tap/vellum`, then **`make verify-release`**
   (`VERSION=` overrides the expected version; default is the latest
   tag). Non-zero means the release is broken. It uses only the Homebrew
   install and the supervisord program `vellum-view-release`
   (`/opt/homebrew/bin/vellum serve-view` on 127.0.0.1:18744, separate
   cache via a private HOME; template `supervisor/vellum-view-release.ini`).
   It checks the installed version; restarts the release service and
   requires the same pid after 60s plus the right MCP serverInfo
   (`scripts/post-release-check.sh`); converts the fixtures in
   `test/verify-release/` md→PDF/HTML/PPTX and DOCX→md with content
   assertions; fail-fast without weasyprint; HTTP MCP tools/list and
   convert both ways; the viewer page and offline `/_vellum/katex/`;
   same-page `#anchor` links in the viewer page and the PDF.
   Clipboard is skipped (no guaranteed GUI session). It is not part of
   `cv gate`.

Dev and release run side by side: `vellum-view` (127.0.0.1:18742) serves
`supervisor/run-view.sh`, which prefers the checkout's `./vellum` build;
`vellum-view-release` serves the Homebrew binary.

## Gates

profile: base
override:
  - pr-workflow: skip

Direct push to `master` is the default delivery path (Colossus gated-push).
`cv gate` is the local oracle; keep `tests-exist`. `/push` must not open a
PR for this repo unless the user explicitly asks for one on a non-default
branch. Never auto-merge remote PRs.

## Delivery

Pushed to master (Colossus gated-push; no mandatory GitHub PR).

## Followable work

Bullseye targets in `bullseye.yaml` (via the bullseye MCP tools) are the sole record of followable work.
