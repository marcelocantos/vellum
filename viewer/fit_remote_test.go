// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// A remote SVG figure is pending on the first page, measured when the
// browser asks, and fitted in the page from then on. One the viewer
// cannot fetch stays as it is.
func TestRemoteSVGFigure(t *testing.T) {
	var fetches atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		switch r.URL.Path {
		case "/small.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(fitSampleSVG))
		case "/badge":
			w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
			_, _ = w.Write([]byte(fitSampleSVG))
		case "/not-svg.svg":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG"))
		default:
			http.Error(w, "sign in first", http.StatusUnauthorized)
		}
	}))
	t.Cleanup(remote.Close)

	s, _, md := fitFixture(t)
	doc := "# Remote\n\n![a](" + remote.URL + "/small.svg)\n\n![b](" + remote.URL + "/badge)\n\n![c](" + remote.URL + "/private.svg)\n\n![d](" + remote.URL + "/not-svg.svg)\n"
	if err := os.WriteFile(md, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := startTestServer(t, s)
	_, _, page := getText(t, ViewURL(ts.URL, md))
	// The article only: chrome.js, inlined in the page, names the markers.
	article := func(page string) string {
		start, end := strings.Index(page, "<article"), strings.Index(page, "</article>")
		if start < 0 || end < start {
			t.Fatalf("no article in the page:\n%s", page)
		}
		return page[start:end]
	}
	if n := strings.Count(article(page), fitPendingAttr+`=""`); n != 4 {
		t.Fatalf("%d pending images on the first page, want 4:\n%s", n, page)
	}
	if fetches.Load() != 0 {
		t.Fatalf("the page waited on %d fetches", fetches.Load())
	}

	ask := func(src string) (int, map[string]float64) {
		t.Helper()
		u := ts.URL + ChromeSVGFitPath + "?path=" + url.QueryEscape(md) + "&src=" + url.QueryEscape(src)
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		req.Header.Set(secFetchSiteHeader, secFetchSiteSameOrigin)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var got map[string]float64
		if resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
		}
		return resp.StatusCode, got
	}
	for _, src := range []string{remote.URL + "/small.svg", remote.URL + "/badge"} {
		status, got := ask(src)
		if status != http.StatusOK || got["natural"] != 300 || got["ems"] != 22.5 {
			t.Errorf("%s: %d %v, want 300px growing to 22.5em", src, status, got)
		}
	}
	for _, src := range []string{remote.URL + "/private.svg", remote.URL + "/not-svg.svg"} {
		if status, _ := ask(src); status != http.StatusNoContent {
			t.Errorf("%s: %d, want 204", src, status)
		}
	}
	// Only what the document shows.
	before := fetches.Load()
	if status, _ := ask(remote.URL + "/elsewhere.svg"); status != http.StatusNoContent || fetches.Load() != before {
		t.Errorf("an image the document does not show: %d, fetched %d", status, fetches.Load()-before)
	}
	// Remembered: no second fetch.
	if _, _ = ask(remote.URL + "/small.svg"); fetches.Load() != before {
		t.Errorf("measured again: %d fetches", fetches.Load()-before)
	}

	_, _, page = getText(t, ViewURL(ts.URL, md))
	if n := strings.Count(page, `style="`+fitSampleStyle+`"`); n != 2 {
		t.Errorf("%d remote figures fitted on the next page, want 2:\n%s", n, page)
	}
	if n := strings.Count(article(page), fitPendingAttr); n != 0 {
		t.Errorf("%d still pending; a failure is remembered too:\n%s", n, page)
	}
	if strings.Contains(article(page), figurePendingAttr) {
		t.Errorf("a figure block is still pending")
	}
}

// A document's own <svg>, written as an HTML block, is fitted in the page
// the viewer serves; one in a sentence is not.
func TestServer_PageFitsInlineSVG(t *testing.T) {
	s, _, md := fitFixture(t)
	doc := "# Inline\n\n<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"300\" height=\"100\" viewBox=\"0 0 300 100\">\n  <text font-size=\"10\">some label text</text>\n</svg>\n\nAn icon " + fitSampleSVG + " in a sentence.\n"
	if err := os.WriteFile(md, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := startTestServer(t, s)
	status, _, page := getText(t, ViewURL(ts.URL, md))
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if n := strings.Count(page, fitSampleStyle); n != 1 {
		t.Fatalf("%d drawings sized, want the one that stands alone:\n%s", n, page)
	}
	if n := strings.Count(page, figureAttr+`=""`); n != 1 {
		t.Fatalf("%d figures marked, want 1", n)
	}
}
