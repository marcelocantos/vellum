# Entropy audit — vellum — 2026-08-22

Full-mode audit (entropy + hygiene). First report on this repository; no prior `docs/audits/` baseline.

## Executive summary

- **Snapshot:** `/Users/marcelo/work/github.com/marcelocantos/vellum`, branch `master`, HEAD `f30a79ffa17b792177610c80e9543477b8c44015` (`v0.13.0-9-gf30a79f`, 2026-08-17). Working tree dirty: `M clipboard/clipboard_darwin_test.go` (user-owned; +64 lines of a machine-wide pasteboard lock). `master` is 9 commits ahead of `origin/master`. Date of this audit: 2026-08-22.
- **Scope:** committed Go packages, CLI/MCP/viewer surfaces, `cvfile`, GitHub workflows, and agent/stability docs. **Exclusions:** generated/vendored trees (none); fixture binaries under `convert/testdata/corpus/` and `test/sample.pdf` (named, not mixed into production conclusions); untracked `./vellum` binary (gitignored); parent `go.work` at `/Users/marcelo/work/github.com/marcelocantos/go.work` (commands used `GOWORK=off`).
- **Headline mechanism:** the runtime DAG is a single `convert.Run` hub with no import cycles, but several pre-unification artefacts still claim authority — a 2026-04-11 handover that describes a different product, a weaker `Makefile` green next to `cvfile`, a leftover `convert.Convert` PDF writer, and a STABILITY catalogue two minors behind — so the same change still has more than one place to be true.
- **Highest-consequence findings:** ENT-001 (competing `make bullseye` vs `cv gate`); ENT-002 (`release.yml` ships after a Linux unit job that does not run the declared gate); ENT-003 (`docs/HANDOVER.md` still documents Prince-default / MCP-not-implemented).
- **Unverified residue:** full `cv gate` (`go test ./... -race` twice, skip-census) was not run in this session; `govulncheck` is not installed; `staticcheck` cannot compile a `go 1.26.1` module (built with Go 1.25); live Aperture-gateway clipboard path (🎯T23) was not re-probed; PDF visual fidelity and real `mmdc` rendering remain the corpus's declared residue.

## Scope and exclusions

Analysed: `cmd/vellum`, `convert`, `clipboard`, `mcp`, `importer`, `viewer`, `config`, `adf`, `embed`, `docs`, `internal/pandoc`, `internal/testdeps`, `cvfile`, `Makefile`, `.github/workflows/{ci,release}.yml`, `STABILITY.md`, `README.md`, `CLAUDE.md`, `docs/{HANDOVER,agents-guide,audit-log}.md`, `bullseye.yaml` (intent ledger, not code).

Skipped as fixtures / non-production: `convert/testdata/corpus/**` (frozen LibreOffice/textutil artefacts; provenance is itself an oracle), `test/sample.md` / `test/sample.pdf`, gitignored `./vellum`.

`convert/extensions/` is documented in `CLAUDE.md` but is an empty untracked directory (`git ls-files convert/extensions` is empty). Treated as a documentation contradiction, not as a skipped tree.

Languages judged: Go (sole module language). No committed `*.sh`. Makefile/`cvfile` recipes inspected as glue. HTML/CSS under `embed/` are PDF templates, not a web frontend. No Python/C++/Rust/SQL product code.

## Commands run

| Command | Version / notes | Exit | Shipped vs auxiliary | Limitations |
|---|---|---|---|---|
| `git rev-parse --abbrev-ref HEAD`; `git rev-parse HEAD`; `git status --porcelain=v1 -b` | git | 0 | provenance | Initial snapshot before any write |
| `GOWORK=off go list ./...` | go1.26.4; `GOWORK=off` required because parent `~/work/github.com/marcelocantos/go.work` otherwise rejects `./...` | 0 | auxiliary | Workspace is outside this repo |
| Python walk of `go list -json ./...` import graph (internal edges only) | python3 3.13.0 | 0 | auxiliary | Test-only imports excluded |
| `GOWORK=off gofmt -l .` | gofmt from go1.26.4 | 0, empty | shipped (`cv fmt`) | Format only |
| `GOWORK=off go vet ./...` | go1.26.4 | 0 | shipped (`cv vet`) | |
| `cv fmt`; `cv vet` | cv v0.10.0 | 0 | shipped | |
| `GOWORK=off VELLUM_REQUIRE_DEPS=1 go test ./adf ./mcp ./config ./importer ./cmd/vellum ./convert ./viewer ./clipboard -count=1` | converters present: pandoc, weasyprint, pdftoppm, pdftotext, node, mmdc, prince | 0 | **partial shipped path** | Not `-race`; skip-census not run; `internal/pandoc` has no `_test.go` |
| `go build -o /tmp/vellum-audit-bin ./cmd/vellum` then `--version` / `--help` / `--help-agent` | prints `0.13.0` | 0 | shipped (`cli-contract` subset) | Did not use cv's `./vellum` artefact |
| `cv gate` | — | **not run** | shipped definition of green | Race suite + skip-census would re-execute `go test ./...` twice; residue |
| `/Users/marcelo/.claude/skills/hygiene/hygiene_check.py` | uv-run | 1 (`FileNotFoundError: hygiene.yaml`) | hygiene | No posture file; not initialized |
| `staticcheck ./...` | staticcheck built with go1.25.0 | compile fail | auxiliary | Module requires go1.26.1 |
| `govulncheck` | not on PATH | — | — | No vuln scan this run |
| `git log --name-only` churn; `git log` on HANDOVER/Makefile/STABILITY/cvfile | | 0 | history | |

