// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package dataless lets this process read cloud-only files.
//
// macOS cloud folders (OneDrive, Google Drive, iCloud Drive and Dropbox,
// under ~/Library/CloudStorage) keep files that are not downloaded as
// "dataless" placeholders. A process started by launchd, such as the
// brew-service viewer, gets a policy that refuses to download them: a read
// fails with EDEADLK ("resource deadlock avoided"). Enable switches this
// process to the policy a terminal gets, so a read downloads the file
// first. Child processes (pandoc, WeasyPrint, mmdc) inherit it.
//
// The LaunchAgent key MaterializeDatalessFiles does the same, but Homebrew
// rewrites the plist on every upgrade, so the binary sets it itself.
package dataless
