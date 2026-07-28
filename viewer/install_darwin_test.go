// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package viewer

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallViewer_BundleShape(t *testing.T) {
	dir := t.TempDir()
	fakeVellum := filepath.Join(dir, "vellum")
	if err := os.WriteFile(fakeVellum, []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	appPath, err := InstallViewer(&InstallOptions{
		ApplicationsDir:    dir,
		VellumPath:         fakeVellum,
		SkipDefaultHandler: true,
	})
	if err != nil {
		t.Fatalf("InstallViewer: %v", err)
	}
	if !strings.HasSuffix(appPath, AppName) {
		t.Errorf("unexpected app path: %s", appPath)
	}

	plist := filepath.Join(appPath, "Contents", "Info.plist")
	body, err := os.ReadFile(plist)
	if err != nil {
		t.Fatalf("Info.plist: %v", err)
	}
	s := string(body)
	for _, want := range []string{
		BundleID,
		"net.daringfireball.markdown",
		"public.markdown",
		"<string>md</string>",
		"VellumViewer",
		"VELLUM_BIN",
		fakeVellum,
		"LSUIElement",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Info.plist missing %q", want)
		}
	}

	launcher := filepath.Join(appPath, "Contents", "MacOS", "VellumViewer")
	info, err := os.Stat(launcher)
	if err != nil {
		t.Fatalf("launcher: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("launcher not executable: mode %v", info.Mode())
	}
	// Must be a Mach-O binary, not a shell script (LS delivers docs via
	// Apple Events which scripts cannot receive).
	head := make([]byte, 4)
	f, err := os.Open(launcher)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Read(head)
	_ = f.Close()
	// MH_MAGIC_64 / CIGAM / FAT
	machO := bytes.Equal(head, []byte{0xcf, 0xfa, 0xed, 0xfe}) ||
		bytes.Equal(head, []byte{0xfe, 0xed, 0xfa, 0xcf}) ||
		bytes.Equal(head, []byte{0xca, 0xfe, 0xba, 0xbe}) ||
		bytes.Equal(head, []byte{0xbe, 0xba, 0xfe, 0xca})
	if !machO {
		t.Errorf("launcher is not Mach-O (got magic %x) — shell scripts break LS document open", head)
	}

	if err := UninstallViewer(&InstallOptions{ApplicationsDir: dir}); err != nil {
		t.Fatalf("UninstallViewer: %v", err)
	}
	if _, err := os.Stat(appPath); !os.IsNotExist(err) {
		t.Errorf("bundle still present after uninstall: %v", err)
	}
}

// TestOpenA_DeliversDocument verifies the real bug class: open -a must
// result in the applet invoking vellum --open with the file path.
// Requires clang (install-viewer) and a stub vellum that logs argv.
func TestOpenA_DeliversDocument(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "vellum-calls.log")
	// Stub vellum: record args, exit 0.
	stub := filepath.Join(dir, "vellum")
	script := "#!/bin/sh\necho \"$@\" >> " + logPath + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(md, []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	appPath, err := InstallViewer(&InstallOptions{
		ApplicationsDir:    dir,
		VellumPath:         stub,
		SkipDefaultHandler: true,
	})
	if err != nil {
		t.Fatalf("InstallViewer: %v", err)
	}

	cmd := exec.Command("open", "-W", "-a", appPath, md)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("open -a: %v\n%s", err, out)
	}

	// open -W waits for app exit; give the stub log a moment on slow hosts.
	deadline := time.Now().Add(5 * time.Second)
	var got []byte
	for time.Now().Before(deadline) {
		got, err = os.ReadFile(logPath)
		if err == nil && strings.Contains(string(got), md) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(string(got), "--open") || !strings.Contains(string(got), md) {
		t.Fatalf("stub vellum was not called with --open and the file path.\nlog=%q\napp log may be at ~/Library/Logs/vellum-viewer.log", got)
	}
}
