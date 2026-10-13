// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package xdg

import (
	"path/filepath"
	"testing"
)

func TestConfigDirAppData(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", `C:\Users\x\AppData\Roaming`)
	got, err := ConfigDir()
	if err != nil || got != filepath.Join(`C:\Users\x\AppData\Roaming`, "vellum") {
		t.Fatalf("ConfigDir = %q, %v", got, err)
	}
	t.Setenv("XDG_CONFIG_HOME", `D:\cfg`)
	if got, _ := ConfigDir(); got != filepath.Join(`D:\cfg`, "vellum") {
		t.Fatalf("XDG override ignored: %q", got)
	}
}
