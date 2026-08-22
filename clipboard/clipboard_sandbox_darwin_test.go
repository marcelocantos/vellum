// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package clipboard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The owner's 2026-08-15 report: every to.media=clipboard conversion
// failed with "clipboard: failed to parse HTML into NSAttributedString",
// independent of input, while `pandoc file.md -t rtf | pbcopy` worked
// (🎯T23).
//
// On macOS 26 the HTML importer behind NSAttributedString is not
// in-process: initWithData:NSHTMLTextDocumentType brokers to a per-user
// launchd agent, com.apple.textkit.nsattributedstringagent. Where that
// agent cannot be looked up — a sandboxed process, or any context
// outside the user's GUI session, which is how vellum runs behind the
// Aperture gateway — the initialiser returns nil for every input. The
// pasteboard itself is unaffected, which is exactly why the pbcopy
// workaround kept working.
//
// Denying that one mach service reproduces the report deterministically
// on any Mac, so the environment the owner hit is a test fixture rather
// than a story. Nothing else is denied: the window server, WebKit and
// the pasteboard all stay reachable, so a failure here is the TextKit
// agent and nothing else.
const textKitAgent = "com.apple.textkit.nsattributedstringagent"

// The window server and the render server carry the obvious rival
// explanation — "AppKit needs a GUI session" — and TestWriteKeepsAppKit-
// RouteWithWindowServerDenied is what refutes it. Denying these two
// leaves the AppKit route working, so "no GUI session" does not explain
// the owner's failure and the TextKit agent is the whole mechanism.
//
// com.apple.pasteboard.1 is deliberately NOT denied: it is the
// pasteboard server itself, and denying it breaks the write for reasons
// that have nothing to do with the conversion route under test.
var windowServerNames = []string{
	"com.apple.windowserver.active",
	"com.apple.CARenderServer",
}

// sandboxChildEnv marks the re-executed test binary as the child that
// performs the write under the sandbox. The parent reads the pasteboard
// back afterwards.
const sandboxChildEnv = "VELLUM_TEST_CLIPBOARD_SANDBOX_CHILD"

// sandboxChildMarker is the word the child places on the pasteboard in
// bold. The parent looks for it in every representation.
const sandboxChildMarker = "vellum-textkit-denied-marker"

func TestMain(m *testing.M) {
	if os.Getenv(sandboxChildEnv) == "" {
		os.Exit(m.Run())
	}
	// Child mode: write, report, exit. Never runs the test suite.
	html := "<html><head><style>body{color:red}</style></head><body><p>Hello <b>" +
		sandboxChildMarker + "</b>.</p></body></html>"
	rep, err := Write(Payload{HTML: html})
	if err != nil {
		os.Stdout.WriteString("WRITE-ERR: " + err.Error() + "\n")
		os.Exit(1)
	}
	os.Stdout.WriteString("WRITE-OK route=" + string(rep.Route) +
		" fallback=" + rep.Fallback + "\n")
	os.Exit(0)
}

// writeWithMachServicesDenied re-executes this test binary under a
// sandbox profile that denies mach-lookup for exactly the named global
// services and nothing else, and returns what the child reported.
//
// sandbox-exec is stock macOS, so a missing one is a real signal about
// the machine rather than a reason to stop testing: this fails loudly
// instead of skipping. Silent skipping is how the defect behind 🎯T23
// reached the owner in the first place.
func writeWithMachServicesDenied(t *testing.T, name string, services []string) string {
	t.Helper()

	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Fatalf("sandbox-exec not found on PATH: %v; it ships with macOS, "+
			"and without it the 🎯T23 mechanism cannot be reproduced", err)
	}

	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n(deny mach-lookup\n")
	for _, s := range services {
		b.WriteString("  (global-name \"" + s + "\")\n")
	}
	b.WriteString(")\n")

	profile := filepath.Join(t.TempDir(), name+".sb")
	if err := os.WriteFile(profile, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("writing sandbox profile: %v", err)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating test binary: %v", err)
	}
	cmd := exec.Command("sandbox-exec", "-f", profile, self)
	cmd.Env = append(os.Environ(), sandboxChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("clipboard write failed with %v unreachable: %v\n%s", services, err, out)
	}
	if !strings.Contains(string(out), "WRITE-OK") {
		t.Fatalf("child did not report a successful write:\n%s", out)
	}
	return string(out)
}

