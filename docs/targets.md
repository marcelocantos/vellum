# Targets

## Active

### 🎯T2 Project has a README covering install, usage, and MCP configuration
- **Value**: 8
- **Cost**: 2
- **Acceptance**:
  - README.md exists at repo root
  - Covers: what vellum is, prerequisites (Prince, Node+katex, mmdc+Chromium), install paths (go install, Homebrew when available, binary download), CLI quick start, MCP server config block for Claude Code, feature list with at least one example, licence
  - Links resolve (GitHub URLs use full form)
- **Context**: No README exists yet. This blocks onboarding for anyone discovering the repo — including the user in a fresh session. Use mpe2pdf's README as a starting point but adapt for vellum's Go+MCP positioning.
- **Tags**: docs
- **Origin**: handover-doc-roadmap
- **Status**: Identified
- **Discovered**: 2026-04-11

### 🎯T3 Vellum fails fast with clear errors when runtime dependencies are missing
- **Value**: 5
- **Cost**: 2
- **Acceptance**:
  - Startup (CLI convert or MCP server mode) checks for `prince`, `node`, `katex`, and `mmdc` on PATH
  - Missing dependency produces an actionable error naming the tool and the install command
  - Checks run before any conversion work begins, so failures surface at the top of the pipeline
- **Context**: Currently a missing dependency produces a confusing error mid-pipeline (e.g. deep in Prince shell-out or Node exec). A `vellum doctor`-style preflight — or at minimum `exec.LookPath` calls at startup — gives users a clear onboarding error.
- **Tags**: ux, reliability
- **Origin**: handover-doc-roadmap
- **Status**: Identified
- **Discovered**: 2026-04-11

### 🎯T4 Vellum has a Go test suite covering preprocessors and the full pipeline
- **Value**: 8
- **Cost**: 3
- **Acceptance**:
  - Unit tests for math preprocessor (including code-block protection and multi-line matrices)
  - Unit tests for mermaid preprocessor (including `<!-- vellum:scale N -->` hint)
  - Integration test that runs convert.Convert on a small markdown file and asserts the PDF exists, is non-empty, and contains expected text (via pdftotext or ledongthuc/pdf)
  - CLI tests for --help, --version, and missing-input error paths
  - `go test ./...` passes locally and in CI
- **Context**: No Go tests exist. The preprocessors are pure string transforms and easy to unit-test; the full pipeline needs an integration test to catch regressions in rendering. Required before first release to give CI something meaningful to run.
- **Tags**: tests, quality
- **Origin**: handover-doc-roadmap
- **Status**: Identified
- **Discovered**: 2026-04-11

### 🎯T5 Vellum ships an agent guide accessible via --help-agent
- **Value**: 5
- **Cost**: 2
- **Acceptance**:
  - `docs/agents-guide.md` exists and is embedded via go:embed
  - `vellum --help-agent` prints usage plus the embedded agent guide
  - Guide covers: what vellum does, MCP-preferred invocation, tool schema with example, input restrictions (absolute path, .md), the `<!-- vellum:scale N -->` hint, security notes on Prince JS
- **Context**: All marcelocantos CLI binaries are required to support `--help-agent`. This makes vellum self-describing for AI agents that discover it.
- **Tags**: docs, agent
- **Origin**: global-cli-convention
- **Status**: Identified
- **Discovered**: 2026-04-11

### 🎯T6 Vellum v0.1.0 is released with CI-built binaries and a Homebrew formula
- **Value**: 8
- **Cost**: 3
- **Acceptance**:
  - `.github/workflows/release.yml` builds macOS arm64, Linux x86_64, Linux arm64 tarballs on release events
  - GitHub release v0.1.0 exists with attached binaries
  - `marcelocantos/homebrew-tap` has a working `vellum` formula (via homebrew-releaser)
  - `brew install marcelocantos/tap/vellum` succeeds on a clean machine and `vellum --version` prints v0.1.0
  - STABILITY.md exists cataloguing the CLI+MCP surface
- **Context**: First public release. Depends on README, MCP server, tests, and agent guide being in place so the release is usable. Follow the pattern from mpe2pdf's release workflow; watch the homebrew-releaser gotchas (skip_checksum, explicit version, repo description).
- **Tags**: release, ci
- **Origin**: handover-doc-roadmap
- **Status**: Identified
- **Discovered**: 2026-04-11

## Achieved

### 🎯T1 Vellum exposes a working MCP server over stdio
- **Value**: 13
- **Cost**: 5
- **Acceptance**:
  - `vellum --mcp` starts a stdio MCP server without error
  - Server exposes a `convert` tool whose schema matches mpe2pdf's `{files: [{input, output?}]}`
  - Invoking the tool with a sample markdown file produces a PDF at the specified path
  - Server logs go to stderr only (stdout reserved for JSON-RPC)
  - Manual smoke test via Claude Code `.mcp.json` configuration succeeds
- **Context**: The stated product vision for vellum is an MCP-first document preparation server for AI agents. The CLI already works end-to-end, but `--mcp` is currently stubbed with `return fmt.Errorf("MCP server not yet implemented")` in cmd/vellum/main.go, and the mcp/ directory is empty. Implementation path: use github.com/modelcontextprotocol/go-sdk (official SDK), wire `mcp.NewServer` → `mcp.AddTool` → `server.Run(StdioTransport{})`, and reuse `convert.Convert()`. This is the single highest-value outstanding feature.
- **Tags**: feature, mcp
- **Origin**: handover-doc-roadmap
- **Status**: Achieved
- **Discovered**: 2026-04-11
- **Achieved**: 2026-04-11
- **Actual-cost**: 3

## Graph

```mermaid
graph TD
    T2["Project has a README covering…"]
    T3["Vellum fails fast with clear …"]
    T4["Vellum has a Go test suite co…"]
    T5["Vellum ships an agent guide a…"]
    T6["Vellum v0.1.0 is released wit…"]
```
