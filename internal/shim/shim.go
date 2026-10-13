// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package shim builds exec.Cmds for tools that npm installs as
// launcher shims. On Windows, npm's global bin holds mmdc.cmd, a batch
// wrapper around a Node script. Running batch files through
// CreateProcess routes arguments via cmd.exe quoting rules, so Command
// unwraps the shim and runs `node <script>` directly when it can.
package shim

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// npmShimScriptRe matches the script path in an npm-generated .cmd shim:
//
//	"%_prog%"  "%dp0%\node_modules\@mermaid-js\mermaid-cli\src\cli.js" %*
var npmShimScriptRe = regexp.MustCompile(`"%dp0%\\([^"]+\.(?:js|cjs|mjs))"`)

// Command returns an exec.Cmd running name with args. Off Windows it is
// exec.CommandContext(ctx, name, args...).
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if runtime.GOOS != "windows" {
		return exec.CommandContext(ctx, name, args...)
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return exec.CommandContext(ctx, name, args...)
	}
	if script, ok := NpmShimScript(path); ok {
		node := "node"
		if local := filepath.Join(filepath.Dir(path), "node.exe"); fileExists(local) {
			node = local
		}
		return exec.CommandContext(ctx, node, append([]string{script}, args...)...)
	}
	return exec.CommandContext(ctx, path, args...)
}

// NpmShimScript reports the Node script an npm .cmd/.bat shim at path
// launches, when path is such a shim and the script exists.
func NpmShimScript(path string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".cmd" && ext != ".bat" {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	m := npmShimScriptRe.FindSubmatch(data)
	if m == nil {
		return "", false
	}
	rel := filepath.FromSlash(strings.ReplaceAll(string(m[1]), `\`, "/"))
	script := filepath.Join(filepath.Dir(path), rel)
	if !fileExists(script) {
		return "", false
	}
	return script, true
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
