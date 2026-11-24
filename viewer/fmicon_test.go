// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"bytes"
	"net/http"
	"runtime"
	"strings"
	"testing"
)

func TestFileManagerIcon_Served(t *testing.T) {
	ts := startTestServer(t, &Server{CacheDir: t.TempDir()})
	resp, body := doEdit(t, http.MethodGet, ts.URL+ChromeFileManagerIconPath, "", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	ctype := resp.Header.Get("Content-Type")
	if runtime.GOOS == "darwin" {
		// Finder's own icon, converted from the system bundle.
		if ctype != "image/png" || !bytes.HasPrefix([]byte(body), []byte("\x89PNG\r\n\x1a\n")) {
			t.Fatalf("want Finder's icon as PNG, got %q (%d bytes)", ctype, len(body))
		}
		return
	}
	if !strings.HasPrefix(ctype, "image/svg+xml") || body != folderIconSVG {
		t.Fatalf("want the folder SVG, got %q", ctype)
	}
}

func TestInjectChrome_ToolbarIconButtons(t *testing.T) {
	out := injectChrome(`<html><head></head><body><h1>x</h1></body></html>`, &chromePage{SourcePath: "/docs/note.md"})
	out, _, _ = strings.Cut(out, "<script>\n"+chromeJS)
	for _, action := range []string{"edit", "clipboard", "reveal"} {
		i := strings.Index(out, `data-vellum="`+action+`"`)
		if i < 0 {
			t.Fatalf("no %s button", action)
		}
		start := strings.LastIndex(out[:i], "<button")
		end := strings.Index(out[i:], "</button>")
		button := out[start : i+end]
		if !strings.Contains(button, `class="vellum-btn"`) || !strings.Contains(button, `aria-label="`) {
			t.Errorf("%s button is not an icon button with a label: %s", action, button)
		}
		if !strings.Contains(button, "<svg") && !strings.Contains(button, `src="`+ChromeFileManagerIconPath+`"`) {
			t.Errorf("%s button has no icon: %s", action, button)
		}
	}
}
