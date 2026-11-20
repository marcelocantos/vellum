// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSampleDocumentsStillRender(t *testing.T) {
	repo := filepath.Join("..", "test")
	docs := []struct {
		name string
		need []string
	}{
		{"sample.md", []string{"mpe2pdf Feature Showcase", "Incomplete task", "GFM tables", "Definition for term 1"}},
		{"viewer-smoke.md", []string{"Viewer smoke", "Passport", "Deep section"}},
		{filepath.Join("verify-release", "doc.md"), []string{"Verify Release Fixture", "tableCellOmega", "codeSentinel42", "Second Heading"}},
	}
	dir, err := filepath.Abs(repo)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{CacheDir: t.TempDir(), Roots: []string{dir}}
	ts := startTestServer(t, s)
	for _, doc := range docs {
		t.Run(doc.name, func(t *testing.T) {
			p := filepath.Join(dir, doc.name)
			if _, err := os.Stat(p); err != nil {
				t.Fatal(err)
			}
			resp, err := http.Get(ts.URL + pathURL(p))
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			body := string(b)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %.200s", resp.StatusCode, body)
			}
			if strings.Contains(strings.ToLower(body), "<script") && !strings.Contains(body, "nonce=") {
				t.Fatalf("document script without the viewer nonce:\n%.200s", body)
			}
			for _, needle := range doc.need {
				if !strings.Contains(body, needle) {
					t.Errorf("page lost %q", needle)
				}
			}
		})
	}
}
