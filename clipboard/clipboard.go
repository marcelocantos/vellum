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

import (
	"errors"
	"regexp"
	"strings"
)

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

// ReadRTF returns the RTF representation from the system clipboard, or nil
// if no RTF data is present. Returns [ErrUnsupported] on non-macOS.
func ReadRTF() ([]byte, error) { return readClipboard(FormatRTF) }

// ReadHTML returns the HTML representation from the system clipboard, or
// nil if no HTML data is present. Returns [ErrUnsupported] on non-macOS.
func ReadHTML() ([]byte, error) { return readClipboard(FormatHTML) }

// ReadRichText returns the richest rich-text representation available on
// the clipboard. It tries RTF first (because it preserves more formatting
// than HTML in typical macOS apps) and falls back to HTML.
//
// On success, format is one of [FormatRTF] or [FormatHTML]. If neither is
// present, returns (nil, "", nil). On non-macOS, returns [ErrUnsupported].
func ReadRichText() (data []byte, format string, err error) {
	for _, f := range []string{FormatRTF, FormatHTML} {
		b, err := readClipboard(f)
		if err != nil {
			return nil, "", err
		}
		if len(b) > 0 {
			return b, f, nil
		}
	}
	return nil, "", nil
}

// Format identifiers for the clipboard read helpers.
const (
	FormatRTF  = "rtf"
	FormatHTML = "html"
)

// FileRefPayload lists absolute file paths to place on the pasteboard as
// Finder-compatible file references (public.file-url / NSFilenamesPboardType).
type FileRefPayload struct {
	Paths []string
}

// WriteFileRefs places file URLs on the system clipboard so a paste in
// Finder (or other file-aware targets) copies/moves the referenced files.
// On success the pasteboard commit has completed. Non-macOS returns
// [ErrUnsupported].
func WriteFileRefs(p FileRefPayload) error {
	if len(p.Paths) == 0 {
		return errors.New("clipboard: no file paths for file reference write")
	}
	return writeFileRefs(p.Paths)
}

// ReadFileRefs returns absolute paths from Finder-style file references
// on the system clipboard. Returns (nil, nil) when no file references
// are present. Non-macOS returns [ErrUnsupported].
func ReadFileRefs() ([]string, error) {
	return readFileRefs()
}

var bodyRe = regexp.MustCompile(`(?is)<body[^>]*>(.*)</body>`)

// htmlBodyFragment extracts the inner content of <body>…</body> from a
// full HTML document. Slack and other rich-paste targets expect a body
// fragment, not a complete document with <head><style> blocks (which
// they reject outright). Returns the input unchanged when no <body>
// tag is found, on the assumption that the caller already supplied a
// fragment.
func htmlBodyFragment(html string) string {
	m := bodyRe.FindStringSubmatch(html)
	if m == nil {
		return html
	}
	return strings.TrimSpace(m[1])
}
