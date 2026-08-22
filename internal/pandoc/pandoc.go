// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package pandoc runs pandoc for the export direction: vellum's rendered
// HTML out to rich-text formats.
//
// The import direction (rich text → Markdown) lives in importer/, which
// needs --extract-media, a media directory and a cache, and shares
// nothing with this beyond the binary's name.
//
// pandoc is an ordinary subprocess with no window server, no Aqua
// session and no XPC service behind it, which is why it can carry the
// rich-text routes in process contexts where AppKit cannot (🎯T23).
package pandoc

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Binary is the executable name, and the string tests pass to
// testdeps.Need.
const Binary = "pandoc"

// Available reports whether pandoc can be run, with an error naming the
// install command when it cannot.
func Available() error {
	if _, err := exec.LookPath(Binary); err != nil {
		return fmt.Errorf("%s not found on PATH (install: brew install %s)", Binary, Binary)
	}
	return nil
}

// HTMLToRTF converts an HTML document to a standalone RTF document.
// resourcePath, when non-empty, is where relative image references
// resolve from; without it pandoc looks in the working directory and
// silently drops images that were written next to the source.
//
// pandoc reads the markup, not the stylesheet, so structural formatting
// (bold, italic, headings, lists) survives and CSS does not.
func HTMLToRTF(html, resourcePath string) ([]byte, error) {
	// --standalone emits the {\rtf … } wrapper; without it pandoc writes
	// a bare fragment that no RTF consumer will accept as a document.
	args := []string{"-f", "html", "-t", "rtf", "--standalone"}
	if resourcePath != "" {
		args = append(args, "--resource-path="+resourcePath)
	}
	return run(html, args...)
}

// HTMLToPlain converts an HTML document to plain text.
func HTMLToPlain(html string) ([]byte, error) {
	return run(html, "-f", "html", "-t", "plain")
}

func run(stdin string, args ...string) ([]byte, error) {
	if err := Available(); err != nil {
		return nil, err
	}
	cmd := exec.Command(Binary, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%s %s: %w: %s", Binary, strings.Join(args, " "), err, msg)
		}
		return nil, fmt.Errorf("%s %s: %w", Binary, strings.Join(args, " "), err)
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("%s %s produced no output", Binary, strings.Join(args, " "))
	}
	return out.Bytes(), nil
}
