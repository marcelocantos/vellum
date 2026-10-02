// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/vellum/convert"
)

// ChromeFragmentPath serves one deferred article fragment (a Mermaid
// diagram or a math expression) by content id. Not a filesystem path.
const ChromeFragmentPath = "/_vellum/fragment"

// fragmentCachePrefix names rendered fragments in the cache directory.
// They are keyed by content id alone, so one file serves every document
// that contains the same diagram or expression.
const fragmentCachePrefix = "frag-"

// fragmentHeadBytes is how much of a cached SVG is read to recover its
// viewBox for the placeholder's aspect ratio.
const fragmentHeadBytes = 4096

var errFragmentNotFound = errors.New("fragment not in document")

var (
	imgTagRe       = regexp.MustCompile(`(?i)<(img)\b([^>]*)>`)
	loadingAttrRe  = regexp.MustCompile(`(?i)\bloading\s*=`)
	decodingAttrRe = regexp.MustCompile(`(?i)\bdecoding\s*=`)
	viewBoxRe      = regexp.MustCompile(`(?i)\bviewBox\s*=\s*["']\s*[-0-9.]+[\s,]+[-0-9.]+[\s,]+([0-9.]+)[\s,]+([0-9.]+)\s*["']`)
	maxWidthRe     = regexp.MustCompile(`(?i)max-width\s*:\s*([0-9.]+(?:px|em|rem|%))`)
	// mermaidPlaceholderRe matches the opening tag of a deferred Mermaid
	// placeholder as emitted by convert.Render with Options.Defer.
	mermaidPlaceholderRe = regexp.MustCompile(`<div class="vellum-deferred vellum-deferred-mermaid" data-vellum-fragment="([0-9a-f]{16})"[^>]*>`)
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

// reserveDiagramBoxes gives each Mermaid placeholder whose SVG is already
// in the fragment cache the diagram's aspect ratio and max-width, so a
// revisit does not shift the text below it when the SVG arrives. A
// placeholder with no cached render keeps the generic loading box.
func reserveDiagramBoxes(page, cacheRoot string) string {
	return mermaidPlaceholderRe.ReplaceAllStringFunc(page, func(m string) string {
		id := mermaidPlaceholderRe.FindStringSubmatch(m)[1]
		f, err := os.Open(fragmentCachePath(cacheRoot, id))
		if err != nil {
			return m
		}
		defer f.Close()
		head := make([]byte, fragmentHeadBytes)
		n, _ := io.ReadFull(f, head)
		root := string(head[:n])
		if end := strings.IndexByte(root, '>'); end >= 0 {
			root = root[:end+1]
		}
		var style []string
		if vb := viewBoxRe.FindStringSubmatch(root); len(vb) == 3 && vb[1] != "0" && vb[2] != "0" {
			style = append(style, "aspect-ratio: "+vb[1]+" / "+vb[2])
		}
		if mw := maxWidthRe.FindStringSubmatch(root); len(mw) == 2 {
			style = append(style, "max-width: "+mw[1])
		}
		if len(style) == 0 {
			return m
		}
		return strings.TrimSuffix(m, ">") + ` style="` + strings.Join(style, "; ") + `">`
	})
}

func fragmentCachePath(cacheRoot, id string) string {
	return filepath.Join(cacheRoot, fragmentCachePrefix+id+".html")
}

// cachedFragment returns the rendered fragment for id from the cache,
// touching its mtime so pruning sees it as recently used.
func cachedFragment(cacheRoot, id string, now time.Time) (string, bool) {
	p := fragmentCachePath(cacheRoot, id)
	b, err := os.ReadFile(p)
	if err != nil || len(b) == 0 {
		return "", false
	}
	_ = os.Chtimes(p, now, now)
	return string(b), true
}

// writeCachedFragment stores one rendered fragment atomically (temp file
// then rename), so a daemon that dies mid-write never leaves a partial
// fragment that a later request could serve as complete. Caching is
// best-effort: a failure here only costs a re-render next time.
func writeCachedFragment(cacheRoot, id, html string) {
	tmp, err := os.CreateTemp(cacheRoot, fragmentCachePrefix+id+".*.tmp")
	if err != nil {
		return
	}
	_, werr := tmp.WriteString(html)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	if err := os.Rename(tmp.Name(), fragmentCachePath(cacheRoot, id)); err != nil {
		_ = os.Remove(tmp.Name())
	}
}

// flightGroup collapses concurrent renders of the same key (one Mermaid
// id, or one document's math batch) into a single subprocess run whose
// result every waiter shares.
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

type flightCall struct {
	done chan struct{}
	res  map[string]convert.FragmentResult
	err  error
}

func (g *flightGroup) do(key string, fn func() (map[string]convert.FragmentResult, error)) (map[string]convert.FragmentResult, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*flightCall{}
	}
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-c.done
		return c.res, c.err
	}
	c := &flightCall{done: make(chan struct{})}
	g.calls[key] = c
	g.mu.Unlock()
	c.res, c.err = fn()
	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()
	close(c.done)
	return c.res, c.err
}

