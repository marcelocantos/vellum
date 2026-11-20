// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package clipboard

import (
	"context"

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
	// Sandboxed: the pasteboard carries bytes, not a document directory,
	// so only images already in the HTML as data: URLs travel with it.
	// pandoc reads no path and fetches no URL the document names, so a
	// document cannot copy a file from outside the viewer's folders onto
	// the clipboard, nor make a request to the viewer that it would take
	// for the user (🎯T66). AppKit's importer fetches nothing either.
	rtf, _, err = pandoc.HTMLToRTFSandboxed(context.Background(), html)
	if err != nil {
		return nil, nil, err
	}
	plain, err = pandoc.HTMLToPlain(html)
	if err != nil {
		return nil, nil, err
	}
	return rtf, plain, nil
}