Converters on PATH at audit time: `pandoc`, `weasyprint`, `pdftoppm`, `pdftotext`, `node`, `mmdc`, `prince` (all Homebrew).

## Observed architecture

### Entry points and deployable unit

One Go module (`github.com/marcelocantos/vellum`, go 1.26.1). One binary: `cmd/vellum` (`const version = "0.13.0"`). Modes: CLI sugars, `vellum convert --from/--to`, `vellum --mcp`, `vellum view` / `install-viewer`. Homebrew formula (written by `release.yml`) wraps the binary with a PATH-prepending `sh` launcher.

### Runtime DAG (observed, no cycles)

```
cmd/vellum → config, convert, docs, mcp, viewer
mcp        → config, convert
viewer     → convert
config     → convert          (Style type lives in convert)
convert    → clipboard, embed, importer
clipboard  → internal/pandoc
adf        → (goldmark only; nothing in-repo imports it)
importer, embed, docs, internal/pandoc, internal/testdeps → no internal deps
```

12 packages, 0 import cycles. `convert` is the fan-in hub. `adf` is a library island.

Declared architecture (`CLAUDE.md`, `README.md`) agrees on the media-orthogonal pipeline:

```
from media → normalise (pandoc import and/or goldmark HTML) → to media
```

Markdown→PDF: goldmark → HTML template → WeasyPrint (default) / Prince (opt-in).

### Public surfaces

- CLI flags/subcommands catalogued in `STABILITY.md` (snapshot claims v0.11.0).
- MCP: one tool `convert` (`mcp/server.go`); DTO types `Endpoint` / `FilePair` / `ConvertInput` duplicated from `convert` with jsonschema tags; mapped by `toEndpoint`.
- Go packages listed in STABILITY; `adf` and `internal/pandoc` are absent from that catalogue.
- Config: `~/.config/vellum/config.yaml` (`config.Load`).

### Cross-cutting concerns

- External binaries: WeasyPrint/Prince, node+katex, mmdc, pandoc, poppler. `convert.CheckDeps` covers renderer + node + mmdc only. `importer.CheckDep` is lazy (pandoc). Poppler is look-pathed inside PDF import.
- Soft vs hard errors: `convert.SoftError` after output is produced (Mermaid; clipboard fallback).
- Platform: clipboard, file_reference, viewer are darwin-only (`//go:build darwin`).

### Declared vs observed rules

| Rule | Status |
|---|---|
| `cv gate` is the definition of green (`CLAUDE.md`, `cvfile` header, `ci.yml`) | **Enforced** on PR/push via macOS `cv gate`. **Contradicted** by `Makefile` `bullseye` and by `release.yml` `test` |
| Single MCP tool `convert`; retired names not advertised | **Enforced** (`mcp/server_test.go`) |
| Media-orthogonal `convert.Run` is the router | **Observed**; CLI sugars call `Run`. Viewer PDF still calls `convert.Convert` |
| WeasyPrint default, Prince opt-in | **Agreed** in code/README/CLAUDE; **contradicted** by `docs/HANDOVER.md` |
| Source-level math/mermaid preprocessors, not goldmark extensions | **Observed** (`convert/katex.go`, `convert/mermaid.go`); `CLAUDE.md` still lists `convert/extensions/` |
| ADF is a library (🎯T26), not a convert sink yet | **Observed**; no CLI/MCP wiring. Intent: 🎯T27 publish map |
| Clipboard rich-text must survive missing TextKit agent (🎯T23) | **Code landed** (AppKit then pandoc fallback); target still `identified`; dirty test is a pasteboard lock, not the property |

## Dimension vector

