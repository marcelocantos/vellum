// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package xdg locates vellum's per-user configuration directory.
//
// vellum follows the XDG base-directory convention on every platform
// rather than os.UserConfigDir, which on macOS points at
// ~/Library/Application Support — a place nobody expects to find a
// hand-edited config.yaml. On Windows, where there is no XDG habit,
// the fallback is %APPDATA%\vellum.
package xdg

import (
	"os"
	"path/filepath"
	"runtime"
)

// appDir is the subdirectory vellum owns under the config root.
const appDir = "vellum"

// ConfigDir returns $XDG_CONFIG_HOME/vellum when XDG_CONFIG_HOME is
// set, otherwise %APPDATA%\vellum on Windows and $HOME/.config/vellum
// elsewhere. It does not create the directory.
func ConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appDir), nil
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, appDir), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", appDir), nil
}
