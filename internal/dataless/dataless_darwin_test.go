// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package dataless

import "testing"

// A dataless file cannot be made on demand in a test (no File Provider
// domain is guaranteed, and eviction needs one), so this checks the policy
// the kernel reports back.
func TestEnable_SetsTheProcessPolicy(t *testing.T) {
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	if !enabled() {
		t.Fatal("policy after Enable is not IOPOL_MATERIALIZE_DATALESS_FILES_ON")
	}
}
