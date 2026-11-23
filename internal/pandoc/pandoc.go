// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package pandoc runs pandoc for the export direction: vellum's rendered
// HTML out to rich-text formats, and Markdown out to binary document
// formats such as pptx.
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
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/vellum/internal/hint"
)

// Binary is the executable name, and the string tests pass to
// testdeps.Need.
const Binary = "pandoc"

// Available reports whether pandoc can be run, with an error naming the
// install command when it cannot.
func Available() error {
	if _, err := exec.LookPath(Binary); err != nil {
		return fmt.Errorf("%s not found on PATH (install: %s)", Binary, hint.Pandoc())
	}
	return nil
}

// sandbox stops pandoc reading anything but its input: no file, no
// network. A data: URL is not a read, so a data: image still embeds.
const sandbox = "--sandbox"

// HTMLToRTFSandboxed converts an HTML document to a standalone RTF
// document. pandoc reads no file and fetches no URL, so an image reaches
// the RTF only as a data: URL. An image named by path, file: URL or
// http(s) URL is left out (pandoc warns and writes its description).
//
// pandoc reads the markup, not the stylesheet, so structural formatting
// (bold, italic, headings, lists) survives and CSS does not. Warnings
// report content pandoc dropped while still producing a document.
//
// The clipboard and the CLI and MCP RTF sink use it (the latter inline
// the images a document may show first). Without it pandoc would embed whatever a
// document names, outside every folder the viewer serves, and its
// requests to the viewer itself carry no fetch metadata, which the viewer
// reads as the user opening a document.
func HTMLToRTFSandboxed(ctx context.Context, html string) (rtf []byte, warnings []string, err error) {
	return run(ctx, html, sandbox, "-f", "html", "-t", "rtf", "--standalone")
}

// HTMLToPlain converts an HTML document to plain text. Plain text has no
// images, so pandoc runs sandboxed: it needs nothing but its input.
func HTMLToPlain(html string) ([]byte, error) {
	plain, _, err := run(context.Background(), html, sandbox, "-f", "html", "-t", "plain")
	return plain, err
}

// userDataDirPrefix starts the line of pandoc --version that names its
// user data directory.
const userDataDirPrefix = "User data directory:"

// UserDataFile returns the path of name in pandoc's user data directory
// (as pandoc --version reports it) when that file exists, else "".
// Under --sandbox pandoc no longer looks there itself, so a caller that
// wants pandoc's own default (a user's reference.pptx) passes it.
func UserDataFile(ctx context.Context, name string) (string, error) {
	out, _, err := run(ctx, "", "--version")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, userDataDirPrefix) {
			continue
		}
		dir := strings.TrimSpace(strings.TrimPrefix(line, userDataDirPrefix))
		if dir == "" {
			return "", nil
		}
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return p, nil
		}
		return "", nil
	}
	return "", nil
}

// ToJSON parses src as from and returns pandoc's JSON AST. pandoc runs
// sandboxed: parsing reads nothing but the input.
func ToJSON(ctx context.Context, src, from string) ([]byte, error) {
	out, _, err := run(ctx, src, sandbox, "-f", from, "-t", "json")
	return out, err
}

// ToFile converts src from one format to another and writes the result
// to outPath (pandoc -o). It returns pandoc's [WARNING] lines so callers
// can surface them as soft errors: pandoc still exits 0 when it drops an
// unsupported construct, and silently losing content is the failure
// mode a deck author cannot see.
//
// pandoc runs sandboxed: it reads only the files named on its command
// line (a reference doc), so an image reaches the output only as a
// data: URL. Callers inline what a document may embed first.
//
// extra holds additional pandoc arguments (reference doc, extensions),
// appended verbatim.
func ToFile(ctx context.Context, src, from, to, outPath string, extra ...string) (warnings []string, err error) {
	if err := Available(); err != nil {
		return nil, err
	}
	args := append([]string{sandbox, "-f", from, "-t", to, "-o", outPath}, extra...)
	cmd := exec.CommandContext(ctx, Binary, args...)
	cmd.Stdin = strings.NewReader(src)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%s -t %s: %w: %s", Binary, to, err, msg)
		}
		return nil, fmt.Errorf("%s -t %s: %w", Binary, to, err)
	}
	return warningLines(stderr.String()), nil
}

func warningLines(stderr string) []string {
	var warnings []string
	for _, line := range strings.Split(stderr, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "[WARNING]") {
			warnings = append(warnings, Binary+": "+strings.TrimSpace(strings.TrimPrefix(line, "[WARNING]")))
		}
	}
	return warnings
}

func run(ctx context.Context, stdin string, args ...string) ([]byte, []string, error) {
	if err := Available(); err != nil {
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, Binary, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, nil, fmt.Errorf("%s %s: %w: %s", Binary, strings.Join(args, " "), err, msg)
		}
		return nil, nil, fmt.Errorf("%s %s: %w", Binary, strings.Join(args, " "), err)
	}
	if out.Len() == 0 {
		return nil, nil, fmt.Errorf("%s %s produced no output", Binary, strings.Join(args, " "))
	}
	return out.Bytes(), warningLines(stderr.String()), nil
}
