// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// EnsureViewServer probes origin and, if down, starts `vellum serve-view`
// detached so Cmd-click / `vellum view` can open a same-origin URL. Prefer
// `brew services start vellum` for a persistent daemon; this is the fallback
// when the service is not running.
func EnsureViewServer(origin string) error {
	if origin == "" {
		origin = DefaultViewOrigin()
	}
	if err := ProbeViewServer(origin); err == nil {
		return nil
	}
	bin, err := resolveVellumBin()
	if err != nil {
		return fmt.Errorf("view server not running at %s and cannot start one: %w\n"+
			"  Start it with: brew services start vellum\n"+
			"  Or foreground:  vellum serve-view", origin, err)
	}
	addr := addrFromOrigin(origin)
	cmd := exec.Command(bin, "serve-view", "--addr", addr)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting view server: %w\n"+
			"  Start it with: brew services start vellum\n"+
			"  Or foreground:  vellum serve-view", err)
	}
	// Detach: do not wait; child is session leader.
	_ = cmd.Process.Release()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := ProbeViewServer(origin); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("view server at %s did not become ready\n"+
		"  Start it with: brew services start vellum\n"+
		"  Or foreground:  vellum serve-view", origin)
}

func resolveVellumBin() (string, error) {
	if p, err := exec.LookPath("vellum"); err == nil {
		return filepath.Abs(p)
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			return resolved, nil
		}
		return exe, nil
	}
	return "", fmt.Errorf("vellum binary not found on PATH")
}

func addrFromOrigin(origin string) string {
	o := origin
	for _, p := range []string{"https://", "http://"} {
		if strings.HasPrefix(o, p) {
			o = strings.TrimPrefix(o, p)
			break
		}
	}
	return strings.TrimRight(o, "/")
}
