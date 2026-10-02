// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// ChromeFragmentPath serves one deferred article fragment (a Mermaid SVG)
// by content id. Not a filesystem path.
const ChromeFragmentPath = "/_vellum/fragment"

// fragmentIDLen is the hex length of a fragment id (64 bits of SHA-256).
const fragmentIDLen = 16

const (
	mermaidWrapperOpen = `<div class="mermaid-svg"`
	svgOpen            = "<svg"
	svgClose           = "</svg>"
	divClose           = "</div>"
)

var (
	imgTagRe       = regexp.MustCompile(`(?i)<(img)\b([^>]*)>`)
	loadingAttrRe  = regexp.MustCompile(`(?i)\bloading\s*=`)
	decodingAttrRe = regexp.MustCompile(`(?i)\bdecoding\s*=`)
	viewBoxRe      = regexp.MustCompile(`(?i)\bviewBox\s*=\s*["']\s*[-0-9.]+[\s,]+[-0-9.]+[\s,]+([0-9.]+)[\s,]+([0-9.]+)\s*["']`)
	maxWidthRe     = regexp.MustCompile(`(?i)max-width\s*:\s*([0-9.]+(?:px|em|rem|%))`)
	fragmentIDRe   = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// lazyLoadImages marks every <img> as browser-lazy so first paint of the
// article text does not wait on image bytes. Tags that already declare
// loading= are left alone (an author's eager hint wins).
func lazyLoadImages(html string) string {
	return imgTagRe.ReplaceAllStringFunc(html, func(m string) string {
		sub := imgTagRe.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		tag, attrs := sub[1], sub[2]
		if !loadingAttrRe.MatchString(attrs) {
			attrs += ` loading="lazy"`
		}
		if !decodingAttrRe.MatchString(attrs) {
			attrs += ` decoding="async"`
		}
		return "<" + tag + attrs + ">"
	})
}

// fragmentID is the content id of one deferred fragment.
func fragmentID(svg string) string {
	sum := sha256.Sum256([]byte(svg))
	return hex.EncodeToString(sum[:])[:fragmentIDLen]
}

// deferMermaid lifts every inline Mermaid SVG out of html and replaces it
// with a same-size placeholder that chrome.js fills from
// ChromeFragmentPath once the diagram scrolls near the viewport. The
// returned map holds each lifted SVG by content id. Wrappers that hold a
// render-failure <pre> (no SVG) stay inline. html is returned unchanged
// (nil map) when no diagram is lifted.
//
// mmdc emits SVG with nested <svg> (icons) and <foreignObject><div>
// labels, so the wrapper's closing </div> is found by tracking <svg>
// depth rather than by matching div tags.
func deferMermaid(html string) (string, map[string]string) {
	var out strings.Builder
	var fragments map[string]string
	pos := 0
	for {
		rel := strings.Index(html[pos:], mermaidWrapperOpen)
		if rel < 0 {
			break
		}
		start := pos + rel
		gt := strings.IndexByte(html[start:], '>')
		if gt < 0 {
			break
		}
		bodyStart := start + gt + 1
		svgLen, ok := inlineSVGLength(html[bodyStart:])
		if !ok || !strings.HasPrefix(html[bodyStart+svgLen:], divClose) {
			out.WriteString(html[pos:bodyStart])
			pos = bodyStart
			continue
		}
		svg := html[bodyStart : bodyStart+svgLen]
		id := fragmentID(svg)
		if fragments == nil {
			fragments = map[string]string{}
		}
		fragments[id] = svg
		out.WriteString(html[pos:bodyStart])
		out.WriteString(lazySVGPlaceholder(id, svg))
		pos = bodyStart + svgLen
	}
	if fragments == nil {
		return html, nil
	}
	out.WriteString(html[pos:])
	return out.String(), fragments
}

// inlineSVGLength returns the byte length of the <svg>…</svg> element at
// the start of s, counting nested <svg> elements. ok is false when s does
// not start with <svg or the element never closes.
func inlineSVGLength(s string) (int, bool) {
	if !strings.HasPrefix(s, svgOpen) {
		return 0, false
	}
	depth := 0
	i := 0
	for {
		o := strings.Index(s[i:], svgOpen)
		c := strings.Index(s[i:], svgClose)
		if c < 0 {
			return 0, false
		}
		if o >= 0 && o < c {
			depth++
			i += o + len(svgOpen)
			continue
		}
		depth--
		i += c + len(svgClose)
		if depth == 0 {
			return i, true
		}
	}
}

// lazySVGPlaceholder reserves the diagram's box (aspect ratio from the
// viewBox, max-width from the root style) so text below it does not
// shift when the SVG arrives.
func lazySVGPlaceholder(id, svg string) string {
	rootEnd := strings.IndexByte(svg, '>')
	if rootEnd < 0 {
		rootEnd = len(svg) - 1
	}
	root := svg[:rootEnd+1]
	var style []string
	if vb := viewBoxRe.FindStringSubmatch(root); len(vb) == 3 && vb[1] != "0" && vb[2] != "0" {
		style = append(style, "aspect-ratio: "+vb[1]+" / "+vb[2])
	}
	if mw := maxWidthRe.FindStringSubmatch(root); len(mw) == 2 {
		style = append(style, "max-width: "+mw[1])
	}
	attrs := ` class="vellum-lazy-svg" data-vellum-fragment="` + id + `" role="img" aria-label="Diagram (loading)"`
	if len(style) > 0 {
		attrs += ` style="` + strings.Join(style, "; ") + `"`
	}
	return "<div" + attrs + "></div>"
}

// handleFragment serves one deferred fragment of the cached convert HTML
// for ?path=…&id=…. Ids are content hashes, so the response is immutable.
func (s *Server) handleFragment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	absPath, err := queryAbsPath(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !fragmentIDRe.MatchString(id) {
		http.Error(w, "missing or malformed fragment id", http.StatusBadRequest)
		return
	}
	body, err := s.cachedHTML(r.Context(), absPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, errIsDirectory) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, fragments := deferMermaid(string(body))
	svg, ok := fragments[id]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = io.WriteString(w, svg)
}