| Dimension | State | Evidence summary | Change from baseline |
|---|---|---|---|
| Architecture topology | concern | Clean DAG and `Run` hub; leftover `Convert` PDF writer, unwired `adf`, documented-but-absent `convert/extensions/` | — (first audit) |
| Redundancy / sources of truth | concern | HANDOVER vs current product; Makefile vs cvfile; STABILITY vs `clipboard.Write`; dual goldmark; MCP DTOs | — |
| Change amplification | concern | PDF mermaid-PNG + backend.Render written twice; markdown-feature work must touch `convert` and `adf`; CLI sugars live in a 764-line `main.go` (14 commits, highest churn) | — |
| Local code quality | healthy | Linear `Run` ingest/materialize; clipboard seams; darwin tags; gofmt/vet clean; no cycle; `main.go` is long but is flag sugar, not hidden graph logic | — |
| Correctness / verification | healthy | Corpus provenance oracle, `VELLUM_REQUIRE_DEPS` ratchet, MCP advertisement tests, T23 sandbox + pandoc unit tests; residue listed below | — |
| Security / dependencies | concern | KaTeX CSS from jsdelivr at PDF render; `html.WithUnsafe()` on owner markdown; yaml.v3 pin from 2020-03; no scanner in CI | — |
| Build / release / operations | concern | macOS `ci.yml` → `cv gate` is honest; `release.yml` tests on ubuntu without converters; Homebrew wrapper PATH is sound | — |
| Documentation / governance | concern | HANDOVER fossil (last commit `103795e` 2026-04-11); STABILITY snapshot v0.11.0; hygiene undeclared; bullseye is the followable-work record | — |

Do not aggregate these states into a scalar.

## Findings

### ENT-001: Two incompatible definitions of green

- **Priority:** P1
- **Dimensions:** Correctness / verification; Build / release / operations; Redundancy
- **Status:** observed fact
- **Evidence:**
  - `cvfile:71-77` — `!gate: fmt vet test skip-census cli-contract`; `!bullseye: gate` plus dirty-tree check. `!test` (`cvfile:47-48`) is `VELLUM_REQUIRE_DEPS=1 go test ./... -count=1 -race`.
  - `Makefile:3-9` — `bullseye` is `gofmt`, `go vet`, `go build`, `go test ./...` **without** race, **without** `VELLUM_REQUIRE_DEPS`, **without** skip-census, **without** CLI contract. Last edited `48b5c44` (2026-04-25), before `cvfile` existed (`79077de` 2026-08-07).
  - `CLAUDE.md:46-50` and `.github/workflows/ci.yml:8-11,32-33` name `cv gate` as the definition of green.
  - bullseye convergence (global tool) prefers `make bullseye` when a Makefile target exists.
- **Mechanism:** a contributor or convergence scan can report green while converter tests skipped, races unexamined, and skip-census unratcheted. Two oracles for one property will drift; the weaker one is the one that gets used when it is easier.
- **Blast radius:** every “invariants hold” claim that went through Make rather than `cv`; CI on `master`/PRs is not itself wrong.
- **Counterevidence checked:** `ci.yml` does call `cv gate` on macOS. Local developers following `CLAUDE.md` use `cv`. The Makefile target is still the name `bullseye` that standing-invariants tooling looks up first.
- **Smallest coherent remediation:** make `Makefile` `bullseye` a one-line delegate to `cv bullseye` (or delete the Makefile target and document `cv bullseye` as the hook). Do not keep a second recipe.
- **Verification:** `make bullseye --dry-run` / recipe text equals `cv bullseye`; a skip injected under `VELLUM_REQUIRE_DEPS=1` fails both commands.
- **Ratchet candidate:** architecture/Make test, or a `hygiene.yaml` `make_target: bullseye` whose `command` evidence is `cv bullseye` after the recipes converge.

### ENT-002: Release workflow tests a Linux subset and still names Prince as the skip reason

- **Priority:** P1
- **Dimensions:** Build / release / operations; Correctness / verification
- **Status:** observed fact
- **Evidence:**
  - `.github/workflows/release.yml:10-22` — `runs-on: ubuntu-latest`; `go test ./...` with no converter install; step name still says “preprocessor + CLI unit tests only; pipeline test is skipped without Prince”.
  - `.github/workflows/ci.yml:17-33` — macOS, `brew install pandoc weasyprint poppler`, `cv gate`.
  - `on: release: types: [published]` (`release.yml:3-5`): publishing a GitHub release runs this workflow; it does not `needs:` the `ci.yml` gate job.
- **Mechanism:** a published tag can upload darwin/linux binaries after a job that never ran the import corpus, PDF backends, clipboard, or skip-census. The step name encodes a world in which Prince was required and pipeline tests were optional — false since 🎯T9 / v0.4.0 (`docs/audit-log.md` 2026-05-17).
- **Blast radius:** every GitHub Release asset and the Homebrew formula job that `needs: build` after this test.
- **Counterevidence checked:** `/release` practice in `docs/audit-log.md` waits for PR CI green before cutting the tag, so a careful human path is protected. The workflow file does not encode that. Linux is explicitly out of product scope (`CLAUDE.md:53-54`; `cvfile:9-10`).
- **Smallest coherent remediation:** replace `release.yml` `test` with the same macOS `cv gate` recipe as `ci.yml` (or `needs:` a workflow_run of CI). Update the step name.
- **Verification:** a missing `weasyprint` on the release test job fails the release; the string “skipped without Prince” is absent from workflows.
- **Ratchet candidate:** `hygiene.yaml` `ci_job: release.yml#test` plus a `ci_step` or `command` that asserts `cv gate`; fail if the Prince-skip comment returns.

