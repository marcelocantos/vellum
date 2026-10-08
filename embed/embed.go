// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package embed

import (
	stdembed "embed"
	"io/fs"
	"strings"
)

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

// katexTree holds KaTeX's stylesheet and woff2 fonts, vendored by
// scripts/vendor-katex.sh from the same npm package the math renderer
// loads (convert/katex.go). Shipping them means a math document never
// fetches its CSS from a CDN: the viewer serves this tree, and PDF and
// standalone HTML link a copy of it under the user cache directory.
//
//go:embed katex
var katexTree stdembed.FS

//go:embed katex/VERSION
var katexVersion string

// KaTeXVersion is the KaTeX release the vendored assets came from.
var KaTeXVersion = strings.TrimSpace(katexVersion)

// KaTeXCSSName is the stylesheet's path inside KaTeX; its @font-face
// rules reference KaTeXFontsDir relative to it.
const (
	KaTeXCSSName  = "katex.min.css"
	KaTeXFontsDir = "fonts"
)

// KaTeX returns the vendored KaTeX tree rooted at its stylesheet:
// KaTeXCSSName, KaTeXFontsDir/*.woff2, LICENSE and VERSION.
func KaTeX() fs.FS {
	sub, err := fs.Sub(katexTree, "katex")
	if err != nil {
		panic("embed: katex tree missing: " + err.Error())
	}
	return sub
}
