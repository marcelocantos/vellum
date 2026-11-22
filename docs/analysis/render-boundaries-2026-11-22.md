# Render boundaries: what a document can cause, by entry point

Analysis of `master` at `c47e428` (branch `filler/analysis-2026-11-22`),
2026-11-23. Read-only: no product code or test changed. Reproductions run
against a tree build (`go build ./cmd/vellum`) with a private `HOME`, on
random free loopback ports, in a scratch directory under `scratch-root`;
the installed viewers (18742, 18744), their LaunchAgents and the Homebrew
install were not touched. No network beyond localhost.

**Status: first pass (code reading only). Sections below are filled as
reproductions run.**

## 1. The system, from the code

A document enters vellum four ways. Each picks a different renderer chain
and a different trust posture.

| Entry | Code | Markdown → HTML | Sanitised | Fragments (math, Mermaid) | Sink engines and their reach |
|---|---|---|---|---|---|
| Viewer page (browser GET of `/<abs path>.md`) | `viewer/server.go` `handlePath` → `serveMarkdown` → `cachedHTML` → `convert.Render` (`Defer`) | goldmark, `html.WithUnsafe` | yes: `safehtml.Document` (allow-list), then a nonce CSP (`viewer/csp.go`) | deferred; `/_vellum/fragment` renders on demand, output through `safehtml.Rendered` | the user's browser: `img-src *`, `style-src 'self' 'unsafe-inline' https:`, `font-src 'self' data: https:`, `connect-src 'self' ws://<host>`, script by nonce only |
| Viewer actions (PDF, clipboard, reveal, task toggle, edit) | `viewer/actions.go`, `task.go`, `edit.go` | as the page | yes | rendered now, scrubbed | PDF: `InlineResources` (local via `readServed`, remote via `viewer/fetch.go`) then WeasyPrint `--allowed-protocols=data` / Prince `--no-network --no-local-files`; clipboard: AppKit (fetches nothing) or pandoc `--sandbox` |
| CLI `vellum convert` | `cmd/vellum/main.go` → `convert.Run` (`convert/run.go`) | goldmark, `WithUnsafe` | **no** | rendered now | PDF: `WritePDF`, engine with `--base-url=file://<doc dir>/` and full reach; RTF: pandoc `HTMLToRTF` with `--resource-path`; pptx: pandoc with `--resource-path`; HTML: raw page written out |
| MCP `convert` (daemon `/mcp`, or `vellum --mcp`) | `mcp/server.go` → `convert.Run` | as CLI | **no** | as CLI | as CLI; `from: content` has no base directory, so relative names resolve against the output file's folder |
| Import (rich text, clipboard, docx, HTML, PDF) | `importer/importer.go`, `convert/run.go` `ingest` | pandoc reader → gfm | no | n/a | pandoc `--extract-media=<media dir>` (no `--sandbox`); Poppler for PDF |

The viewer serves files only inside *served roots*: `Server.Roots` plus a
folder granted when a Markdown document is opened "by the user", which
`openedByUser` (`viewer/roots.go`) defines as a request with
`Sec-Fetch-Site: none` **or with no fetch metadata at all**. Grants persist
in `<cache>/roots.json` (64, most recent first). Every path-taking route
resolves symlinks and opens through `os.Root`.

## 2. Invariants

Status values: *holds (argument)*, *holds (tested)*, *violated
(reproduced)*, *by design* (the code chooses this; listed because it
crosses the boundary the question asks about), *unknown*.

| ID | Invariant | Where maintained | How it could break | Status |
|---|---|---|---|---|
| V1 | A document shown in the viewer runs no script of its author's in the viewer's origin | `safehtml.Document` (`convert/convert.go:270`), nonce CSP (`viewer/csp.go`), `lockdownCSP` sandbox on raw files | allow-list hole; a fragment (KaTeX/Mermaid SVG) carrying script past `safehtml.Rendered`; string rewriting after the sanitiser | pending |
| V2 | A viewed document reads no file outside the served roots | `confine` + `os.Root` (`viewer/roots.go`) | symlink race; a route that skips `openServed` | pending |
| V3 | Nothing a document contains can add a served root | `openedByUser` (`viewer/roots.go:68`) | any non-browser client that fetches a URL the document names sends no fetch metadata and is read as the user (🎯T67) | pending |
| V4 | A viewed document makes no network request except to load images, fonts and stylesheets | CSP `connect-src`, `form-action 'none'`, no script | by design it loads any `http(s)` image, font or `https` stylesheet on render (tracking, LAN/localhost GETs) | pending |
| V5 | A document (or its diagram) cannot restyle or overlay the viewer's chrome | `cleanStyle`, reserved `vellum-` class prefix, `<style>` dropped by `safehtml.Document` | `safehtml.Rendered` keeps `<style>` in Mermaid SVG; inline SVG style applies to the whole page | pending |
| V6 | The viewer's PDF of a document contains no file outside the served roots and its engine makes no request | `InlineResources`, `WriteSelfContainedPDF`, Mermaid confine hook | tested by 🎯T66/🎯T68 | pending (retest) |
| C1 | A CLI or MCP conversion reads no file outside the document's folder unless the user names it | none | WeasyPrint/Prince load `file:` and absolute paths; WeasyPrint embeds `rel=attachment` targets; pandoc embeds images by absolute path | pending |
| C2 | A CLI or MCP conversion makes no network request the document names unless asked | none | engines and pandoc fetch `http(s)` | pending |
| C3 | A CLI or MCP conversion cannot get a running viewer to serve a folder | none (🎯T67) | engine GET of `http://127.0.0.1:<viewer>/<folder>/x.md` without fetch metadata | pending |
| C4 | Mermaid rendered for CLI/MCP reads no image outside the document's folder | confine hook in `mmdcCommand` | hook only on some paths | pending |
| C5 | KaTeX renders no link or image a formula names | `katexRenderScript` options | `trust: true` | pending |
| I1 | Importing rich text reads no local file and fetches nothing the content names | none | pandoc `--extract-media` copies every `<img src>` it can read or download | pending |
| I2 | Import writes only inside the media directory | pandoc's media naming | a crafted docx/HTML media name | pending |

## 3. Confirmed defects

Pending.

## 4. Design findings

Pending.

## 5. Unconfirmed suspicions

Pending.

## 6. Coverage

Read in full so far: `viewer/server.go`, `viewer/roots.go`,
`viewer/csp.go`, `internal/safehtml/safehtml.go`; `convert/convert.go`
lines 120–300, `convert/run.go` lines 262–510, `convert/pptx.go` 100–180,
`importer/importer.go` 40–200, both backend files.
