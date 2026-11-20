// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/marcelocantos/vellum/internal/pandoc"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

func TestRun_RTFWithTextKitDenied(t *testing.T) {
	testdeps.Need(t, pandoc.Binary)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Match the T23 clipboard negative control. The CLI round-trip must
	// still work when the AppKit HTML importer cannot look up its agent.
	const profile = `(version 1) (allow default)
		(deny mach-lookup (global-name "com.apple.textkit.nsattributedstringagent"))`
	out, err := exec.Command("/usr/bin/sandbox-exec", "-p", profile,
		self, "-test.run=^TestRun_RTFByOutputExtension$", "-test.v").CombinedOutput()
	if err != nil || !bytes.Contains(out, []byte("--- PASS: TestRun_RTFByOutputExtension ")) {
		t.Fatalf("RTF round-trip with TextKit denied: %v\n%s", err, out)
	}
}
