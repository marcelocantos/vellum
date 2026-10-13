// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package shim

import (
	"os"
	"path/filepath"
	"testing"
)

const npmShim = `@ECHO off
GOTO start
:find_dp0
SET dp0=%~dp0
EXIT /b
:start
SETLOCAL
CALL :find_dp0
endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\node_modules\@mermaid-js\mermaid-cli\src\cli.js" %*
`

func TestNpmShimScript(t *testing.T) {
	dir := t.TempDir()
	cmd := filepath.Join(dir, "mmdc.cmd")
	if err := os.WriteFile(cmd, []byte(npmShim), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := NpmShimScript(cmd); ok {
		t.Fatal("script missing on disk: want ok=false")
	}
	script := filepath.Join(dir, "node_modules", "@mermaid-js", "mermaid-cli", "src", "cli.js")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := NpmShimScript(cmd)
	if !ok || got != script {
		t.Fatalf("NpmShimScript = %q, %v; want %q, true", got, ok, script)
	}
	if _, ok := NpmShimScript(filepath.Join(dir, "mmdc")); ok {
		t.Fatal("non-.cmd path: want ok=false")
	}
}
