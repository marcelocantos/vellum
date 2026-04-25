// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package clipboard places multi-representation rich-text payloads on the
// system clipboard in a single atomic transaction.
//
// The macOS backend uses NSPasteboard via cgo and writes RTF, HTML, and
// plain-text representations together so paste targets (Slack, Mail,
// TextEdit, …) receive content in their preferred format. Linux is out
// of scope; Windows is parked (🎯T7.1). Non-macOS callers receive
// [ErrUnsupported] rather than silent failure.
package clipboard

import "errors"

// ErrUnsupported indicates the current platform has no clipboard backend.
var ErrUnsupported = errors.New("clipboard: platform not supported")

// Payload describes the content to place on the clipboard. HTML is the
// authoritative representation; backends derive RTF and plain text from
// it where the platform allows. HTML must be a complete document or a
// well-formed fragment — NSAttributedString parses CSS embedded in
// <head>, so passing the full assembled page yields the richest paste.
type Payload struct {
	HTML string
}

// Write places p on the system clipboard. On success the implementation
// has confirmed the underlying pasteboard's commit (e.g., NSPasteboard
// changeCount has advanced) before returning, so a subsequent paste in
// another application sees the new content with no race window.
func Write(p Payload) error {
	if p.HTML == "" {
		return errors.New("clipboard: HTML payload is empty")
	}
	return writePayload(p)
}
