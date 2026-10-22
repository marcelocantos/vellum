// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/internal/testdeps"
)

// 🎯T50: wide tables and oversized figures must fit the printable page
// instead of clipping at the right margin (the old github.css used
// width:max-content + overflow:auto, which scrolls in a browser and
// truncates under WeasyPrint/Prince).
func TestConvert_WideArtifactsFitPage(t *testing.T) {
	testdeps.Need(t, "pdftotext")

	const n = 12
	cols := make([]string, n)
	cells := make([]string, n)
	for i := 0; i < n; i++ {
		// Short tokens so pdftotext keeps each marker contiguous after
		// the table wraps; the distinctive UNIQUE{i} form still fails
		// under the old clip-at-margin CSS because trailing columns vanish.
		cols[i] = fmt.Sprintf("HDR%d", i)
		cells[i] = fmt.Sprintf("UNIQUE%d", i)
	}
	header := "| " + strings.Join(cols, " | ") + " |"
	sepParts := make([]string, n)
	for i := range sepParts {
		sepParts[i] = "---"
	}
	sep := "| " + strings.Join(sepParts, " | ") + " |"
	row := "| " + strings.Join(cells, " | ") + " |"

	const leftMarker = "LEFTWIDEMARKER"
	const rightMarker = "RIGHTWIDEMARKER"
	svgBody := `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="2000" height="80" viewBox="0 0 2000 80">
  <rect width="2000" height="80" fill="#ddeeff"/>
  <text x="20" y="50" font-family="sans-serif" font-size="36" fill="black">` + leftMarker + `</text>
  <text x="1700" y="50" font-family="sans-serif" font-size="36" fill="black">` + rightMarker + `</text>
</svg>`

	md := "# Wide\n\n" + header + "\n" + sep + "\n" + row + "\n\n![wide](wide.svg)\n"

	for _, backend := range backends(t) {
		t.Run(backend, func(t *testing.T) {
			dir := t.TempDir()
			in := filepath.Join(dir, "doc.md")
			svg := filepath.Join(dir, "wide.svg")
			out := filepath.Join(dir, "doc.pdf")
			if err := os.WriteFile(svg, []byte(svgBody), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(in, []byte(md), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := Convert(context.Background(), in, out, &Options{Backend: backend}); err != nil {
				t.Fatalf("Convert: %v", err)
			}
			txt := filepath.Join(dir, "doc.txt")
			if o, err := exec.Command("pdftotext", out, txt).CombinedOutput(); err != nil {
				t.Fatalf("pdftotext: %v: %s", err, o)
			}
			body, err := os.ReadFile(txt)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.ReplaceAll(string(body), "\n", "")
			for _, want := range cells {
				if !strings.Contains(text, want) {
					t.Errorf("table cell %q truncated from PDF:\n%s", want, body)
				}
			}
			for _, want := range cols {
				if !strings.Contains(text, want) {
					t.Errorf("table header %q truncated from PDF:\n%s", want, body)
				}
			}
			if !strings.Contains(text, leftMarker) {
				t.Errorf("wide image left marker %q missing:\n%s", leftMarker, body)
			}
			if !strings.Contains(text, rightMarker) {
				t.Errorf("wide image right marker %q missing (image clipped):\n%s", rightMarker, body)
			}
		})
	}
}
