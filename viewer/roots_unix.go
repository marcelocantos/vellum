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

// realParent is dir's parent as the kernel walks "..", which is one
// directory however dir is spelled. filepath.Dir answers for the
// spelling: macOS also resolves /.nofollow/<path> and
// /.vol/<device>/<inode>, neither a symlink, and filepath.Dir of
// /.nofollow/Volumes is /.nofollow, while /.vol/<device> is not a
// directory at all.
func realParent(dir string) (os.FileInfo, error) {
	return os.Stat(dir + string(filepath.Separator) + "..")
}

// isUnderRoot reports whether dir's parent is / or the Data volume, by
// identity. / is its own parent, so it is included. A folder the system
// volume firmlinks (/System/Volumes/Data/Library) has / as its parent,
// and one only the Data volume has (/System/Volumes/Data/usr) has that
// volume.
func isUnderRoot(dir string) bool {
	parent, err := realParent(dir)
	if err != nil {
		return false
	}
	for _, root := range []string{string(filepath.Separator), dataVolumeRoot} {
		if info, err := os.Stat(root); err == nil && os.SameFile(parent, info) {
			return true
		}
	}
	return false
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
	parent, err := realParent(dir)
	if err != nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	pst, pok := parent.Sys().(*syscall.Stat_t)
	return ok && pok && st.Dev != pst.Dev
}
