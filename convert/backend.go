// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Backend is a Markdown→PDF renderer that accepts assembled HTML and writes
// the final PDF. Implementations shell out to a particular engine (prince,
// weasyprint, etc.) and are responsible for any engine-specific argument
// translation (e.g. mapping a canonical PDF/A profile name to the engine's
// flag syntax).
type Backend interface {
	// Name returns the canonical short identifier ("weasyprint", "prince").
	Name() string

	// Render writes a PDF at outputPath from htmlContent. baseDir is used as
	// the URL base for resolving relative links. The engine reads no local
	// file but its input: every local resource the page shows must already
	// be a data: URL (InlineResources). It may fetch http and https.
	// The CLI and MCP PDF path does not use Render; it uses renderFetchless
	// so the engine makes no request. pdfaProfile, when non-empty, names a
	// PDF/A variant in canonical form (e.g. "PDF/A-3b"); the backend
	// translates to its engine's flag syntax.
	Render(ctx context.Context, htmlContent, outputPath, baseDir, pdfaProfile string) error

	// Dep returns the engine's runtime binary dependency for `CheckDeps`.
	Dep() Dep
}

// Backend identifiers. WeasyPrint is the default because it is open-source
// (BSD-3) and has no commercial-license entanglement; Prince is opt-in for
// users who have a Prince license and want its typography upgrades.
const (
	BackendWeasyPrint = "weasyprint"
	BackendPrince     = "prince"
	DefaultBackend    = BackendWeasyPrint
)

// ResolveBackend returns the named backend. An empty name resolves to the
// default backend.
func ResolveBackend(name string) (Backend, error) {
	switch name {
	case "", DefaultBackend:
		return &weasyprintBackend{}, nil
	case BackendPrince:
		return &princeBackend{}, nil
	default:
		return nil, fmt.Errorf("unknown backend %q (supported: %s, %s)",
			name, BackendWeasyPrint, BackendPrince)
	}
}

// fetchlessRenderer is a backend that can run its engine with every
// fetch refused but data: URLs (WriteSelfContainedPDF, writePDF).
// baseDir, when set, is the document folder relative links resolve
// against. It does not let the engine read a file.
type fetchlessRenderer interface {
	renderFetchless(ctx context.Context, htmlContent, outputPath, baseDir, pdfaProfile string) error
}

// runEngine writes htmlContent to a temporary file and runs bin with the
// arguments argv returns for that file's path. stderr passes through
// clean before it joins the error.
func runEngine(ctx context.Context, bin, htmlContent, outputPath string, clean func(string) string, argv func(input string) []string) error {
	if dir := filepath.Dir(outputPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmpFile, err := os.CreateTemp("", "vellum-*.html")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(htmlContent); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, argv(tmpFile.Name())...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := clean(stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}
