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

## 2026-04-27 — /release v0.2.0

- **Commit**: `pending`
- **Outcome**: Released v0.2.0. Adds clipboard delivery (🎯T7: `--to-clipboard` CLI flag + `convert_to_clipboard` MCP tool, macOS-only, atomic RTF + HTML + plain text NSPasteboard write) and fixes the Homebrew formula's launchd PATH (🎯T8: `depends_on node` + `mermaid-cli`, `service` block pins PATH so node/mmdc/prince resolve under `brew services`). New public Go API: `convert.Render`, `convert.RenderFile`, and the `clipboard` package. STABILITY.md settling clock re-baselined to 2026-04-27 because of the new surface; pre-1.0 still. Release-workflow risk signal: this release modifies `release.yml` itself (adds `formula_includes`), so cut as `v0.2.0-rc.1` prerelease first to validate homebrew-releaser end-to-end before the real tag. Bundled PR carries T8 + release-prep (squash collapses atomic history on master; PR record preserves it).
- **Deferred**:
  - Windows build matrix (still STABILITY.md gap)
  - Bundled Chromium for `mmdc` (still STABILITY.md out-of-scope)
  - `vellum doctor` (still STABILITY.md gap)
  - Linux/Windows clipboard support (🎯T7.1 parked)
