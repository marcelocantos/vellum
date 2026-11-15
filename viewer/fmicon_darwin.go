// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"os"
	"os/exec"
	"path/filepath"
)

// finderIcon is Finder's application icon as macOS ships it.
const finderIcon = "/System/Library/CoreServices/Finder.app/Contents/Resources/Finder.icns"

// fileManagerIconPixels is the PNG's edge: four times the 16px it is
// shown at, so it stays sharp on high-density displays.
const fileManagerIconPixels = "64"

// platformFileManagerIcon converts the system's own Finder icon with
// sips, so vellum shows Finder's icon without shipping a copy of it.
func platformFileManagerIcon() ([]byte, bool) {
	dir, err := os.MkdirTemp("", "vellum-icon-*")
	if err != nil {
		return nil, false
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "finder.png")
	cmd := exec.Command("/usr/bin/sips", "-s", "format", "png", "-Z", fileManagerIconPixels, finderIcon, "--out", out)
	if err := cmd.Run(); err != nil {
		return nil, false
	}
	data, err := os.ReadFile(out)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}
