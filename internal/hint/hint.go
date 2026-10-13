// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package hint picks per-OS install and service instructions for
// error messages.
package hint

import "runtime"

// Pick returns windows on Windows and darwin everywhere else.
func Pick(darwin, windows string) string {
	if runtime.GOOS == "windows" {
		return windows
	}
	return darwin
}

// Pandoc is the install hint for pandoc.
func Pandoc() string {
	return Pick("brew install pandoc  (or: https://pandoc.org/installing.html)",
		"winget install JohnMacFarlane.Pandoc  (or: https://pandoc.org/installing.html)")
}

// Poppler is the install hint for pdftoppm/pdftotext.
func Poppler() string {
	return Pick("brew install poppler",
		"MSYS2: pacman -S mingw-w64-clang-aarch64-poppler (x64: mingw-w64-ucrt-x86_64-poppler), then add the MSYS2 bin dir to PATH")
}

// WeasyPrint is the install hint for weasyprint.
func WeasyPrint() string {
	return Pick("brew install weasyprint  (or: pipx install weasyprint)",
		"pip install weasyprint, plus Pango from MSYS2 (pacman -S mingw-w64-clang-aarch64-pango) with WEASYPRINT_DLL_DIRECTORIES set to the MSYS2 bin dir")
}

// StartViewer is the instruction for running the view server persistently.
func StartViewer() string {
	return Pick("brew services start vellum",
		"vellum install-service")
}

// FileManager names the platform file manager for UI labels.
func FileManager() string { return Pick("Finder", "Explorer") }
