// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package clipboard

import (
	"github.com/marcelocantos/vellum/internal/pandoc"
)

// pandocRichText converts a full HTML document to the RTF and plain-text
// representations the pasteboard needs.
//
// This is the fallback for [RouteAppKit]. pandoc is already a vellum
// runtime dependency (rich-text import goes through it), so the fallback
// adds no new install burden, and unlike AppKit's HTML importer it needs
// no window server and no XPC service — see [pandoc] (🎯T23).
func pandocRichText(html string) (rtf, plain []byte, err error) {
	// No resource path: the pasteboard carries bytes, not a document
	// directory, so relative images cannot travel with it either way.
	rtf, err = pandoc.HTMLToRTF(html, "")
	if err != nil {
		return nil, nil, err
	}
	plain, err = pandoc.HTMLToPlain(html)
	if err != nil {
		return nil, nil, err
	}
	return rtf, plain, nil
}
