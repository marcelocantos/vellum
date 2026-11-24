// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The viewer serves only files inside the folders it serves: Server.Roots,
// plus the folder of every Markdown document the user opened from outside
// the browser page (vellum view, the Finder handler, a typed URL or
// bookmark). A page cannot open a folder: its links, images and fetches
// reach only what is already served.
//
// Every route that takes a path resolves it (symlinks and "..") and
// compares the result with the resolved roots, then opens it through
// os.Root, which refuses to leave the root even if the path changes on
// disk after the check.

// errOutsideRoots is shown to the reader as the body of a 403, so it says
// what makes a folder served. A reload repeats the request as it was first
// made, so reloading a page reached by a link is still a page's request.
var errOutsideRoots = errors.New("outside the folders this viewer serves. " +
	"A folder is served once a Markdown document in it is opened from outside any page: " +
	"enter the document's URL in the address bar, run `open <url>`, or run `vellum view <file>`. " +
	"Following a link does not count, nor does reloading a page that was reached by a link")

const (
	// rootsFileName persists granted roots in the cache directory, so a
	// tab that was open before the server restarted keeps working.
	rootsFileName = "roots.json"
	// rootsCap bounds the persisted grants, most recent first.
	rootsCap = 64
	// gitMarker names the entry that marks a git work tree's top
	// (a directory, or a file in a linked worktree).
	gitMarker = ".git"
)

// servedRoot is one folder (or, for a document opened from the home
// folder or a filesystem root, one file) the viewer serves. Path is
// fully resolved.
type servedRoot struct {
	Path string `json:"path"`
	File bool   `json:"file,omitempty"`
}

// contains reports whether resolved (a fully resolved absolute path) is
// this root or lies beneath it.
func (r servedRoot) contains(resolved string) bool {
	if resolved == r.Path {
		return true
	}
	if r.File {
		return false
	}
	rel, err := filepath.Rel(r.Path, resolved)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// openedByUser reports whether r came from outside any page: the browser
// sends Sec-Fetch-Site "none" for a typed URL, a bookmark, or a link
// opened from another application, and clients that are not browser
// pages (curl, vellum's own probes) send no fetch metadata at all. A
// request a page makes always says same-origin, same-site or cross-site.
func openedByUser(r *http.Request) bool {
	for _, site := range r.Header.Values(secFetchSiteHeader) {
		if site != secFetchSiteNone {
			return false
		}
	}
	return true
}

// resolvePath resolves symlinks in p, an absolute cleaned path. A path
// that does not exist yet (a waiting page, a watched file) resolves its
// deepest existing ancestor and keeps the rest as written.
func resolvePath(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(p)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(p)
	if parent == p {
		return "", err
	}
	head, err := resolvePath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(head, filepath.Base(p)), nil
}

// rootFor is the root a user-opened document grants: the top of the git
// work tree it is in, or else its own folder. The search stops below the
// home folder, and a document directly in the home folder or a filesystem
// root grants only itself, never everything beneath it.
//
// The folder is the one the document's name is in, not its symlink
// target's: opening a link that points out of its folder serves the
// link's folder, and the request for the target is then refused.
func rootFor(doc string) (servedRoot, error) {
	dir, err := resolvePath(filepath.Dir(doc))
	if err != nil {
		return servedRoot{}, err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return servedRoot{}, fmt.Errorf("folder of %s: not a directory", doc)
	}
	home := ""
	if h, err := os.UserHomeDir(); err == nil {
		if rh, err := filepath.EvalSymlinks(h); err == nil {
			home = rh
		}
	}
	isTop := func(d string) bool { return d == home || filepath.Dir(d) == d }
	if isTop(dir) {
		return servedRoot{Path: filepath.Join(dir, filepath.Base(doc)), File: true}, nil
	}
	for d := dir; !isTop(d); d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, gitMarker)); err == nil {
			return servedRoot{Path: d}, nil
		}
	}
	return servedRoot{Path: dir}, nil
}

// servedRoots returns Server.Roots (resolved) and the granted roots.
func (s *Server) servedRoots() []servedRoot {
	var out []servedRoot
	for _, r := range s.Roots {
		abs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			continue
		}
		out = append(out, servedRoot{Path: resolved})
	}
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	s.loadGrantsLocked()
	return append(out, s.granted...)
}

func (s *Server) loadGrantsLocked() {
	if s.grantsLoaded {
		return
	}
	s.grantsLoaded = true
	root, err := s.cacheRoot()
	if err != nil {
		return
	}
	b, err := os.ReadFile(filepath.Join(root, rootsFileName))
	if err != nil {
		return
	}
	var saved []servedRoot
	if json.Unmarshal(b, &saved) != nil {
		return
	}
	for _, r := range saved {
		if filepath.IsAbs(r.Path) && filepath.Clean(r.Path) == r.Path {
			s.granted = append(s.granted, r)
		}
	}
}

