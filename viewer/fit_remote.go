// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/marcelocantos/vellum/internal/svgfit"
)

// A remote image that may be an SVG is measured when the viewer can
// fetch it. The browser may reach what the viewer cannot (a page behind
// a sign-in, a network the daemon is not on), so the page never waits:
// it is served with the figure pending, chrome.js asks ChromeSVGFitPath
// once the page is up, and a figure the viewer could not measure stays
// as it was. A measurement, or a failure, is remembered for a while and
// written straight into later pages.
const (
	// ChromeSVGFitPath answers {"natural": px, "ems": n} for a remote SVG
	// figure of a served document, or 204 when it cannot be measured.
	ChromeSVGFitPath = "/_vellum/svg-fit"

	remoteFitTTL       = time.Hour
	remoteFitFailedTTL = 10 * time.Minute
	svgMediaType       = "image/svg+xml"
)

// remoteFit is one remote measurement, finished when done is closed.
type remoteFit struct {
	done chan struct{}
	fit  svgfit.Fit
	ok   bool
	at   time.Time
}

func (r *remoteFit) fresh(now time.Time) bool {
	ttl := remoteFitTTL
	if !r.ok {
		ttl = remoteFitFailedTTL
	}
	return now.Sub(r.at) < ttl
}

type remoteFits struct {
	mu    sync.Mutex
	byURL map[string]*remoteFit
}

// knownRemoteFit is the remembered measurement of src, without waiting:
// known is false when there is none, or it is still being taken.
func (s *Server) knownRemoteFit(src string) (fit svgfit.Fit, ok, known bool) {
	s.remote.mu.Lock()
	r := s.remote.byURL[src]
	s.remote.mu.Unlock()
	if r == nil {
		return svgfit.Fit{}, false, false
	}
	select {
	case <-r.done:
	default:
		return svgfit.Fit{}, false, false
	}
	if !r.fresh(time.Now()) {
		return svgfit.Fit{}, false, false
	}
	return r.fit, r.ok, true
}

// measureRemote measures the SVG at src, fetching it unless a fresh
// measurement is remembered. Concurrent callers share one fetch.
func (s *Server) measureRemote(ctx context.Context, src string) (svgfit.Fit, bool) {
	s.remote.mu.Lock()
	if s.remote.byURL == nil {
		s.remote.byURL = map[string]*remoteFit{}
	}
	r := s.remote.byURL[src]
	if r != nil {
		select {
		case <-r.done:
			if !r.fresh(time.Now()) {
				r = nil
			}
		default:
		}
	}
	owner := r == nil
	if owner {
		r = &remoteFit{done: make(chan struct{})}
		s.remote.byURL[src] = r
	}
	s.remote.mu.Unlock()
	if owner {
		// Not the request's context: other callers wait on this fetch.
		fetchCtx, cancel := context.WithTimeout(context.Background(), remoteImageTimeout)
		data, _, err := fetchRemote(fetchCtx, src, svgMediaType, svgMediaType, svgfit.MaxBytes)
		cancel()
		if err == nil {
			r.fit, r.ok = svgfit.Measure(data)
		}
		r.at = time.Now()
		close(r.done)
	}
	select {
	case <-r.done:
		return r.fit, r.ok
	case <-ctx.Done():
		return svgfit.Fit{}, false
	}
}

// handleSVGFit measures a remote figure of the document named by path.
// It fetches only an image that document shows, so the route is not a
// way to make the viewer fetch anything else.
func (s *Server) handleSVGFit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	absPath, err := queryAbsPath(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	src, ok := remoteImageURL(r.URL.Query().Get("src"))
	if !ok {
		http.Error(w, "src is not a remote image", http.StatusBadRequest)
		return
	}
	body, _, err := s.cachedHTML(r.Context(), absPath)
	if err != nil {
		servedStatus(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if !showsImage(string(body), src) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	fit, ok := s.measureRemote(r.Context(), src)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]float64{"natural": fit.Width, "ems": fit.GrowEm()})
}

// showsImage reports whether page has an image whose src is src.
func showsImage(page, src string) bool {
	z := html.NewTokenizer(strings.NewReader(page))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			if string(name) != "img" {
				continue
			}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				if string(k) == "src" {
					if u, ok := remoteImageURL(strings.TrimSpace(string(v))); ok && u == src {
						return true
					}
				}
			}
		}
	}
}
