# Audit Log

Chronological record of audits, releases, documentation passes, and other
maintenance activities. Append-only — newest entries at the bottom.

## 2026-04-11 — /release v0.1.0

- **Commit**: `103795e`
- **Outcome**: First public release of vellum. Cut v0.1.0 with stdio MCP server (`convert` tool), CLI, goldmark-based pipeline, KaTeX math, Mermaid diagrams, `vellum:scale` hint, runtime dependency checks, and `--help-agent`. Ships via GitHub Releases and Homebrew tap `marcelocantos/tap/vellum` for darwin-arm64, linux-amd64, and linux-arm64. STABILITY.md and THIRD_PARTY_NOTICES.md added as the 1.0 baseline. Release PR bundles the accumulated pre-release work (13 commits) with the release-prep additions, followed by a second PR to rewrite this `pending` hash to the real merge-commit SHA.
- **Deferred**:
  - Windows build matrix (STABILITY.md gap — decide in-scope or out-of-scope for 1.0)
  - Bundled Chromium for `mmdc` (STABILITY.md out-of-scope — too much footprint)
  - `vellum doctor` one-shot dependency installer (STABILITY.md gap)

## 2026-05-17 — /release v0.5.0

- **Commit**: `pending` (rewritten to the squash-merge SHA in a follow-up bundled with the next release, per the established pattern).
- **Outcome**: Released v0.5.0. Adds the inverse direction: rich-text → Markdown (🎯T11). New `vellum import <file>` subcommand handles RTF, DOCX, HTML, ODT, EPUB, LaTeX and anything else pandoc supports out of the box; `vellum import --from-clipboard` ingests the system clipboard's rich-text representation (RTF preferred, HTML fallback) on macOS. Two new MCP tools — `convert_from_clipboard` (the dominant usage per the user's framing) and `import` (file-based, with optional `output` write path) — symmetric to the existing `convert_to_clipboard` and `convert` tools. New `importer` package shells to pandoc; pandoc is documented as a runtime dep and added to the Homebrew formula via `depends_on "pandoc"`. New `clipboard` reads: `ReadRTF`, `ReadHTML`, `ReadRichText`. Dependency check is **lazy** — PDF-only users never get a pandoc error at startup; the check fires only inside `vellum import` and the import MCP tools. STABILITY.md re-baselined to v0.5.0 because of the new surface; pre-1.0 still. Drive-by: rewrites the v0.4.0 audit-log entry's `pending` hash to `a4aa81b`, consolidating the typical post-release pending-hash-rewrite PR into this release's PR per the one-PR-at-a-time policy.
- **Deferred**:
  - Linux/Windows clipboard read (paired with the parked 🎯T7.1 write).
  - 🎯T10 *Mermaid render failures surface loudly* — still open.
  - CI Node.js 20 deprecation (actions/checkout@v4, actions/setup-go@v5) — noted in v0.4.0; still not actioned.

## 2026-05-17 — /release v0.4.0

- **Commit**: `a4aa81b`
- **Outcome**: Released v0.4.0. Replaces Prince with WeasyPrint as the default renderer backend (🎯T9: open-source default + Prince opt-in; corporate-licensing reputational risk drove the flip — Prince remains available via `backend: prince`). Adds a `convert.Backend` interface with two implementations, a `config` package reading `~/.config/vellum/config.yaml`, and a 13-field `convert.Style` customisation surface (`font_size`, `line_height`, `font_family`, `code_font_family`, `page_size`, `page_margin`, `page_first_top_margin`, `page_numbers`, `running_head`, `bookmarks` default on, `hyphenate`, `lang`, `pdfa`). CLI gains `--backend`; MCP `convert` and `convert_to_clipboard` tools gain optional `style` and `backend` per-call overrides. Fixes long-standing relative-path image bug by passing `--baseurl` / `--base-url` to the renderer. Default `font-size` reduced to 14px and `@page margin` to 1cm for more usable horizontal space. Tests parameterized across both backends. Homebrew formula now `depends_on "weasyprint"`; caveats rewritten to position Prince as opt-in. STABILITY.md re-baselined to v0.4.0 because of the new `Style`/`Backend`/`Config` surface; pre-1.0 still. Release-workflow risk: release.yml's homebrew block grew (added weasyprint dep + rewrote caveats) — homebrew-releaser has handled multi-line `install:` and `formula_includes:` before (v0.3.0 introduced both), so direct release without an rc tag should be fine, but watch the homebrew job on first run.
- **Deferred**:
  - 🎯T10 *Mermaid render failures surface loudly* — filed this session; the silent-fallback-to-source-as-code is the recurring symptom of Chromium-version drift in mmdc. Not blocking the release.
  - Chromium auto-resolve for mmdc (potential 🎯T11) — discussed but not filed; the auto-install-on-error path is the right shape once T10 ships.

## 2026-04-28 — /release v0.3.0

- **Commit**: `f40514a`
- **Outcome**: Released v0.3.0. Homebrew formula no longer declares a `service do` block; instead `def install` writes a thin `sh` wrapper (`bin/vellum`) that prepends the canonical tool dirs and execs `bin/vellum-bin` (the renamed binary). Fixes the v0.2.0 launch-context limitation: the service block only set PATH for `brew services`-spawned processes, but Claude Code and other MCP clients launched from a GUI inherit the same minimal PATH and were not covered. Per-binary wrapper covers all launch contexts and removes the `brew services list` entry that tempted users to start vellum as a daemon (which fails because stdio MCP has nothing to talk to under launchd). No public-surface change; STABILITY.md settling clock unchanged. Release-workflow risk: release.yml `install:` parameter is now multi-line (was one-line) — cut as `v0.3.0-rc.1` prerelease first to validate homebrew-releaser handles the heredoc-in-yaml correctly, then real `v0.3.0`. Also incidentally cleans up homebrew-releaser's v0.2.0 duplicate-methods quirk.
- **Deferred**:
  - Standardise NOTICE-file naming across the portfolio (`~/think` 🎯T2). v0.3.0 ships with `THIRD_PARTY_NOTICES.md` unchanged; rename will land in a future release once the convention target lands portfolio-wide.

## 2026-04-27 — /release v0.2.0

- **Commit**: `6bd4e85`
- **Outcome**: Released v0.2.0. Adds clipboard delivery (🎯T7: `--to-clipboard` CLI flag + `convert_to_clipboard` MCP tool, macOS-only, atomic RTF + HTML + plain text NSPasteboard write) and fixes the Homebrew formula's launchd PATH (🎯T8: `depends_on node` + `mermaid-cli`, `service` block pins PATH so node/mmdc/prince resolve under `brew services`). New public Go API: `convert.Render`, `convert.RenderFile`, and the `clipboard` package. STABILITY.md settling clock re-baselined to 2026-04-27 because of the new surface; pre-1.0 still. Release-workflow risk signal: this release modifies `release.yml` itself (adds `formula_includes`), so cut as `v0.2.0-rc.1` prerelease first to validate homebrew-releaser end-to-end before the real tag. Bundled PR carries T8 + release-prep (squash collapses atomic history on master; PR record preserves it).
- **Deferred**:
  - Windows build matrix (still STABILITY.md gap)
  - Bundled Chromium for `mmdc` (still STABILITY.md out-of-scope)
  - `vellum doctor` (still STABILITY.md gap)
  - Linux/Windows clipboard support (🎯T7.1 parked)
