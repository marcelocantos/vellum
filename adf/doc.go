// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package adf converts Markdown to Atlassian Document Format (ADF) for
// Confluence-oriented sinks (Rovo MCP contentFormat=adf, or direct REST).
//
// The conversion is pure: no network, no Confluence credentials, and no
// mmdc rendering. Mermaid fences become extension nodes that preserve
// source for a Confluence Mermaid macro; media edges follow an explicit
// ImagePolicy (never silent link demotion).
//
// # Declared residue
//
// The following Markdown / vellum specials are intentionally not given a
// full Confluence-native mapping yet and are handled as documented so they
// are not silently wrong:
//
//   - KaTeX / LaTeX math ($…$, $$…$$, and vellum's math extractors): emitted
//     as a codeBlock with language "latex" (or "math") so the source is
//     preserved and visible, not interpreted as prose or dropped.
//   - Inline raw HTML other than trivial line breaks: ignored or reduced to
//     text where goldmark surfaces it; not mapped to storage macros.
//   - Footnotes and definition lists: not specially mapped (GFM core path
//     only for T26); content may appear as ordinary blocks if goldmark
//     flattens them.
//
// Callers that need equation macros or HTML passthrough should extend
// ConvertArgs and the walker deliberately rather than relying on silent
// defaults.
package adf