### ENT-003: `docs/HANDOVER.md` is a second, contradictory architecture document

- **Priority:** P2
- **Dimensions:** Documentation / governance; Redundancy; Architecture topology
- **Status:** observed fact
- **Evidence:**
  - Last commit on the file: `103795e` 2026-04-11 (“Release v0.1.0”).
  - `docs/HANDOVER.md:76` pipeline ends at “Prince → PDF”; `:123-124` “HTML → PDF | Prince 16.2”; `:124` “MCP SDK (planned)”; `:202-203` “`--mcp` flag is stubbed … `mcp/` directory exists but is empty”; `:216` “no Go test suite exists”; `:228` prerequisites still list Prince first.
  - Current product: WeasyPrint default (`convert/backend.go:33-37`), MCP implemented (`mcp/server.go:63-71`), tests exist (this audit ran them).
- **Mechanism:** agents and humans who open HANDOVER (the filename invites that) will implement or review against a product that has not existed since v0.4.0. That is a competing source of truth, not a historical annex — nothing in the file says it is frozen.
- **Blast radius:** any session that treats HANDOVER as current; already visible in 🎯T1's context block which still quotes the stub error (historical, inside bullseye).
- **Counterevidence checked:** `CLAUDE.md` and `README.md` are current. HANDOVER is not embedded (`docs/embed.go` embeds `agents-guide.md` only). It is still a tracked, prominently named doc.
- **Smallest coherent remediation:** retitle to a dated historical note with a one-line “superseded by CLAUDE.md / STABILITY.md”, or delete it and keep `docs/audit-log.md` as the chronology.
- **Verification:** `rg -n 'MCP SDK \(planned\)|prince.*default|mcp/ directory exists but is empty' docs/HANDOVER.md` is empty, or the file opens with `Status: historical (2026-04-11)`.
- **Ratchet candidate:** a docs test or hygiene `file` evidence that HANDOVER either does not exist or contains a `historical` marker.

### ENT-004: PDF writing is implemented twice

- **Priority:** P2
- **Dimensions:** Change amplification; Architecture topology; Redundancy
- **Status:** observed fact
- **Evidence:**
  - `convert/convert.go:100-127` — `Convert` reads a file, `RenderFile` with `MermaidPNG`, `ResolveBackend`, `backend.Render`.
  - `convert/run.go:463-489` — `writeFileOutput` for PDF does the same `Render` + `MermaidPNG` + `ResolveBackend` + `backend.Render` sequence on already-ingested Markdown.
  - Sole production caller of `Convert` besides its definition: `viewer/viewer.go:250-254` (PDF view cache). CLI and MCP PDF go through `convert.Run` → `writeFileOutput` (`cmd/vellum/main.go:744`, `mcp/server.go:132`).
- **Mechanism:** a change to PDF/A flags, temp-file handling, mermaid format, or backend error wrapping must be applied in both functions or the viewer PDF path silently diverges from `vellum convert` / MCP (different mermaid format, different baseDir, different mkdir/error text).
- **Blast radius:** `vellum view --pdf` / `install-viewer` vs every other PDF sink.
- **Counterevidence checked:** both currently force PNG mermaid and pass `Style.PDFA`. Tests cover `Run` more densely than `Convert`. The duplication is small (~25 lines) but it is the PDF contract.
- **Smallest coherent remediation:** make `Convert` a thin wrapper that builds a `Request` and calls `Run` (or extract one `writePDF(html, path, opts, baseDir)` used by both).
- **Verification:** a mutation of mermaid format or PDFA handling in `writeFileOutput` fails a viewer PDF test (or `Convert` no longer contains its own `ResolveBackend` call).
- **Ratchet candidate:** `rg 'func Convert\('` plus an architecture test that `Convert`'s body does not call `backend.Render` directly.

### ENT-005: Fail-fast dependency checks do not cover the CLI convert path and omit pandoc/poppler

- **Priority:** P2
- **Dimensions:** Architecture topology; Correctness / verification
- **Status:** observed fact
- **Evidence:**
  - `convert/deps.go:28-45` — `RequiredDeps` = backend binary + `node` + `mmdc`. Comment at `:23-27` admits katex is not look-pathed.
  - Callers of `CheckDeps`: `cmd/vellum/main.go:760` (`runMCP` only), `viewer/viewer.go:251` (PDF view only). `runCLI` (`main.go:717-748`) and `runConvert` call `convert.Run` with no preflight.
  - Pandoc is checked lazily in `importer/importer.go:35-40,78-79`. Poppler is not in `RequiredDeps`.
  - 🎯T3 acceptance (achieved) still says startup checks `prince`, `node`, `katex`, and `mmdc` before conversion; HANDOVER `:276-278` repeats that.
