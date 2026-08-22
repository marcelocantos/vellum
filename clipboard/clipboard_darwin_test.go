// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// macOS pasteboard UTI constants. NSPasteboard exposes these as
// NSPasteboardType* constants; we use the same string values the
// system maps them to.
const (
	utiRTF   = "public.rtf"
	utiHTML  = "public.html"
	utiPlain = "public.utf8-plain-text"
)

// requirePasteboard skips when there is no usable system pasteboard.
//
// GitHub's macOS runners have no window server, so NSPasteboard setData
// fails outright and reads return whatever a previous process happened
// to leave behind — which surfaces as a confusing cross-run path
// mismatch rather than an honest "unavailable". These tests cover real
// behaviour and pass against a real pasteboard, so they are gated
// rather than deleted; `cv gate` runs them for real on a developer Mac,
// which is where this darwin-only surface has to work.
//
// The skip reason must mention "pasteboard": cv's skip-census permits
// exactly this class and fails on any other skip.
func requirePasteboard(t *testing.T) {
	t.Helper()
	lockPasteboard(t)
	// A probe is not enough: on a hosted runner the pasteboard is
	// intermittently functional — a probe write can succeed and the very
	// next write still fail with setData, while reads return whatever a
	// previous process left behind. Nothing observable distinguishes
	// "works" from "works this once", so key off the environment
	// instead, which is deterministic.
	if os.Getenv("CI") != "" {
		t.Skip("pasteboard unusable on hosted CI runners (no window server); " +
			"cv gate runs these for real on a developer Mac")
	}
	probe := "vellum-pasteboard-probe-" + strconv.Itoa(os.Getpid())
	if _, err := Write(Payload{HTML: "<p>" + probe + "</p>"}); err != nil {
		t.Skipf("pasteboard unavailable (headless session): %v", err)
	}
	if !strings.Contains(string(readPasteboardData(utiHTML)), probe) {
		t.Skip("pasteboard unavailable (headless session): write did not round-trip")
	}
}

// pasteboardLockPath names the lock that serialises every process
// running these tests. The general pasteboard belongs to the login
// session, so the lock is scoped by uid and lives outside any per-run
// temporary directory — two test binaries must agree on the path
// without inheriting an environment from each other.
var pasteboardLockPath = fmt.Sprintf("/tmp/vellum-pasteboard-test-%d.lock", os.Getuid())

// lockPasteboard takes an exclusive machine-wide lock and holds it for
// the rest of the calling test.
//
// NSPasteboard has no compare-and-swap. declareTypes:owner: transfers
// ownership of the general pasteboard, and setData:forType: from a
// process that has since lost ownership returns NO — reported here as
// "clipboard: NSPasteboard setData failed". So two processes writing at
// once is not a race one of them usually wins; it is a race one of them
// necessarily loses, whatever the timing.
//
// `cv gate` runs exactly that: `test` and `skip-census` each run
// `go test ./...`, and cv builds prerequisites in parallel (-j auto), so
// two copies of this package drive the one pasteboard concurrently.
// That is the whole of the 2026-08-15 gate failure, reproduced on the
// first attempt by running two copies of this test binary side by side:
// the sandbox child's write lost its declared types and exited 1, and
// requirePasteboard's own probe turned the same loss into a skip.
//
// The lock is what makes exclusivity structural rather than lucky. It
// is held across the sandbox child's run too: the child re-executes this
// binary in TestMain and never reaches a test, so the parent's hold
// covers it and there is no second acquisition to deadlock on.
//
// It cannot defend against a human pressing ⌘C mid-gate — nothing can,
// the pasteboard is genuinely shared — but that is a person at the
// keyboard, not a scheduling accident the gate produces on its own.
func lockPasteboard(t *testing.T) {
	t.Helper()
	f, err := os.OpenFile(pasteboardLockPath, os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		t.Fatalf("opening pasteboard lock %s: %v", pasteboardLockPath, err)
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		// The Go runtime preempts goroutines with signals, so a blocking
		// flock returns EINTR on a schedule of its own. Reissuing the
		// same wait is how the call is spelled correctly; it is not a
		// retry of a failed lock.
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		break
	}
	if err != nil {
		f.Close()
		t.Fatalf("locking %s: %v", pasteboardLockPath, err)
	}
	t.Cleanup(func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	})
}