// grant serves the root of doc from now on (see rootFor) and persists it.
func (s *Server) grant(doc string) error {
	root, err := rootFor(doc)
	if err != nil {
		return err
	}
	s.rootsMu.Lock()
	defer s.rootsMu.Unlock()
	s.loadGrantsLocked()
	next := []servedRoot{root}
	for _, r := range s.granted {
		if r != root && len(next) < rootsCap {
			next = append(next, r)
		}
	}
	s.granted = next
	cache, err := s.cacheRoot()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(cache, rootsFileName+".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(cache, rootsFileName))
}

// confine resolves p and returns the served root it lies in and the
// resolved path relative to that root's directory. errOutsideRoots means
// no root contains it; a path that cannot be resolved counts as outside.
func (s *Server) confine(p string) (dir, rel string, err error) {
	resolved, err := resolvePath(p)
	if err != nil {
		return "", "", errOutsideRoots
	}
	for _, r := range s.servedRoots() {
		if !r.contains(resolved) {
			continue
		}
		dir = r.Path
		if r.File {
			dir = filepath.Dir(r.Path)
		}
		rel, err := filepath.Rel(dir, resolved)
		if err != nil {
			continue
		}
		return dir, rel, nil
	}
	return "", "", errOutsideRoots
}

// dirServed reports whether dir lies in a served folder (not merely a
// served file in it), so its listing reveals only served names.
func (s *Server) dirServed(dir string) bool {
	resolved, err := resolvePath(dir)
	if err != nil {
		return false
	}
	for _, r := range s.servedRoots() {
		if !r.File && r.contains(resolved) {
			return true
		}
	}
	return false
}

// checkServed is confine for routes that only need the verdict.
func (s *Server) checkServed(p string) error {
	_, _, err := s.confine(p)
	return err
}

// openServed opens p for reading, inside a served root. os.Root walks
// the path itself, so a component swapped for a symlink after confine
// cannot lead outside the root.
func (s *Server) openServed(p string) (*os.File, error) {
	dir, rel, err := s.confine(p)
	if err != nil {
		return nil, err
	}
	if s.beforeOpen != nil {
		s.beforeOpen(p)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		return nil, s.servedError(p, err)
	}
	return f, nil
}

// servedError reports a failed root operation on p: errOutsideRoots when
// p now resolves outside every root (os.Root refused to follow it), the
// operation's own error otherwise.
func (s *Server) servedError(p string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if s.checkServed(p) != nil {
		return errOutsideRoots
	}
	var pe *fs.PathError
	if errors.As(err, &pe) && strings.Contains(pe.Err.Error(), "escapes") {
		return errOutsideRoots
	}
	return err
}

// statServed stats p inside a served root.
func (s *Server) statServed(p string) (os.FileInfo, error) {
	f, err := s.openServed(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// readServed returns p's bytes and the stat of the file they came from.
func (s *Server) readServed(p string) ([]byte, os.FileInfo, error) {
	f, err := s.openServed(p)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return nil, nil, errIsDirectory
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}
	return data, info, nil
}

// writeServed replaces p's contents atomically inside its served root:
// a temporary file beside the target, then a rename, both through
// os.Root. A symlinked path replaces its target (the link stays a link),
// and the file keeps its mode. The returned info is the written file's
// own (rename keeps its mtime and size), so a stamp made from it names
// this write even if another lands right after it.
func (s *Server) writeServed(p string, data []byte) (os.FileInfo, error) {
	if s.WriteFile != nil {
		if err := s.checkServed(p); err != nil {
			return nil, err
		}
		if err := s.WriteFile(p, data); err != nil {
			return nil, err
		}
		return s.statServed(p)
	}
	dir, rel, err := s.confine(p)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	mode := os.FileMode(0o644)
	if info, err := root.Stat(rel); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, s.servedError(p, err)
	}
	var nonce [8]byte
	_, _ = rand.Read(nonce[:])
	tmp := filepath.Join(filepath.Dir(rel), ".vellum-write-"+hex.EncodeToString(nonce[:])+".tmp")
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, s.servedError(p, err)
	}
	if err := f.Chmod(mode); err != nil && runtime.GOOS != "windows" {
		_ = f.Close()
		_ = root.Remove(tmp)
		return nil, err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = root.Remove(tmp)
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		_ = root.Remove(tmp)
		return nil, err
	}
	if err := f.Close(); err != nil {
		_ = root.Remove(tmp)
		return nil, err
	}
	if err := root.Rename(tmp, rel); err != nil {
		_ = root.Remove(tmp)
		return nil, s.servedError(p, err)
	}
	return info, nil
}

// servedStatus writes the HTTP error for a failed path operation and
// reports whether it did.
func servedStatus(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errOutsideRoots):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, fs.ErrNotExist):
		http.NotFound(w, r)
	case errors.Is(err, errIsDirectory):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	return true
}
