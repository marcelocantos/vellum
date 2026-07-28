// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package viewer

import "fmt"

// BundleID is defined for API symmetry; install is macOS-only.
const BundleID = "com.marcelocantos.vellum-viewer"

// AppName is defined for API symmetry; install is macOS-only.
const AppName = "Vellum Viewer.app"

// InstallOptions is a stub on non-macOS platforms.
type InstallOptions struct {
	ApplicationsDir    string
	VellumPath         string
	SkipDefaultHandler bool
}

// InstallViewer returns an unsupported error on non-macOS platforms.
func InstallViewer(opts *InstallOptions) (string, error) {
	return "", fmt.Errorf("viewer: install-viewer is only supported on macOS")
}

// UninstallViewer returns an unsupported error on non-macOS platforms.
func UninstallViewer(opts *InstallOptions) error {
	return fmt.Errorf("viewer: uninstall-viewer is only supported on macOS")
}

// AppBundlePath returns an unsupported error on non-macOS platforms.
func AppBundlePath(applicationsDir string) (string, error) {
	return "", fmt.Errorf("viewer: install-viewer is only supported on macOS")
}
