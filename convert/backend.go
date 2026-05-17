// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"fmt"
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
	// the URL base for resolving relative paths (e.g. <img src="logo.svg">).
	// pdfaProfile, when non-empty, names a PDF/A variant in canonical form
	// (e.g. "PDF/A-3b"); the backend translates to its engine's flag syntax.
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
