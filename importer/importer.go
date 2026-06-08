// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package importer converts rich-text formats (RTF, DOCX, HTML, ODT, EPUB,
// LaTeX, …) back to GitHub-Flavoured Markdown by shelling out to pandoc.
//
// Pandoc is the only mature engine that handles RTF → GFM cleanly, and
// using it opens the door to all other formats it supports for free. The
// caller does not need to enumerate supported formats — pandoc auto-detects
// from file extension when [ImportFile] is called without an explicit
// format, or accepts an explicit format hint via [ImportBytes].
package importer

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// PandocDep is the runtime dependency this package requires.
//
// vellum's main dependency check does NOT include pandoc — only `vellum
// import` (and the MCP import tools) does. PDF-only users never need
// pandoc installed.
var PandocDep = struct {
	Name    string
	Purpose string
	Install string
}{
	Name:    "pandoc",
	Purpose: "rich-text → Markdown conversion (RTF, DOCX, HTML, ODT, EPUB, LaTeX, …)",
	Install: "brew install pandoc  (or: https://pandoc.org/installing.html)",
}

// CheckDep returns an error if pandoc is not on PATH.
func CheckDep() error {
	if _, err := exec.LookPath(PandocDep.Name); err != nil {
		return fmt.Errorf("required dependency %q not found on PATH (%s).\nInstall: %s",
			PandocDep.Name, PandocDep.Purpose, PandocDep.Install)
	}
	return nil
}

// ImportFile reads inputPath, runs it through pandoc, and returns
// GFM Markdown. Empty format lets pandoc auto-detect from the file
// extension; a non-empty format (e.g. "rtf", "docx", "html") forces it.
func ImportFile(ctx context.Context, inputPath, format string) (string, error) {
	args := []string{"-t", "gfm"}
	if format != "" {
		args = append(args, "-f", format)
	}
	args = append(args, inputPath)
	return runPandoc(ctx, args, nil)
}

// ImportBytes runs raw bytes through pandoc, returning GFM Markdown. The
// format string is required because pandoc can't detect format from a
// stdin stream — e.g. "rtf", "html", "docx". Common values match pandoc's
// `-f` / `--from` argument.
func ImportBytes(ctx context.Context, data []byte, format string) (string, error) {
	if format == "" {
		return "", fmt.Errorf("importer: format required when importing bytes")
	}
	if len(data) == 0 {
		return "", fmt.Errorf("importer: empty input")
	}
	return runPandoc(ctx, []string{"-f", format, "-t", "gfm"}, data)
}

func runPandoc(ctx context.Context, args []string, stdin []byte) (string, error) {
	cmd := exec.CommandContext(ctx, "pandoc", args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("pandoc: %w: %s", err, msg)
		}
		return "", fmt.Errorf("pandoc: %w", err)
	}
	return out.String(), nil
}