- **Mechanism:** CLI PDF fails inside WeasyPrint exec; CLI import fails inside pandoc; MCP startup refuses to start if `mmdc` is missing even for a DOCX→Markdown call. The fail-fast story is split across three functions and does not match T3 or the MCP-vs-CLI symmetry.
- **Blast radius:** first-run UX; launchd/MCP clients vs CLI scripts; anyone implementing `vellum doctor` (STABILITY 1.0 gap).
- **Counterevidence checked:** lazy pandoc is deliberate (`docs/audit-log.md` v0.5.0: “PDF-only users never get a pandoc error at startup”). That is a documented exception for pandoc, not for skipping CheckDeps on CLI PDF.
- **Smallest coherent remediation:** call `CheckDeps` from the PDF branch of `writeFileOutput` / `Run` (so CLI and MCP share it); keep pandoc lazy; document that `mmdc`/`node` are required only when the document needs them, or split “hard” vs “feature” deps.
- **Verification:** `PATH` without `weasyprint` on `vellum file.md` errors with `RequiredDeps` wording before a WeasyPrint exec error; MCP-only CheckDeps grep is empty.
- **Ratchet candidate:** a CLI test with a stubbed PATH.

### ENT-006: PDF/HTML math styling depends on a live jsDelivr fetch

- **Priority:** P2
- **Dimensions:** Security / dependencies; Build / release / operations
- **Status:** observed fact
- **Evidence:**
  - `convert/convert.go:28` — `katexCSSLink = <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/katex@0.16.21/dist/katex.min.css">`.
  - `:173` injects it into every assembled page's `<head>`.
  - `convert/backend_weasyprint.go:61` runs `weasyprint` on that HTML; WeasyPrint fetches remote stylesheets unless blocked.
- **Mechanism:** offline, air-gapped, or jsDelivr-blocked renders produce KaTeX HTML without its CSS (unreadably laid out math). A CDN compromise or MITM supplies CSS into the PDF. This is a runtime network dependency not listed in `RequiredDeps` or the Homebrew formula.
- **Blast radius:** every HTML view and PDF that contains math; CI does not pin or vendor this CSS.
- **Counterevidence checked:** KaTeX *HTML* is rendered locally via node (`convert/katex.go`). Only the stylesheet is remote. `Options.CSS` / `HeadExtra` can overlay but do not replace the default link. STABILITY `:223-226` documents `VELLUM_DEBUG_HTML` but not the CDN.
- **Smallest coherent remediation:** embed `katex.min.css` next to `embed/github.css` (or ship it from the node `katex` package at render time).
- **Verification:** `rg cdn.jsdelivr.net` is empty; a PDF built with network blocked still contains KaTeX layout rules.
- **Ratchet candidate:** `rg` in `cvfile` or a test that assembled HTML has no `https://` stylesheet links.

### ENT-007: STABILITY catalogue is two minors behind the shipped API

- **Priority:** P2
- **Dimensions:** Documentation / governance; Redundancy
- **Status:** observed fact
- **Evidence:**
  - `STABILITY.md:24` — “Snapshot as of **v0.11.0**”. Last commit `40acb01` 2026-08-05 (the v0.11.0 PR). Tags since: `v0.12.0`, `v0.13.0`. Binary `--version` is `0.13.0`.
  - `STABILITY.md:63` still lists `func Write(p Payload) error`. Code: `clipboard/clipboard.go:69` `func Write(p Payload) (WriteReport, error)` (unreleased relative to origin, landed at HEAD).
  - No STABILITY entries for `adf`, `internal/pandoc`, `WriteReport` / `Route`, or the pandoc clipboard fallback.
- **Mechanism:** the document that exists to freeze the 1.0 surface no longer matches the tree, so 1.0 readiness and agent review both use a stale inventory. Signature drift is already present (`Write`).
- **Blast radius:** 1.0 settling clock (`STABILITY.md:313-315` last reset 2026-07-31 / v0.8.0); any consumer of the Go API.
- **Counterevidence checked:** pre-1.0 allows change (`STABILITY.md:19-20`). The snapshot date is still a claimed inventory. `docs/agents-guide.md` is more current than STABILITY for MCP.
- **Smallest coherent remediation:** restamp the snapshot as part of `/release` (already implied by the 1.0 catalogue role); add `adf` as needs-review or “internal until a convert sink exists”.
- **Verification:** `rg 'Snapshot as of' STABILITY.md` matches `vellum --version`; `rg 'func Write' STABILITY.md` matches `clipboard.go`.
- **Ratchet candidate:** `cli-contract` compares `STABILITY.md` snapshot version to `const version`.

### ENT-008: RTF is a first-class format name that file sinks refuse

- **Priority:** P2
- **Dimensions:** Architecture topology; Correctness / verification
- **Status:** observed fact
- **Evidence:**
  - `convert/run.go:45` `FormatRTF = "rtf"`; `:606-607` `.rtf` → `FormatRTF`.
  - `convert/run.go:490-491` `writeFileOutput` default: `to.media file does not support format %q`.
  - 🎯T25 (identified, depends on 🎯T23) is exactly this capability; 🎯T23 context reports the owner workaround `pandoc file.md -t rtf | pbcopy`.
  - `internal/pandoc/pandoc.go:36-51` already implements HTML→RTF for the clipboard fallback.
