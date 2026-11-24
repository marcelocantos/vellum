// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"html"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	homeRecentName = "mru.json"
	homeRecentCap  = 20
	homeTreeDepth  = 4
	homeTreeFiles  = 200
)

type recentDoc struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

// noteRecent records a successfully rendered Markdown file, newest first.
func (s *Server) noteRecent(absPath string) error {
	absPath = filepath.Clean(absPath)
	title := s.recentTitle(absPath)
	s.mruMu.Lock()
	defer s.mruMu.Unlock()
	root, err := s.cacheRoot()
	if err != nil {
		return err
	}
	docs, err := readRecent(root)
	if err != nil {
		docs = nil
	}
	next := make([]recentDoc, 0, len(docs)+1)
	next = append(next, recentDoc{Path: absPath, Title: title})
	for _, doc := range docs {
		if doc.Path == absPath || len(next) >= homeRecentCap {
			continue
		}
		next = append(next, doc)
	}
	return writeRecent(root, next)
}

func (s *Server) recentDocs() []recentDoc {
	s.mruMu.Lock()
	defer s.mruMu.Unlock()
	root, err := s.cacheRoot()
	if err != nil {
		return nil
	}
	docs, err := readRecent(root)
	if err != nil {
		return nil
	}
	return docs
}

func readRecent(root string) ([]recentDoc, error) {
	b, err := os.ReadFile(filepath.Join(root, homeRecentName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var docs []recentDoc
	if err := json.Unmarshal(b, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func writeRecent(root string, docs []recentDoc) error {
	b, err := json.MarshalIndent(docs, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := filepath.Join(root, homeRecentName+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(root, homeRecentName))
}

func (s *Server) recentTitle(absPath string) string {
	fallback := strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))
	f, err := s.openServed(absPath)
	if err != nil {
		return fallback
	}
	defer f.Close()
	r := bufio.NewReader(io.LimitReader(f, 8192))
	for {
		line, err := r.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			if title := strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")); title != "" {
				return title
			}
		}
		if err != nil {
			break
		}
	}
	return fallback
}

func (s *Server) serveHome(w http.ResponseWriter, r *http.Request) {
	// The list was written by earlier runs, and folders stop being served
	// when their grant ages out: show only what this server would serve,
	// and only what is still on disk (a deleted document, or a folder with
	// no Markdown left, would be a dead row).
	var docs []recentDoc
	for _, doc := range s.recentDocs() {
		if s.checkServed(doc.Path) != nil {
			continue
		}
		if info, err := os.Stat(doc.Path); err != nil || info.IsDir() {
			continue
		}
		docs = append(docs, doc)
	}
	var dirs []string
	for _, dir := range recentRoots(docs) {
		if s.dirServed(dir) && dirHasMarkdown(dir, homeTreeDepth) {
			dirs = append(dirs, dir)
		}
	}
	page := renderHome(docs, dirs)
	w.Header().Set(cspHeader, homeCSP)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = io.WriteString(w, page)
}

//go:embed home.css
var homeCSS string

// homePath shows a path the way a shell would, with the home folder as ~.
func homePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}

// renderHome lists docs (recent, newest first) and the Markdown tree under
// each of dirs.
func renderHome(docs []recentDoc, dirs []string) string {
	var b bytes.Buffer
	b.WriteString(`<!DOCTYPE html><html lang="en" data-vellum-theme="system"><head><meta charset="utf-8">`)
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1">`)
	b.WriteString(`<title>Vellum</title>`)
	b.WriteString(`<link rel="icon" href="` + html.EscapeString(ChromeFaviconPath) + `" type="image/svg+xml">`)
	b.WriteString("<style>\n" + themeCSS + "\n" + homeCSS + "</style></head><body><main>")
	b.WriteString(`<header><img src="` + html.EscapeString(ChromeFaviconPath) + `" width="26" height="26" alt=""><h1>Vellum</h1></header>`)
	b.WriteString(`<section id="recent"><h2>Recent</h2>`)
	if len(docs) == 0 {
		b.WriteString(`<p>No documents yet. Open a Markdown file with <code>vellum view</code>.</p>`)
	} else {
		b.WriteString(`<ul class="docs">`)
		for _, doc := range docs {
			title := doc.Title
			if title == "" {
				title = filepath.Base(doc.Path)
			}
			b.WriteString(`<li><a href="`)
			b.WriteString(html.EscapeString(pathURL(doc.Path)))
			b.WriteString(`"><span class="title">`)
			b.WriteString(html.EscapeString(title))
			b.WriteString(`</span><span class="path">`)
			b.WriteString(html.EscapeString(homePath(doc.Path)))
			b.WriteString(`</span></a></li>`)
		}
		b.WriteString(`</ul>`)
	}
	b.WriteString(`</section><section id="files"><h2>Files</h2>`)
	roots := dirs
	if len(roots) == 0 {
		b.WriteString(`<p>No folders yet.</p>`)
	}
	remaining := homeTreeFiles
	for _, root := range roots {
		b.WriteString(`<div class="folder"><h3>`)
		b.WriteString(html.EscapeString(homePath(root)))
		b.WriteString(`</h3><div class="tree">`)
		writeTree(&b, root, root, 0, &remaining)
		b.WriteString(`</div></div>`)
	}
	b.WriteString(`</section></main></body></html>`)
	return b.String()
}

func recentRoots(docs []recentDoc) []string {
	var roots []string
	seen := map[string]bool{}
	for _, doc := range docs {
		if doc.Path == "" {
			continue
		}
		dir := filepath.Clean(filepath.Dir(doc.Path))
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		roots = append(roots, dir)
	}
	return roots
}

func writeTree(b *bytes.Buffer, root, dir string, depth int, remaining *int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	b.WriteString(`<ul>`)
	for _, ent := range entries {
		name := ent.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := ent.Info()
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if ent.IsDir() {
			if depth+1 >= homeTreeDepth {
				continue
			}
			if !dirHasMarkdown(path, homeTreeDepth-depth-1) {
				continue
			}
			b.WriteString(`<li><span class="dir">`)
			b.WriteString(html.EscapeString(name))
			b.WriteString(`</span>`)
			writeTree(b, root, path, depth+1, remaining)
			b.WriteString(`</li>`)
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".md" && ext != ".markdown" {
			continue
		}
		if *remaining <= 0 {
			continue
		}
		*remaining--
		b.WriteString(`<li><a href="`)
		b.WriteString(html.EscapeString(pathURL(path)))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(name))
		b.WriteString(`</a></li>`)
	}
	b.WriteString(`</ul>`)
}

func dirHasMarkdown(dir string, depthLeft int) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			if found {
				return fs.SkipAll
			}
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != dir && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
				return fs.SkipDir
			}
			rel, relErr := filepath.Rel(dir, path)
			if relErr == nil && rel != "." && strings.Count(rel, string(filepath.Separator))+1 > depthLeft {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".md" || ext == ".markdown" {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}