// renderFragmentsCached returns a result for every fragment in frags,
// serving cached renders and rendering the rest in one RenderFragments
// call under flight key. Successful renders are written to the cache;
// failures are not, so a retry re-runs the subprocess.
//
// Rendering is detached from ctx's cancellation: a browser that reloads
// mid-render (the watch fires on every save) would otherwise kill an
// mmdc run whose output the reloaded page is about to ask for again.
func (s *Server) renderFragmentsCached(ctx context.Context, key string, frags []convert.Fragment) (map[string]convert.FragmentResult, error) {
	cacheRoot, err := s.cacheRoot()
	if err != nil {
		return nil, err
	}
	now := s.now()
	results := make(map[string]convert.FragmentResult, len(frags))
	var misses []convert.Fragment
	for _, f := range frags {
		if _, done := results[f.ID]; done {
			continue
		}
		if html, ok := cachedFragment(cacheRoot, f.ID, now); ok {
			results[f.ID] = convert.FragmentResult{HTML: html}
			continue
		}
		results[f.ID] = convert.FragmentResult{}
		misses = append(misses, f)
	}
	if len(misses) == 0 {
		return results, nil
	}
	render := s.RenderFragments
	if render == nil {
		render = convert.RenderFragments
	}
	rendered, err := s.flights.do(key, func() (map[string]convert.FragmentResult, error) {
		// A flight that just landed may have filled the cache for us.
		var todo []convert.Fragment
		out := map[string]convert.FragmentResult{}
		for _, f := range misses {
			if html, ok := cachedFragment(cacheRoot, f.ID, now); ok {
				out[f.ID] = convert.FragmentResult{HTML: html}
				continue
			}
			todo = append(todo, f)
		}
		if len(todo) == 0 {
			return out, nil
		}
		s.FragmentRenderCount.Add(int64(len(todo)))
		fresh, err := render(context.WithoutCancel(ctx), todo)
		for id, r := range fresh {
			if r.Err == nil && r.HTML != "" {
				writeCachedFragment(cacheRoot, id, r.HTML)
			}
			out[id] = r
		}
		return out, err
	})
	for id, r := range rendered {
		if _, wanted := results[id]; wanted {
			results[id] = r
		}
	}
	return results, err
}

// fragmentHTML renders (or fetches from cache) the fragment id of the
// document at absPath. The document source is re-scanned to find the
// fragment, so an id that no longer appears (the file changed under the
// page) is errFragmentNotFound. Math fragments are rendered a document at
// a time, because every expression costs the same single node start.
func (s *Server) fragmentHTML(ctx context.Context, absPath, id string) (string, error) {
	cacheRoot, err := s.cacheRoot()
	if err != nil {
		return "", err
	}
	if html, ok := cachedFragment(cacheRoot, id, s.now()); ok {
		return html, nil
	}
	src, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	frags := convert.ExtractFragments(src, s.convertOptions())
	var want *convert.Fragment
	var math []convert.Fragment
	for i := range frags {
		if frags[i].Kind == convert.FragmentMath {
			math = append(math, frags[i])
		}
		if frags[i].ID == id && want == nil {
			want = &frags[i]
		}
	}
	if want == nil {
		return "", errFragmentNotFound
	}
	batch, key := []convert.Fragment{*want}, "fragment\x00"+id
	if want.Kind == convert.FragmentMath {
		batch, key = math, "math\x00"+absPath
	}
	results, err := s.renderFragmentsCached(ctx, key, batch)
	if err != nil {
		return "", err
	}
	res, ok := results[id]
	if !ok {
		return "", errFragmentNotFound
	}
	if res.Err != nil {
		return "", res.Err
	}
	return res.HTML, nil
}

// completeHTML is the fully rendered document: the cached shell with
// every deferred fragment resolved (from the fragment cache or a fresh
// render). It is what the clipboard carries. soft lists fragments that
// failed to render and were replaced by their source fallback.
func (s *Server) completeHTML(ctx context.Context, absPath string) (html string, soft []string, err error) {
	shell, err := s.cachedHTML(ctx, absPath)
	if err != nil {
		return "", nil, err
	}
	src, err := os.ReadFile(absPath)
	if err != nil {
		return "", nil, err
	}
	frags := convert.ExtractFragments(src, s.convertOptions())
	results, err := s.renderFragmentsCached(ctx, "document\x00"+absPath, frags)
	if err != nil {
		return "", nil, err
	}
	html, soft = convert.ResolveFragments(string(shell), results)
	return html, soft, nil
}

// handleFragment serves one deferred fragment for ?path=…&id=…. Ids are
// content hashes, so a successful response is immutable; a render
// failure is a 500 with the subprocess error as text and is never
// cached, so the browser's retry reaches the server again.
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
	if !convert.FragmentIDRe.MatchString(id) {
		http.Error(w, "missing or malformed fragment id", http.StatusBadRequest)
		return
	}
	html, err := s.fragmentHTML(r.Context(), absPath, id)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		switch {
		case os.IsNotExist(err), errors.Is(err, errFragmentNotFound):
			http.NotFound(w, r)
		default:
			// mmdc prints its complaint over several lines; the page shows
			// one line, so collapse the whitespace here.
			http.Error(w, "render failed: "+strings.Join(strings.Fields(err.Error()), " "), http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = io.WriteString(w, html)
}