// TestWriteSurvivesUnreachableTextKitAgent is the failing arm for 🎯T23:
// with the TextKit agent denied, the clipboard must still receive rich
// text with the bold run intact.
//
// Against the code as the owner found it this fails with the reported
// error, because writePayload treated a nil NSAttributedString as fatal
// for the whole transaction rather than as one unavailable rendering
// route.
//
// Read it together with TestWriteKeepsAppKitRouteWithWindowServerDenied:
// on its own this arm shows only that something in a sandbox breaks the
// write. The pair is what pins the cause to this one service.
func TestWriteSurvivesUnreachableTextKitAgent(t *testing.T) {
	requirePasteboard(t)

	out := []byte(writeWithMachServicesDenied(t, "deny-textkit", []string{textKitAgent}))
	// Without this the test could pass vacuously: if AppKit's importer
	// ever stops needing the agent, the profile no longer reproduces the
	// owner's environment and the fallback is never exercised. A
	// mismatch here means the fixture — not the fallback — needs
	// revisiting.
	if !strings.Contains(string(out), "route="+string(RoutePandoc)) {
		t.Errorf("expected the %s fallback route with %s denied; child reported:\n%s",
			RoutePandoc, textKitAgent, out)
	}
	// The degraded write must announce itself; a silent fallback leaves
	// the user pasting unstyled output with no signal.
	if !strings.Contains(string(out), "fallback=") ||
		strings.Contains(string(out), "fallback=\n") {
		t.Errorf("fallback route reported no reason; child reported:\n%s", out)
	}

	rtf := string(readPasteboardData(utiRTF))
	if !strings.HasPrefix(rtf, `{\rtf`) {
		t.Errorf("RTF rep missing the {\\rtf signature; got %q", firstN([]byte(rtf), 60))
	}
	if !strings.Contains(rtf, sandboxChildMarker) {
		t.Errorf("RTF rep does not contain marker %q; got %q", sandboxChildMarker, firstN([]byte(rtf), 200))
	}
	// The acceptance criterion is rich text, not merely bytes under
	// public.rtf: the marker must still carry a bold run. Both RTF
	// producers spell that \b before the marker word.
	if bold := strings.LastIndex(rtf[:strings.Index(rtf, sandboxChildMarker)+1], `\b`); bold < 0 {
		t.Errorf("RTF rep lost the bold run around %q; got %q", sandboxChildMarker, rtf)
	}

	htmlRep := string(readPasteboardData(utiHTML))
	if !strings.Contains(htmlRep, sandboxChildMarker) {
		t.Errorf("HTML rep missing marker; got %q", htmlRep)
	}
	if strings.Contains(htmlRep, "<style>") {
		t.Errorf("HTML rep should be a body fragment; got %q", htmlRep)
	}

	plain := string(readPasteboardData(utiPlain))
	if !strings.Contains(plain, sandboxChildMarker) {
		t.Errorf("plain rep missing marker; got %q", plain)
	}
	if strings.Contains(plain, `{\rtf`) {
		t.Errorf("plain rep leaked raw RTF source; got %q", firstN([]byte(plain), 80))
	}
}

// TestWriteKeepsAppKitRouteWithWindowServerDenied is the control that
// makes the arm above mean something.
//
// The rival explanation for the owner's report was "AppKit needs a GUI
// session, and vellum runs outside one". If that were the mechanism,
// denying the window server would fall back too. It does not: the
// AppKit route stays, so the GUI session is not what is missing and the
// TextKit agent named in the failing arm is the whole cause.
//
// This test is also the fixture's own tripwire. If the sandbox ever
// stops isolating anything — a profile that denies nothing, a
// sandbox-exec that silently no-ops — the failing arm goes green for
// the wrong reason while this one stays green and hides it. Route
// equality here is what makes that impossible: appkit and pandoc cannot
// both be the answer.
func TestWriteKeepsAppKitRouteWithWindowServerDenied(t *testing.T) {
	requirePasteboard(t)

	out := writeWithMachServicesDenied(t, "deny-windowserver", windowServerNames)
	if !strings.Contains(out, "route="+string(RouteAppKit)) {
		t.Errorf("expected the %s route to survive %v being denied; child reported:\n%s",
			RouteAppKit, windowServerNames, out)
	}
	// The preferred route must stay quiet: a fallback notice here would
	// mean vellum reports degraded output on a machine whose only
	// missing piece is the window server.
	if !strings.Contains(out, "fallback=\n") {
		t.Errorf("preferred route reported a fallback reason; child reported:\n%s", out)
	}

	rtf := string(readPasteboardData(utiRTF))
	if !strings.HasPrefix(rtf, `{\rtf`) {
		t.Errorf("RTF rep missing the {\\rtf signature; got %q", firstN([]byte(rtf), 60))
	}
	if !strings.Contains(rtf, sandboxChildMarker) {
		t.Errorf("RTF rep does not contain marker %q; got %q", sandboxChildMarker, firstN([]byte(rtf), 200))
	}
}
