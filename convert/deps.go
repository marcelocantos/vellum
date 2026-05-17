// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"fmt"
	"os/exec"
	"strings"
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

// CheckDeps returns an error if any required dep for the selected backend is
// missing from PATH. The error message lists every missing dep with its
// install instructions.
func CheckDeps(backendName string) error {
	var missing []Dep
	for _, d := range RequiredDeps(backendName) {
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