- **Mechanism:** inference will happily decide `to.format=rtf` from an `.rtf` path, then refuse. The export helper the clipboard path just grew is not reused for files. Callers see a format the API named and then a dead end (owner report that spawned T23/T25).
- **Blast radius:** CLI `convert --to file` with `.rtf`; MCP `to.media=file` + `format=rtf`; any agent following STABILITY format constants.
- **Counterevidence checked:** `checkDisallowed` (`run.go:542-550`) only blocks PDF to content/clipboard, as T25's context already notes. Clipboard sink uses `FormatRich`, not `FormatRTF`. Tracked as 🎯T25 — do not start a parallel rewrite.
- **Smallest coherent remediation:** T25 as written: file RTF via `internal/pandoc.HTMLToRTF` with a resource path, no AppKit.
- **Verification:** T25 acceptance (round-trip bold/italic through pandoc; sandbox negative control).
- **Ratchet candidate:** matrix case `to.media=file, format=rtf` in `convert/matrix_test.go` once T25 lands (today it would fail).

### ENT-009: Agent architecture table documents a package that does not exist and omits one that does

- **Priority:** P3
- **Dimensions:** Documentation / governance; Architecture topology
- **Status:** observed fact
- **Evidence:**
  - `CLAUDE.md:28` — `| convert/extensions/ | Custom goldmark extensions (Mermaid, etc.) |`. `git ls-files convert/extensions` empty; mermaid is `convert/mermaid.go` (source preprocessor). Design decision against goldmark extensions is in HANDOVER `:81-86` and still true in code (`convert/convert.go:149-151`).
  - `adf/` exists (🎯T26, 2026-08-17) and is not in the CLAUDE package table.
- **Mechanism:** the first architecture map an agent reads sends it to create goldmark extensions in an empty directory — the opposite of the standing design.
- **Blast radius:** agent sessions using `CLAUDE.md` only.
- **Counterevidence checked:** `README.md` pipeline description is correct. CLAUDE was last edited `67baf80` 2026-08-08 (before `adf/`).
- **Smallest coherent remediation:** replace the extensions row with `convert/katex.go` + `convert/mermaid.go`; add `adf/` as library-only.
- **Verification:** `rg convert/extensions CLAUDE.md` empty; `rg '| adf/' CLAUDE.md` hits.
- **Ratchet candidate:** a tiny test that every directory named in CLAUDE's package table exists and is non-empty of `*.go`.

### ENT-010: A second goldmark pipeline lives in `adf` with no product wiring

- **Priority:** P3
- **Dimensions:** Change amplification; Architecture topology
- **Status:** observed fact (isolation); inference (future drift)
- **Evidence:**
  - `adf/convert.go:33-36` — `goldmark.New(GFM, AutoHeadingID)` only; math rewritten to latex fences (`adf/math.go`) before parse.
  - `convert/convert.go:49-70` — GFM + Footnote + DefinitionList + Typographer + meta + chroma + `html.WithUnsafe()`.
  - `go list` : `adf` imports nothing in-repo and is imported by nobody in-repo. Tests: `go test ./adf` 13 tests, green this run.
  - Not in STABILITY, README, agents-guide, or MCP `to.format`.
- **Mechanism:** GFM table/list/heading fixes in `convert` do not apply to Confluence ADF. When 🎯T27 or a convert sink lands, two parsers will disagree unless they share an AST walk or a documented subset.
- **Blast radius:** Confluence publish (T27); any future `to.format=adf`.
- **Counterevidence checked:** T26 acceptance is “package adf … no token required” — isolation is the target. Residue (math as latex codeBlock, no footnotes) is declared in `adf/doc.go:12-26`. This is not a premature-unification finding; it is a recorded fork to watch.
- **Smallest coherent remediation:** when ADF becomes a `Run` sink, parse once (goldmark AST) and fan out HTML vs ADF; until then, leave isolated and list `adf` in STABILITY as library-only.
- **Verification:** a shared fixture of GFM tables/headings asserted in both `convert` HTML and `adf` JSON.
- **Ratchet candidate:** later, an architecture test that `convert.Run` is the only way to request ADF.

### ENT-011: MCP schema types duplicate `convert` request types

- **Priority:** P3
- **Dimensions:** Redundancy; Change amplification
- **Status:** observed fact
- **Evidence:** `mcp/server.go:19-32` `Endpoint` / `FilePair` vs `convert/run.go:52-65`. Mapping: `mcp/server.go:159-170` `toEndpoint`.
- **Mechanism:** a new media field must be added twice or MCP silently drops it (`toEndpoint` is a manual field list).
- **Blast radius:** MCP `convert` schema vs `convert.Run`.
- **Counterevidence checked:** jsonschema tags cannot live on `convert.Endpoint` without pulling MCP concerns into the library — a real reason to duplicate. The mapping is small and tested (`mcp/server_test.go`).
- **Smallest coherent remediation:** keep the DTO; generate `toEndpoint` from shared field names, or embed `convert.Endpoint` if the SDK allows jsonschema on the inner type.
- **Verification:** adding a field to `convert.Endpoint` fails a reflection test unless `mcp.Endpoint` and `toEndpoint` match.
- **Ratchet candidate:** the existing MCP reflection test in `mcp/server_test.go` extended to struct-field parity.

