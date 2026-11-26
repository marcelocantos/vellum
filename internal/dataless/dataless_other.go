// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package dataless

// Enable does nothing: only macOS has dataless files with this policy.
// Windows cloud files download on read without help.
func Enable() error { return nil }
