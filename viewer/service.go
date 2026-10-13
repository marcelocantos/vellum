// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

// ServiceTaskName is the Windows Scheduled Task that runs the view
// server at logon (see InstallService).
const ServiceTaskName = "Vellum View Server"

// ServiceOptions configures InstallService.
type ServiceOptions struct {
	// VellumPath is the vellum executable the task runs; empty resolves
	// the running binary.
	VellumPath string
	// Addr is passed to serve-view --addr; empty uses the default.
	Addr string
	// NoStart skips starting the task immediately after registering it.
	NoStart bool
}
