// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package testdeps gates tests that need external converters on PATH.
//
// Locally a missing tool skips, so contributors without the full
// toolchain can still run the suite. In CI the tools are installed and
// VELLUM_REQUIRE_DEPS=1 turns every such skip into a failure — without
// that ratchet the suite silently stops exercising the import and PDF
// pipelines, which is exactly the state it was in before 🎯T20.
package testdeps

import (
	"os"
	"os/exec"
	"testing"
)

// RequireEnv is the environment variable that promotes skips to failures.
const RequireEnv = "VELLUM_REQUIRE_DEPS"

// Required reports whether missing tools must fail rather than skip.
func Required() bool { return os.Getenv(RequireEnv) == "1" }

// Need ensures every named binary is on PATH. It skips the test when
// they are missing and VELLUM_REQUIRE_DEPS is unset, and fails when it
// is set.
func Need(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err == nil {
			continue
		}
		if Required() {
			t.Fatalf("%s=1 but %q is not on PATH; CI must install it", RequireEnv, tool)
		}
		t.Skipf("%s not on PATH", tool)
	}
}

// NeedAny ensures at least one of the named binaries is on PATH and
// returns those present. Used where alternatives exist, such as the
// WeasyPrint/Prince rendering backends.
func NeedAny(t *testing.T, tools ...string) []string {
	t.Helper()
	var found []string
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err == nil {
			found = append(found, tool)
		}
	}
	if len(found) == 0 {
		if Required() {
			t.Fatalf("%s=1 but none of %v are on PATH; CI must install one", RequireEnv, tools)
		}
		t.Skipf("none of %v on PATH", tools)
	}
	return found
}
