// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package embed

import _ "embed"

//go:embed github.css
var GitHubCSS string

//go:embed template.html
var HTMLTemplate string