// TestWriteRoundTrip exercises the macOS NSPasteboard backend end-to-end:
// writes a known HTML fragment, reads each representation back as raw
// pasteboard data via readPasteboardData, and asserts that:
//   - the RTF payload is well-formed (starts with the `{\rtf` signature)
//     and contains the marker word — proving HTML→RTF conversion ran
//     and the data was placed under public.rtf
//   - the HTML payload is present under public.html
//   - the plain-text payload contains the marker and does NOT leak raw
//     RTF source — the failure mode the textutil+osascript path produces
//     when only RTF is set and apps fall back to plain text
func TestWriteRoundTrip(t *testing.T) {
	requirePasteboard(t)
	const marker = "vellum-clipboard-roundtrip-marker"
	html := "<html><body><p><b>" + marker + "</b></p></body></html>"

	if _, err := Write(Payload{HTML: html}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	rtf := readPasteboardData(utiRTF)
	if !bytes.HasPrefix(rtf, []byte(`{\rtf`)) {
		t.Errorf("RTF payload missing `{\\rtf` signature; got %q", firstN(rtf, 40))
	}
	if !bytes.Contains(rtf, []byte(marker)) {
		t.Errorf("RTF payload does not contain marker %q", marker)
	}

	htmlData := readPasteboardData(utiHTML)
	if len(htmlData) == 0 {
		t.Error("HTML payload missing from pasteboard")
	}

	plain := readPasteboardData(utiPlain)
	if !bytes.Contains(plain, []byte(marker)) {
		t.Errorf("plain payload does not contain marker %q; got %q", marker, plain)
	}
	if strings.Contains(string(plain), `{\rtf`) {
		t.Errorf("plain payload leaked raw RTF source; got %q", firstN(plain, 80))
	}
}

// TestWriteFragmentsHTMLAndStripsLineSeparators covers the two
// regressions that surfaced from the first round of paste-target
// testing:
//   - Slack rejects a full HTML document with <head><style>; the
//     pasteboard's public.html rep must be a body fragment.
//   - VS Code (and other editors) flag U+2028 LINE SEPARATOR / U+2029
//     PARAGRAPH SEPARATOR as "unusual line terminators". The plain-text
//     rep must use ordinary U+000A newlines.
func TestWriteFragmentsHTMLAndStripsLineSeparators(t *testing.T) {
	requirePasteboard(t)
	const marker = "vellum-fragment-marker"
	full := `<!DOCTYPE html><html><head><meta charset="utf-8"><style>body{color:red}</style></head><body><p>` + marker + `</p><p>second paragraph</p></body></html>`

	if _, err := Write(Payload{HTML: full}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	htmlData := readPasteboardData(utiHTML)
	htmlStr := string(htmlData)
	if !strings.Contains(htmlStr, marker) {
		t.Errorf("HTML rep missing marker; got %q", htmlStr)
	}
	for _, forbidden := range []string{"<head>", "<style>", "<!DOCTYPE", "<html>"} {
		if strings.Contains(htmlStr, forbidden) {
			t.Errorf("HTML rep should be a body fragment, but contains %q; got %q", forbidden, htmlStr)
		}
	}

	plain := string(readPasteboardData(utiPlain))
	if strings.ContainsRune(plain, ' ') {
		t.Errorf("plain-text rep contains U+2028 LINE SEPARATOR; should have been normalised to \\n; got %q", plain)
	}
	if strings.ContainsRune(plain, ' ') {
		t.Errorf("plain-text rep contains U+2029 PARAGRAPH SEPARATOR; should have been normalised to \\n; got %q", plain)
	}
}

func TestWriteEmptyHTMLRejected(t *testing.T) {
	if _, err := Write(Payload{}); err == nil {
		t.Fatal("expected error for empty payload, got nil")
	}
}

func TestFileRefRoundTrip(t *testing.T) {
	requirePasteboard(t)
	dir := t.TempDir()
	path := dir + "/vellum-file-ref-marker.txt"
	if err := os.WriteFile(path, []byte("ref"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileRefs(FileRefPayload{Paths: []string{path}}); err != nil {
		t.Fatalf("WriteFileRefs: %v", err)
	}
	got, err := ReadFileRefs()
	if err != nil {
		t.Fatalf("ReadFileRefs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d paths: %v", len(got), got)
	}
	if got[0] != path {
		// Allow cleaned absolute forms.
		if filepath.Clean(got[0]) != filepath.Clean(path) {
			t.Fatalf("path: got %q want %q", got[0], path)
		}
	}
}
