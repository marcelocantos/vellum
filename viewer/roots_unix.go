// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package viewer

import (
	"os"
	"path/filepath"
	"syscall"
)

// stickyFoldersAreShared: on Unix a sticky folder (/tmp, /private/tmp,
// /var/tmp) is one every account and program writes into.
const stickyFoldersAreShared = true

// dataVolumeRoot is the APFS Data volume. A path through it is not a
// symlink, so EvalSymlinks leaves it, and it is neither the same
// directory as / nor a different device from its parent, so the root
// check and isVolumeTop both miss it. Granting it would serve every home.
const dataVolumeRoot = "/System/Volumes/Data"

// isDataVolumeRoot reports whether dir is that volume. Comparison is
// by identity, so a case variant is the same root.
func isDataVolumeRoot(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	root, err := os.Stat(dataVolumeRoot)
	if err != nil {
		return false
	}
	return os.SameFile(info, root)
}

// isVolumeTop reports whether dir is a shared temporary folder (sticky)
// or the top of a mounted volume (its device differs from its parent's).
func isVolumeTop(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSticky != 0 {
		return true
	}
	parent, err := os.Stat(filepath.Dir(dir))
	if err != nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	pst, pok := parent.Sys().(*syscall.Stat_t)
	return ok && pok && st.Dev != pst.Dev
}
