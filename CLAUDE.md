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

Markdown → pptx path: Mermaid preprocessor (PNG, inlined as images) → pandoc gfm reader → pptx writer with `--reference-doc` = the selected template (`convert/pptx.go`). An omitted or default template leaves reference selection to installed pandoc; no reference deck is bundled.

### Key packages

| Package | Role |
|---------|------|
| `cmd/vellum/` | CLI entry point |
| `convert/` | Markdown → HTML/PDF pipeline; unified `Run` router; Backend interface. Math and Mermaid are source preprocessors (`convert/katex.go`, `convert/mermaid.go`), not goldmark extensions. `convert/pptx.go` is the pandoc-backed PowerPoint sink and template resolution. |
| `clipboard/` | Rich clipboard + file references. macOS: NSPasteboard via cgo, `Write` tries AppKit then pandoc HTML→RTF. Windows: pure-Go user32 (`clipboard_windows.go`): CF_HTML + RTF (pandoc) + Unicode text, PDF read, CF_HDROP file lists. |
| `importer/` | Rich-text → Markdown via pandoc; PDF via Poppler |
| `adf/` | Markdown → Confluence ADF (library only; not a `convert.Run` sink) |
| `internal/pandoc/` | pandoc export helpers: HTML → RTF/plain (clipboard fallback) and Markdown → file (pptx); not a public API |
| `internal/mdmath/` | Finds `$…$` / `$$…$$` math outside code, with code located by a goldmark parse (CommonMark fences, indented blocks, code spans); used by `convert` (KaTeX placeholders) and `adf` (latex fences). |
| `internal/safehtml/` | Allow-list HTML sanitiser (x/net/html parse → filter → render) for the viewer: `Document` for a document's own HTML, `Rendered` scrubs KaTeX/Mermaid fragments. The viewer pairs it with a nonce CSP (`viewer/csp.go`). |
| `internal/svgfit/` | How wide an SVG should be drawn on a page of text: `Measure` reads its natural size and its text sizes (font-size attributes, inline styles, simple `<style>` rules, transform and viewBox scale) without a browser; `Fit.GrowEm` is the policy (grow until small print reaches 0.75em or typical text 1em). The viewer finds the SVGs that stand alone on a page (`viewer/figures.go`: inline `<svg>`, SVG images in links, figures, `data:` URLs; remote ones measured after load by `viewer/fit_remote.go`), writes the result as CSS custom properties (`viewer/fit.go`) and `chrome.css` does the sizing. `make test-svg-fit` (`scripts/svg-fit-journey.cjs`) checks the drawn sizes in Chrome and Firefox; not part of `cv gate`. |
| `internal/xdg/` | Per-user config directory (`$XDG_CONFIG_HOME/vellum`, else `~/.config/vellum`; `%APPDATA%\vellum` on Windows) shared by `config/` and template lookup |
| `internal/fileurl/`, `internal/shim/`, `internal/hint/` | Portability helpers: drive-letter-safe `file://` URLs, running npm `.cmd` shims (mmdc) as `node <script>` on Windows, per-OS install/service hints |
| `config/` | User configuration loaded from `~/.config/vellum/config.yaml` |
| `mcp/` | MCP server (single `convert` tool; streamable HTTP + stdio) |
| `embed/` | Embedded assets (CSS, HTML template, KaTeX stylesheet + fonts under `katex/`, refreshed by `scripts/vendor-katex.sh`) |
| `internal/testdeps/` | Test gate for external converters (`VELLUM_REQUIRE_DEPS`) |
| `viewer/` | Localhost daemon (HTML view + chrome + `/mcp`); cached PDF open; macOS default .md handler (`install-viewer`); Windows logon Scheduled Task (`install-service`). `viewer/theme.css` holds the colour tokens for both themes, shared by the document chrome (`chrome.css`, `chrome.js`, markup in `chrome.go`) and the homepage (`home.css`, `home.go`); `make test-chrome` (`scripts/viewer-chrome-journey.cjs`) drives the chrome in Chrome and Firefox and `make test-table-layout` checks the reading column and tables. Neither is part of `cv gate`. `viewer/t3.go` is the T3 Code plugin: a second loopback listener (`Server.T3Addr`) that T3's gateway reaches, with only the routes an embedded page needs and no request that grants a folder; `make test-t3` (`scripts/viewer-t3-journey.cjs`) drives it in a sandboxed frame through a gateway that applies T3's rules. Every journey but that one runs its `serve-view` with `VELLUM_T3_ADDR=off`, so it never advertises to a real T3. `viewer/edit.go` is the source editor's read/write endpoint and bundle server. `viewer/roots.go` confines every path-taking route to the served folders (`Server.Roots` plus folders the user opened a document in), opening through `os.Root`. |
| `editor/` | npm project for the viewer's CodeMirror 6 editor. `npm ci && npm run build` writes the embedded bundle to `viewer/editor/` (commit it with the change); `make test-editor` runs the Playwright journey (`editor/journey.cjs`) against a fresh build. Not part of `cv gate`. |

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
Colossus (`ssh hms-vm`) by the gate itself: `cv windows`
(`scripts/windows-gate.sh`) copies the working tree there, runs
`VELLUM_REQUIRE_DEPS=1 go test ./... -count=1` (no `-race`: windows/arm64
has no race detector) and allows only the skips listed in the script,
each tied to an open Windows target. It wakes a stopped or suspended
VM and fails when it cannot reach it. Do not copy a tree to the VM by
hand with macOS `tar`: without `COPYFILE_DISABLE=1` it adds AppleDouble
`._` files, which break `FuzzDocument`. The Windows
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
   (GitHub release, tap via tapper, `bucket/vellum.json`).
   `release-dist` signs the macOS binary with Canticode's Developer ID
   from the login keychain (`scripts/release-signing.sh`: hardened
   runtime, secure timestamp, identifier `com.marcelocantos.vellum`) and
   refuses to package without it. Not notarized: Homebrew does not
   quarantine what it installs. Commit the
   scoop manifest after `cv gate`; push. (`cv release` does the same
   but runs `brew upgrade` itself — dry-run first when other formulae
   may upgrade.)
