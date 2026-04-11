// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// captureStdout redirects os.Stdout to a pipe for the duration of fn and
// returns whatever was written. run() uses fmt.Print / fmt.Println which
// write to the package-level os.Stdout, so swapping the file descriptor
// here is the standard way to observe its output without modifying
// production code.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	var buf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&buf, r)
	}()

	defer func() {
		// Restore even on panic.
		os.Stdout = orig
	}()

	fn()

	_ = w.Close()
	wg.Wait()
	_ = r.Close()
	return buf.String()
}

// withArgs temporarily swaps os.Args and restores it after fn returns.
func withArgs(t *testing.T, args []string, fn func()) {
	t.Helper()
	orig := os.Args
	defer func() { os.Args = orig }()
	os.Args = args
	fn()
}

func TestRun_HelpLong(t *testing.T) {
	var err error
	out := captureStdout(t, func() {
		withArgs(t, []string{"vellum", "--help"}, func() {
			err = run()
		})
	})
	if err != nil {
		t.Fatalf("run() returned error: %v", err)
	}
	if !strings.Contains(out, "Usage: vellum") {
		t.Errorf("help output missing usage line:\n%s", out)
	}
	if !strings.Contains(out, "--mcp") {
		t.Errorf("help output missing --mcp description:\n%s", out)
	}
}

func TestRun_HelpShort(t *testing.T) {
	var err error
	out := captureStdout(t, func() {
		withArgs(t, []string{"vellum", "-help"}, func() {
			err = run()
		})
	})
	if err != nil {
		t.Fatalf("run() returned error: %v", err)
	}
	if !strings.Contains(out, "Usage: vellum") {
		t.Errorf("help output missing usage line:\n%s", out)
	}
}

func TestRun_Version(t *testing.T) {
	var err error
	out := captureStdout(t, func() {
		withArgs(t, []string{"vellum", "--version"}, func() {
			err = run()
		})
	})
	if err != nil {
		t.Fatalf("run() returned error: %v", err)
	}
	got := strings.TrimSpace(out)
	if got != version {
		t.Errorf("version output = %q, want %q", got, version)
	}
}

func TestRun_NoArgs(t *testing.T) {
	// runCLI prints usage on stdout, then returns an error. We want to
	// see both: the error (non-nil), and its message mentioning
	// "no input files".
	var err error
	_ = captureStdout(t, func() {
		withArgs(t, []string{"vellum"}, func() {
			err = run()
		})
	})
	if err == nil {
		t.Fatalf("run() with no args should have returned an error")
	}
	if !strings.Contains(err.Error(), "no input files") {
		t.Errorf("error message = %q, want it to mention %q", err.Error(), "no input files")
	}
}

func TestRun_UnknownFlag(t *testing.T) {
	var err error
	_ = captureStdout(t, func() {
		withArgs(t, []string{"vellum", "--nope"}, func() {
			err = run()
		})
	})
	if err == nil {
		t.Fatalf("run() with unknown flag should have returned an error")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("error message = %q, want it to mention %q", err.Error(), "unknown flag")
	}
}