### ENT-012: Dependency hygiene has no scanner and pins a 2020 yaml.v3

- **Priority:** P3
- **Dimensions:** Security / dependencies
- **Status:** observed fact (pin, no scanner); needs verification (whether the pin is vulnerable)
- **Evidence:**
  - `go.mod:11` `gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c` (direct, used by `config/config.go:18`).
  - `go.mod:22` `gopkg.in/yaml.v2 v2.3.0` indirect via `goldmark-meta`.
  - `.github/workflows/ci.yml` / `release.yml`: no `govulncheck`, no secret scan. `govulncheck` not on PATH this run. `staticcheck` cannot compile the module.
- **Mechanism:** known yaml.v3 issues after 3.0.0 (if any apply) will not fail CI. Two YAML stacks. Indirect `golang.org/x/oauth2` / `jwt` come from the MCP SDK and are unused by vellum itself.
- **Blast radius:** config parse (untrusted config file is local); supply chain of `go.mod`.
- **Counterevidence checked:** config is owner-local YAML, not a network parser. No secrets in-tree (`.gitignore` has `.claude/`; LICENSE Apache-2.0).
- **Smallest coherent remediation:** `go get gopkg.in/yaml.v3@v3.0.6` (or current) and add `govulncheck ./...` to `cvfile` if the fleet standard says so.
- **Verification:** `govulncheck ./...` exit 0 on the module; yaml.v3 version date is post-2022.
- **Ratchet candidate:** hygiene `scanner` / `command: govulncheck ./...` once adopted.

## Redundancy and competing-source-of-truth inventory

| Fact | Authorities | Drift observed |
|---|---|---|
| What “green” means | `cvfile` `!gate`, `Makefile` `bullseye`, `ci.yml`, `release.yml` `test` | ENT-001, ENT-002 |
| Product architecture | `CLAUDE.md`, `README.md`, `docs/HANDOVER.md`, `STABILITY.md` | ENT-003, ENT-007, ENT-009 |
| Markdown→PDF write | `convert.Convert`, `convert.writeFileOutput` | ENT-004 |
| Markdown parse | `convert` goldmark, `adf` goldmark | ENT-010 |
| Convert request shape | `convert.Endpoint`, `mcp.Endpoint` | ENT-011 |
| YAML | yaml.v3 (config), yaml.v2 (goldmark-meta) | ENT-012 |
| Format constants | `convert.Format*`, `clipboard.Format*`, `viewer.Format*` (iota) | same strings, different types; not currently drifted |
| Pandoc invocation | `importer` (import), `internal/pandoc` (HTML→RTF) | **deliberate**; package comment in `internal/pandoc/pandoc.go:4-13` |
| `docs/embed.go` vs `embed/embed.go` | two `go:embed` packages | **deliberate** (agent guide vs CSS/template); confirmed in 🎯T21 attestation |
| Followable work | `bullseye.yaml` only (`CLAUDE.md:60-62`) | holds; no `docs/TODO.md` |

## Healthy structure worth retaining

- **Acyclic package DAG** with `convert` as the only hub (`go list` graph, 0 cycles).
- **`convert.Run` media router** with clipboard/file_reference seams (`convert/run.go:20-29`) that made the 16-pair matrix testable (🎯T20).
- **`convert.Backend` interface** (`convert/backend.go:16-28`) isolating WeasyPrint vs Prince.
- **testdeps ratchet** (`internal/testdeps/testdeps.go:19-38`) plus skip-census locked at 0 non-pasteboard skips (`cvfile:50-64`). Pasteboard skips are named, not silent.
- **Untainted import corpus** (`convert/testdata/corpus/README.md`; `TestCorpus_ProvenanceIsUntainted`) — fixtures not produced by pandoc/WeasyPrint/vellum.
- **MCP advertisement oracle** (`mcp/server_test.go`) tying wire text to registered tools (🎯T24).
- **T23 fallback already in the tree:** AppKit then pandoc (`clipboard/clipboard_darwin.go:266-278`), sandbox reproduction (`clipboard/clipboard_sandbox_darwin_test.go:16-35,121+`), hermetic pandoc unit test (`clipboard/pandoc_test.go:14-42`). Target 🎯T23 remains identified; do not treat the original NSAttributedString hard-fail as current HEAD behaviour.
- **Darwin build tags** for clipboard and viewer; Linux release binaries set `CGO_ENABLED=0`.
- **`cvfile` as the local gate** with CI as a caller (`ci.yml:8-11`) — the right shape, once ENT-001/002 stop running a second gate.
- **SoftError** so Mermaid/clipboard degradation still yields a document and a non-zero/IsError signal.

