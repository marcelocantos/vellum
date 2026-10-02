// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package xdg locates vellum's per-user configuration directory.
//
// vellum follows the XDG base-directory convention on every platform
// rather than os.UserConfigDir, which on macOS points at
// ~/Library/Application Support — a place nobody expects to find a
// hand-edited config.yaml.
package xdg

import (
	"os"
	"path/filepath"
)

// appDir is the subdirectory vellum owns under the config root.
const appDir = "vellum"

// ConfigDir returns $XDG_CONFIG_HOME/vellum when XDG_CONFIG_HOME is
// set, otherwise $HOME/.config/vellum. It does not create the directory.
func ConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appDir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", appDir), nil
}
