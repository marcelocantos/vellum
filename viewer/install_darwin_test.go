// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package viewer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallViewer_BundleShape(t *testing.T) {
	dir := t.TempDir()
	// Fake vellum binary so the launcher has a real path to bake in.
	fakeVellum := filepath.Join(dir, "vellum")
	if err := os.WriteFile(fakeVellum, []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	appPath, err := InstallViewer(&InstallOptions{
		ApplicationsDir:    dir,
		VellumPath:         fakeVellum,
		SkipDefaultHandler: true, // don't touch system defaults in tests
	})
	if err != nil {
		t.Fatalf("InstallViewer: %v", err)
	}
	if !strings.HasSuffix(appPath, AppName) {
		t.Errorf("unexpected app path: %s", appPath)
	}

	plist := filepath.Join(appPath, "Contents", "Info.plist")
	body, err := os.ReadFile(plist)
	if err != nil {
		t.Fatalf("Info.plist: %v", err)
	}
	s := string(body)
	for _, want := range []string{
		BundleID,
		"net.daringfireball.markdown",
		"public.markdown",
		"<string>md</string>",
		"VellumViewer",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Info.plist missing %q", want)
		}
	}

	launcher := filepath.Join(appPath, "Contents", "MacOS", "VellumViewer")
	script, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatalf("launcher: %v", err)
	}
	if !strings.Contains(string(script), fakeVellum) {
		t.Errorf("launcher does not reference vellum path %s:\n%s", fakeVellum, script)
	}
	if !strings.Contains(string(script), "--open") {
		t.Errorf("launcher missing --open:\n%s", script)
	}
	info, err := os.Stat(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("launcher not executable: mode %v", info.Mode())
	}

	// Uninstall removes the bundle.
	if err := UninstallViewer(&InstallOptions{ApplicationsDir: dir}); err != nil {
		t.Fatalf("UninstallViewer: %v", err)
	}
	if _, err := os.Stat(appPath); !os.IsNotExist(err) {
		t.Errorf("bundle still present after uninstall: %v", err)
	}
}
