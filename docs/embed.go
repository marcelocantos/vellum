// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package docs embeds user-facing documentation that vellum serves
// directly from the binary (e.g. the agent guide printed by
// --help-agent).
package docs

import _ "embed"

//go:embed agents-guide.md
var AgentGuide string
