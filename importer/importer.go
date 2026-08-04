// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package importer converts rich-text and PDF sources to GitHub-Flavoured
// Markdown, extracting images and other media into a directory (import
// cache by default) so Markdown references resolve for agents.
//
// Office-like formats (RTF, DOCX, HTML, ODT, …) go through pandoc with
// --extract-media. PDF uses Poppler (pdftoppm + pdftotext) for page
// images plus extracted text — not pandoc (which cannot read PDF).
package importer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PandocDep is the runtime dependency for office-like imports.
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

// Options configures a single import.
type Options struct {
	// Format is the pandoc -f value or "pdf". Empty lets the file
	// extension decide (for path imports).
	Format string
	// MediaDir is where extracted images are written. Empty uses the
	// managed import cache (keyed by content hash).
	MediaDir string
}

// ImportFile converts a file on disk to Markdown + media.
func ImportFile(ctx context.Context, inputPath string, opts *Options) (Result, error) {
	if opts == nil {
		opts = &Options{}
	}
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return Result{}, err
	}
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = formatFromExt(abs)
	}
	if format == "pdf" {
		key, err := HashFile(abs)
		if err != nil {
			return Result{}, err
		}
		mediaDir, err := EnsureMediaDir(opts.MediaDir, key)
		if err != nil {
			return Result{}, err
		}
		return ImportPDF(ctx, abs, mediaDir)
	}

	if err := CheckDep(); err != nil {
		return Result{}, err
	}
	key, err := HashFile(abs)
	if err != nil {
		return Result{}, err
	}
	mediaDir, err := EnsureMediaDir(opts.MediaDir, key)
	if err != nil {
		return Result{}, err
	}

	args := []string{"-t", "gfm", "--extract-media=" + mediaDir}
	if format != "" {
		args = append(args, "-f", format)
	}
	args = append(args, abs)
	md, err := runPandoc(ctx, args, nil)
	if err != nil {
		return Result{}, err
	}
	md, assets, err := AbsolutizeMedia(md, mediaDir)
	if err != nil {
		return Result{}, err
	}
	return Result{Markdown: md, MediaDir: mediaDir, Assets: assets}, nil
}

// ImportBytes converts in-memory document bytes. Format is required
// (e.g. "rtf", "html", "docx", "pdf").
func ImportBytes(ctx context.Context, data []byte, opts *Options) (Result, error) {
	if opts == nil {
		opts = &Options{}
	}
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		return Result{}, fmt.Errorf("importer: format required when importing bytes")
	}
	if len(data) == 0 {
		return Result{}, fmt.Errorf("importer: empty input")
	}

	key := HashBytes(data)
	mediaDir, err := EnsureMediaDir(opts.MediaDir, key)
	if err != nil {
		return Result{}, err
	}

	if format == "pdf" {
		tmp := filepath.Join(mediaDir, "source.pdf")
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return Result{}, err
		}
		return ImportPDF(ctx, tmp, mediaDir)
	}

	if err := CheckDep(); err != nil {
		return Result{}, err
	}
	// Prefer a temp file so pandoc can use extract-media reliably.
	ext := "." + format
	if format == "markdown" || format == "md" || format == "gfm" {
		ext = ".md"
	}
	tmp := filepath.Join(mediaDir, "source"+ext)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return Result{}, err
	}
	args := []string{"-f", format, "-t", "gfm", "--extract-media=" + mediaDir, tmp}
	md, err := runPandoc(ctx, args, nil)
	if err != nil {
		return Result{}, err
	}
	md, assets, err := AbsolutizeMedia(md, mediaDir)
	if err != nil {
		return Result{}, err
	}
	return Result{Markdown: md, MediaDir: mediaDir, Assets: assets}, nil
}

// Legacy wrappers kept for any external callers expecting (string, error).

// ImportFileMarkdown is like ImportFile but returns only the Markdown string.
func ImportFileMarkdown(ctx context.Context, inputPath, format string) (string, error) {
	r, err := ImportFile(ctx, inputPath, &Options{Format: format})
	if err != nil {
		return "", err
	}
	return r.Markdown, nil
}

// ImportBytesMarkdown is like ImportBytes but returns only the Markdown string.
func ImportBytesMarkdown(ctx context.Context, data []byte, format string) (string, error) {
	r, err := ImportBytes(ctx, data, &Options{Format: format})
	if err != nil {
		return "", err
	}
	return r.Markdown, nil
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

func formatFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return "markdown"
	case ".html", ".htm":
		return "html"
	case ".rtf":
		return "rtf"
	case ".docx":
		return "docx"
	case ".odt":
		return "odt"
	case ".epub":
		return "epub"
	case ".tex", ".latex":
		return "latex"
	case ".pdf":
		return "pdf"
	default:
		return ""
	}
}
