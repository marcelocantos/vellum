// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/marcelocantos/vellum/internal/svgfit"
)

// An SVG on the page is drawn as wide as the room allows, but no larger
// than its text warrants (internal/svgfit). The server measures the SVG
// and writes two custom properties on its element; chrome.css does the
// sizing from them, so a diagram's placeholder and the diagram agree and
// nothing waits on script.
const (
	// fitNaturalVar is the SVG's natural width, a length.
	fitNaturalVar = "--vellum-svg-natural"
	// fitEmsVar is the width up to which it may grow, as a number of ems
	// of the article's text (chrome.css multiplies by --vellum-text-size).
	fitEmsVar = "--vellum-svg-ems"
	// figureAttr marks the block that holds nothing but one fitted SVG
	// (figures.go), which chrome.css then lets leave the reading column.
	figureAttr = "data-vellum-figure"

	fitSVGExt = ".svg"
)

var (
	svgRootRe   = regexp.MustCompile(`(?i)<svg\b[^>]*>`)
	styleAttrRe = regexp.MustCompile(`(?i)(?:^|\s)style\s*=\s*"`)
)

// fitMemo is one remembered measurement. A fragment's is keyed by its
// content id and never changes; a file's is good while its stamp holds.
type fitMemo struct {
	fit   svgfit.Fit
	ok    bool
	mtime time.Time
	size  int64
}

func fitStyle(fit svgfit.Fit) string {
	return fitNaturalVar + ": " + strconv.FormatFloat(fit.Width, 'f', 2, 64) + "px; " +
		fitEmsVar + ": " + strconv.FormatFloat(fit.GrowEm(), 'f', 2, 64)
}

// addStyle puts decls first in tag's style attribute, adding the
// attribute when the tag has none.
func addStyle(tag, decls string) string {
	if loc := styleAttrRe.FindStringIndex(tag); loc != nil {
		return tag[:loc[1]] + decls + "; " + tag[loc[1]:]
	}
	end := strings.TrimRight(strings.TrimSuffix(tag, ">"), " ")
	end = strings.TrimSuffix(end, "/")
	return strings.TrimRight(end, " ") + ` style="` + decls + `">`
}

// diagramFit measures the rendered diagram id, whose markup is html.
func (s *Server) diagramFit(id, html string) (svgfit.Fit, bool) {
	key := "fragment\x00" + id
	if m, ok := s.fits.Load(key); ok {
		memo := m.(fitMemo)
		return memo.fit, memo.ok
	}
	fit, ok := svgfit.Measure([]byte(html))
	s.fits.Store(key, fitMemo{fit: fit, ok: ok})
	return fit, ok
}

// fitDiagram writes the sizing properties on a rendered diagram's root
// element. Markup that is not a measurable SVG (math, a diagram with no
// viewBox) is returned as it came.
func (s *Server) fitDiagram(id, html string) string {
	loc := svgRootRe.FindStringIndex(html)
	if loc == nil || strings.TrimSpace(html[:loc[0]]) != "" {
		return html
	}
	fit, ok := s.diagramFit(id, html)
	if !ok {
		return html
	}
	return html[:loc[0]] + addStyle(html[loc[0]:loc[1]], fitStyle(fit)) + html[loc[1]:]
}

// cachedDiagramFit is diagramFit for a diagram already in the fragment
// cache; ok is false when it is not there or cannot be measured.
func (s *Server) cachedDiagramFit(cacheRoot, id string) (svgfit.Fit, bool) {
	if m, ok := s.fits.Load("fragment\x00" + id); ok {
		memo := m.(fitMemo)
		return memo.fit, memo.ok
	}
	html, err := os.ReadFile(fragmentCachePath(cacheRoot, id))
	if err != nil || len(html) == 0 {
		return svgfit.Fit{}, false
	}
	return s.diagramFit(id, string(html))
}

// fileFit measures the SVG file at abs, a path inside a served folder.
func (s *Server) fileFit(abs string) (svgfit.Fit, bool) {
	f, err := s.openServed(abs)
	if err != nil {
		return svgfit.Fit{}, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() || info.Size() > svgfit.MaxBytes {
		return svgfit.Fit{}, false
	}
	key := "file\x00" + abs
	if m, ok := s.fits.Load(key); ok {
		if memo := m.(fitMemo); memo.mtime.Equal(info.ModTime()) && memo.size == info.Size() {
			return memo.fit, memo.ok
		}
	}
	// The limit holds even if the file grew after the stat.
	data, err := io.ReadAll(io.LimitReader(f, svgfit.MaxBytes+1))
	if err != nil {
		return svgfit.Fit{}, false
	}
	fit, ok := svgfit.Measure(data)
	s.fits.Store(key, fitMemo{fit: fit, ok: ok, mtime: info.ModTime(), size: info.Size()})
	return fit, ok
}