3. `brew upgrade marcelocantos/tap/vellum`, then **`make verify-release`**
   (`VERSION=` overrides the expected version; default is the latest
   tag). Non-zero means the release is broken. It uses only the Homebrew
   install and the LaunchAgent `com.marcelocantos.vellum-view-release`
   (`/opt/homebrew/bin/vellum serve-view` on 127.0.0.1:18744, separate
   cache via a private HOME; template
   `launchd/com.marcelocantos.vellum-view-release.plist`, installed by
   `launchd/install.sh vellum-view-release`).
   It checks the installed version and that the installed binary carries
   that signature; restarts the release service and
   requires the same pid after 60s plus the right MCP serverInfo
   (`scripts/post-release-check.sh`); converts the fixtures in
   `test/verify-release/` md→PDF/HTML/PPTX and DOCX→md with content
   assertions; fail-fast without weasyprint; HTTP MCP tools/list and
   convert both ways; the viewer page and offline `/_vellum/katex/`;
   same-page `#anchor` links in the viewer page and the PDF.
   Clipboard is skipped (no guaranteed GUI session). It is not part of
   `cv gate`.

Dev and release run side by side as LaunchAgents: `vellum-view`
(127.0.0.1:18742, `launchd/install.sh`) runs `launchd/run-view.sh`, which
prefers the checkout's `./vellum` build; `vellum-view-release` serves the
Homebrew binary. Before running the checkout's build, `run-view.sh`
signs it with the release's Developer ID as `com.marcelocantos.vellum.dev`
(🎯T86). The linker's ad-hoc signature makes every rebuild a new app to
macOS privacy controls, and an unanswered Removable Volumes prompt holds
every process's access to that volume, so do not bypass the script by
running `./vellum serve-view` under the LaunchAgent another way. Restart with
`launchctl kickstart -k gui/$(id -u)/com.marcelocantos.<program>`, check
with `launchctl print gui/$(id -u)/com.marcelocantos.<program>` (or
`svc restart|status <program>`); logs are
`~/.local/var/log/<program>.log`.

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
