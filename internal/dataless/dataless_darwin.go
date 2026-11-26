// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package dataless

/*
#include <sys/resource.h>
*/
import "C"

import "fmt"

// Enable sets this process's policy to download dataless files on read.
func Enable() error {
	if r, err := C.setiopolicy_np(C.IOPOL_TYPE_VFS_MATERIALIZE_DATALESS_FILES,
		C.IOPOL_SCOPE_PROCESS, C.IOPOL_MATERIALIZE_DATALESS_FILES_ON); r != 0 {
		return fmt.Errorf("setiopolicy_np: %v", err)
	}
	return nil
}

// enabled reports whether the process policy is set to download.
func enabled() bool {
	return C.getiopolicy_np(C.IOPOL_TYPE_VFS_MATERIALIZE_DATALESS_FILES,
		C.IOPOL_SCOPE_PROCESS) == C.IOPOL_MATERIALIZE_DATALESS_FILES_ON
}
