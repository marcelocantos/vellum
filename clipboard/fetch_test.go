// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package clipboard

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/vellum/internal/fileurl"
	"github.com/marcelocantos/vellum/internal/pandoc"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

// converterFixture is HTML naming an image four ways: by absolute path,
// by file: URL, by http URL (a recording server), and as a data: URL. A
// clipboard converter may use only the last (🎯T66).
type converterFixture struct {
	html     string
	requests func() []string
}

func newConverterFixture(t *testing.T) converterFixture {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 7, 5))
	for y := 0; y < 5; y++ {
		for x := 0; x < 7; x++ {
			img.Set(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(secret, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	css := filepath.Join(filepath.Dir(secret), "secret.css")
	if err := os.WriteFile(css, []byte("p{color:#ff0000}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(b.Bytes())
	}))
	t.Cleanup(ts.Close)
	html := `<html><head>` +
		`<link rel="stylesheet" href="` + ts.URL + `/remote.css">` +
		`<link rel="stylesheet" href="` + fileurl.FromPath(css) + `">` +
		`</head><body><p>Hello</p>` +
		`<img src="` + secret + `">` +
		`<img src="` + fileurl.FromPath(secret) + `">` +
		`<img src="` + ts.URL + `/remote.png">` +
		`<img src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(b.Bytes()) + `">` +
		`</body></html>`
	return converterFixture{html: html, requests: func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}}
}

func TestPandocRichText_ReadsAndFetchesNothing(t *testing.T) {
	testdeps.Need(t, pandoc.Binary)
	f := newConverterFixture(t)
	rtf, plain, err := pandocRichText(f.html)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.requests(); len(got) != 0 {
		t.Errorf("pandoc fetched %v", got)
	}
	// Only the data: image embeds.
	if n := strings.Count(string(rtf), `\pict`); n != 1 {
		t.Errorf("rtf embeds %d images, want 1 (the data: URL):\n%.600s", n, rtf)
	}
	if !strings.Contains(string(plain), "Hello") {
		t.Errorf("plain text lost the document: %q", plain)
	}
}
