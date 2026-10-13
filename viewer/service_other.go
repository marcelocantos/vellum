// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package viewer

import (
	"fmt"
	"runtime"
)

func serviceUnsupported() error {
	if runtime.GOOS == "darwin" {
		return fmt.Errorf("install-service is the Windows Scheduled Task; on macOS run the view server with: brew services start vellum")
	}
	return fmt.Errorf("install-service is not supported on %s; run: vellum serve-view", runtime.GOOS)
}

// InstallService registers the view server as a per-user logon service.
// Windows only; macOS uses brew services.
func InstallService(*ServiceOptions) (string, error) { return "", serviceUnsupported() }

// UninstallService removes the logon service. Windows only.
func UninstallService() error { return serviceUnsupported() }

// ServiceStatus reports the logon service state. Windows only.
func ServiceStatus() (string, error) { return "", serviceUnsupported() }
