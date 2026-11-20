// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package viewer

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Reveal and watch look at the path after the roots check. Replacing the
// file with a symlink to a secret in between must not reveal or stamp it.
func TestRevealAndWatch_NoticeASwapAfterTheCheck(t *testing.T) {
	f := newRootsFixture(t)
	target := filepath.Join(f.docs, "swap.md")
	mustWrite(t, target, "# inside\n")
	var revealed []string
	s := &Server{
		CacheDir: filepath.Join(t.TempDir(), "cache"),
		Roots:    []string{f.docs},
		Reveal:   func(p string) error { revealed = append(revealed, p); return nil },
	}
	swapped := false
	s.beforeOpen = func(p string) {
		if p != target || swapped {
			return
		}
		swapped = true
		if err := os.Remove(target); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(f.secret, target); err != nil {
			t.Error(err)
		}
	}
	ts := startTestServer(t, s)

	code, body := pageRequest(t, http.MethodPost, ts.URL+ChromeRevealPath+"?path="+url.QueryEscape(target))
	if !swapped {
		t.Fatal("reveal did not open the path after the check")
	}
	if len(revealed) != 0 || strings.Contains(body, secretBytes) || code == http.StatusOK {
		t.Fatalf("reveal followed the swap: status %d revealed %v body %.80q", code, revealed, body)
	}
	if code != http.StatusForbidden {
		t.Fatalf("reveal status %d, want 403: %s", code, body)
	}

	// Put the file back; the watch request swaps it again.
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, target, "# inside\n")
	swapped = false
	code, body = pageRequest(t, http.MethodGet, ts.URL+ChromeWatchPath+"?path="+url.QueryEscape(target))
	if !swapped {
		t.Fatal("watch did not open the path after the check")
	}
	if strings.Contains(body, secretBytes) || code == http.StatusOK || code == http.StatusSwitchingProtocols {
		t.Fatalf("watch followed the swap: status %d body %.80q", code, body)
	}
	if code != http.StatusForbidden {
		t.Fatalf("watch status %d, want 403: %s", code, body)
	}
}
