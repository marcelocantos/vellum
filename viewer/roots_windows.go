// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

// stickyFoldersAreShared: Windows has no sticky bit.
const stickyFoldersAreShared = false

// isVolumeTop: a Windows volume's top is a drive root, which rootFor
// already treats as a filesystem root. Volumes mounted on folders are
// not detected.
func isVolumeTop(dir string) bool { return false }

// isDataVolumeRoot: there is no APFS Data volume on Windows.
func isDataVolumeRoot(string) bool { return false }
