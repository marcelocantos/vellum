// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/marcelocantos/vellum/internal/hint"
)

// Dep describes an external runtime dependency.
type Dep struct {
	Name    string // binary name for exec.LookPath
	Purpose string // what it's used for (for error messages)
	Install string // install command or URL
}

// RequiredDeps returns the list of external binaries vellum needs at runtime
// when the given backend is selected. An empty backendName resolves to the
// default backend.
//
// Note: the katex package is loaded by node via require("katex") from an
// embedded script (see convert/katex.go), so there is no standalone katex
// binary to look up. If node is present but katex cannot be resolved, the
// underlying conversion will fail at runtime with a node error. Install
// katex globally with `npm install -g katex`.
func RequiredDeps(backendName string) []Dep {
	deps := []Dep{}
	if backend, err := ResolveBackend(backendName); err == nil {
		deps = append(deps, backend.Dep())
	}
	deps = append(deps,
		Dep{
			Name:    "node",
			Purpose: "runs the KaTeX math renderer (requires global katex package: npm install -g katex)",
			Install: "https://nodejs.org/ (then: npm install -g katex)",
		},
		Dep{
			Name:    "mmdc",
			Purpose: "Mermaid diagram rendering",
			Install: "npm install -g @mermaid-js/mermaid-cli",
		},
	)
	return deps
}

// PandocDep is pandoc as a conversion dependency. It is needed only when
// a requested format goes through pandoc: a rich-text source (RTF, DOCX,
// HTML, ODT, …) or a PowerPoint sink. It is not part of RequiredDeps, so
// a Markdown → PDF or HTML user never has to install it.
var PandocDep = Dep{
	Name:    "pandoc",
	Purpose: "rich-text import (RTF, DOCX, HTML, ODT, …) and PowerPoint output",
	Install: hint.Pandoc(),
}

// CheckDeps returns an error if any required dep for the selected backend is
// missing from PATH. The error message lists every missing dep with its
// install instructions.
func CheckDeps(backendName string) error {
	return checkDeps(RequiredDeps(backendName))
}

func checkDeps(deps []Dep) error {
	var missing []Dep
	seen := map[string]bool{}
	for _, d := range deps {
		if seen[d.Name] {
			continue
		}
		seen[d.Name] = true
		if _, err := exec.LookPath(d.Name); err != nil {
			missing = append(missing, d)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	var b strings.Builder
	if len(missing) == 1 {
		d := missing[0]
		fmt.Fprintf(&b, "required dependency %q not found on PATH (%s).\nInstall from %s",
			d.Name, d.Purpose, d.Install)
		return fmt.Errorf("%s", b.String())
	}

	fmt.Fprintf(&b, "%d required dependencies not found on PATH:", len(missing))
	for _, d := range missing {
		fmt.Fprintf(&b, "\n  - %s (%s)\n    install: %s", d.Name, d.Purpose, d.Install)
	}
	return fmt.Errorf("%s", b.String())
}

// preflight checks, before any converter runs, that the binaries the
// planned conversion needs are on PATH, so a missing tool fails up front
// with one message listing every gap instead of surfacing later as an
// exec error from deep inside a backend (ENT-005, 🎯T30.5).
//
// A PDF sink needs RequiredDeps for the selected backend, the same set
// MCP and the viewer have always checked. pandoc stays lazy: it is
// demanded only when a requested format goes through it (a rich-text
// source or a pptx sink), never for Markdown → PDF/HTML. A clipboard
// source's format is only known once the clipboard is read, so its
// import check stays in the importer.
func preflight(from, to Endpoint, inputs []inputItem, backend string) error {
	var needRenderer, needPandoc bool
	for _, in := range inputs {
		fromFmt := plannedSourceFormat(from, in)
		if fromFmt != "" && !isMarkdownFormat(fromFmt) && fromFmt != FormatPDF {
			needPandoc = true
		}
		if fromFmt == "" {
			fromFmt = FormatRTF // what ingest assumes for an undetected clipboard
		}
		if to.Media != MediaFile && to.Media != MediaFileReference {
			continue // text and clipboard sinks cannot carry PDF or pptx
		}
		switch inferToFormat(to, fromFmt) {
		case FormatPDF:
			needRenderer = true
		case FormatPPTX:
			needPandoc = true
		}
	}
	var deps []Dep
	if needRenderer {
		deps = append(deps, RequiredDeps(backend)...)
	}
	if needPandoc {
		deps = append(deps, PandocDep)
	}
	return checkDeps(deps)
}

// plannedSourceFormat mirrors ingest's source-format resolution without
// reading anything. It returns "" when the format is unknowable before
// the read (an undetected clipboard).
func plannedSourceFormat(from Endpoint, in inputItem) string {
	f := normalizeFormat(from.Format)
	switch from.Media {
	case MediaFile:
		if f == "" {
			f = formatFromExt(in.path)
		}
		if f == "" {
			f = FormatMarkdown
		}
	case MediaContent:
		if f == "" {
			f = FormatMarkdown
		}
	}
	return f
}
