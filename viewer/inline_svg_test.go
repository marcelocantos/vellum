// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The page the viewer serves keeps a document's inline SVG as SVG, with
// its text, links, styles, definitions, animation and labels (🎯T83), and
// removes only what can run.
func TestServer_PageKeepsInlineSVG(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(dir, "drawing.md")
	src := "# Drawing\n\n" +
		`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 200 80" width="400" onload="alert(1)">` +
		`<style>.box { fill: #def }</style>` +
		`<defs><linearGradient id="grad"><stop offset="0" stop-color="#fff"/></linearGradient></defs>` +
		`<rect class="box" width="80" height="30" fill="url(#grad)" style="stroke-width:2"/>` +
		`<a xlink:href="https://example.com/s"><text x="10" y="25">Service <tspan font-weight="bold">A</tspan></text></a>` +
		`<circle r="5"><animate attributeName="r" values="3;6;3" dur="2s"/></circle>` +
		`<foreignObject width="100" height="30"><div xmlns="http://www.w3.org/1999/xhtml" style="font-size:10px">label</div></foreignObject>` +
		`<script>alert(1)</script>` +
		`</svg>` + "\n\n" +
		`Icon <svg width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="5" fill="green"/></svg> inline.` + "\n"
	if err := os.WriteFile(md, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := startTestServer(t, &Server{Roots: []string{dir}})
	status, _, page := getText(t, ViewURL(ts.URL, md))
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	body := page[strings.Index(page, `class="vellum-article"`):]
	for _, want := range []string{
		`viewBox="0 0 200 80" width="400"`,
		`<style>.box { fill: #def }</style>`,
		`<linearGradient id="grad"><stop offset="0" stop-color="#fff"></stop></linearGradient>`,
		`<rect class="box" width="80" height="30" fill="url(#grad)" style="stroke-width:2"></rect>`,
		`<a xlink:href="https://example.com/s"><text x="10" y="25">Service <tspan font-weight="bold">A</tspan></text></a>`,
		`<animate attributeName="r" values="3;6;3" dur="2s"></animate>`,
		`<foreignObject width="100" height="30"><div xmlns="http://www.w3.org/1999/xhtml" style="font-size:10px">label</div></foreignObject>`,
		`Icon <svg width="12" height="12" viewBox="0 0 12 12"><circle cx="6" cy="6" r="5" fill="green"></circle></svg> inline.`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("lost from the served page: %s", want)
		}
	}
	for _, bad := range []string{"alert(1)", "onload"} {
		if strings.Contains(body, bad) {
			t.Errorf("%s survived in the served page", bad)
		}
	}
	if t.Failed() {
		t.Logf("served article:\n%s", body)
	}
}
