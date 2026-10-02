// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package embed

import _ "embed"

//go:embed github.css
var GitHubCSS string

//go:embed template.html
var HTMLTemplate string

// ReferencePPTX is the built-in PowerPoint reference deck: a pinned copy
// of pandoc's own default (pandoc --print-default-data-file
// reference.pptx, pandoc 3.11). Pinning it makes pptx output independent
// of whichever pandoc happens to be installed.
//
//go:embed reference.pptx
var ReferencePPTX []byte
