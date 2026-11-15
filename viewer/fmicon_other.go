// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package viewer

// platformFileManagerIcon has no system icon file to convert outside
// macOS (Explorer's icon lives in a resource DLL); the folder icon serves.
func platformFileManagerIcon() ([]byte, bool) {
	return nil, false
}
