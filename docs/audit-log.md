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