## Hygiene posture

**Hygiene posture not declared.** There is no `hygiene.yaml` at the repo root.

Validator invocation (mandatory explicit run):

```
/Users/marcelo/.claude/skills/hygiene/hygiene_check.py
```

Exit 1:

```
FileNotFoundError: [Errno 2] No such file or directory:
'/Users/marcelo/work/github.com/marcelocantos/vellum/hygiene.yaml'
```

Not initialized (brief forbids creating it during this audit).

Overlap with entropy: ENT-001/002/012 are the steady-state controls hygiene would declare (gate command, release job, vuln scan). Entropy explains the competing-green mechanism; hygiene would only be able to say “undeclared” today.

When onboarding hygiene, ground `enforced` items in what actually exists: `cvfile` `!gate`, `.github/workflows/ci.yml#gate`, `LICENSE`, `README.md`, `STABILITY.md`, skip-census, testdeps. Park `govulncheck`, SBOM, and secret-scan as `planned`/`skipped` with reasons. Do not set floors above what CI actually runs.

## Oracle coverage and residue

| Property | Decides today | Gap |
|---|---|---|
| gofmt / vet | `cv fmt` / `cv vet` (green this run) | |
| Package tests with converters | `VELLUM_REQUIRE_DEPS=1 go test` without `-race` (green this run on adf, mcp, config, importer, cmd, convert, viewer, clipboard) | full `cv gate` `-race` + skip-census **not run this session** |
| Skip census = 0 non-pasteboard | `cvfile !skip-census` | not executed here |
| CLI `--version`/`--help`/`--help-agent` | manual this run; `cvfile !cli-contract` | `--version` is `0.13.0` at HEAD (9 unreleased commits) |
| Import correctness vs independent producers | corpus + manifests | legacy `.doc`; real Word/Google Docs (declared in corpus README) |
| Mermaid render via real `mmdc` | routing tests with fake `renderMermaidFn` | real Chromium/`mmdc` (declared T10/T17 residue) |
| PDF visual fidelity | existence / extractable text | layout not oracled |
| MCP tool names vs wire | `mcp/server_test.go` in-memory transport | live stdio probe of this HEAD binary not repeated (T24 did v0.13.0 brew binary) |
| Clipboard rich-text without TextKit | sandbox test + pandoc unit test | owner Aperture path; 🎯T23 still identified; dirty pasteboard lock uncommitted |
| File RTF sink | none (refused) | 🎯T25 |
| ADF conversion | `go test ./adf` | not a product sink; 🎯T27 |
| Vulnerability / licence scan | none | ENT-012 |
| Architecture rules (Convert vs Run, HANDOVER current, Makefile==cv) | none | ENT-001–004 |
| Hygiene floors | undeclared | |

**Owner residue (intent, not mechanical leftover):**

- Is `docs/HANDOVER.md` still a document anyone should open, or may it be archived?
- Should `make bullseye` remain as a name for non-cv users, or is `cv bullseye` the only hook?
- When should `adf` become a `convert.Run` sink versus staying a library for a future Confluence publisher?
- Is KaTeX CSS allowed to stay a CDN link for HTML-in-browser, if PDF embeds it?

Do not hand back “run `cv gate`” as owner work: it is mechanical and was skipped here for time (`-race` twice). A follow-up audit should run it on a clean tree.

## Remediation sequence

1. **Converge the green definition (ENT-001, ENT-002).** Point `Makefile` `bullseye` at `cv bullseye`. Make `release.yml` `test` the macOS `cv gate` (or depend on `ci.yml`). This is the enforcement seam everything else hangs from.
2. **Retire competing architecture prose (ENT-003, ENT-007, ENT-009).** Historical banner or delete HANDOVER; restamp STABILITY against `const version` and `clipboard.Write`; fix the CLAUDE package table. Cheap, stops wrong-product edits.
3. **One PDF writer (ENT-004)** and **CheckDeps on the shipped CLI PDF path (ENT-005)** — small, local, after the gate tells the truth.
4. **Embed KaTeX CSS (ENT-006)** once there is a test that assembled HTML has no remote stylesheets.
5. **Do not duplicate 🎯T23 / 🎯T25.** Clipboard fallback is in HEAD; finish T23's remaining acceptance and then T25 using `internal/pandoc`. Dirty `clipboard/clipboard_darwin_test.go` (pasteboard lock) is user-owned — leave it.
6. **Keep `adf` isolated (ENT-010)** until a convert sink exists; then parse once.
7. **Declare hygiene** from `cvfile` + `ci.yml` (do not invent scanners). Add ENT-001/002/006/007 as ratchet items only after the code matches.
8. **Re-run this audit** on the same finding IDs after the gate recipes coincide.

No architectural rewrite is required. The topology is already the one the docs (except HANDOVER) describe.
