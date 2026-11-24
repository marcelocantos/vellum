// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"crypto/sha256"
	stdembed "embed"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Editor paths on the view server.
const (
	// ChromeEditPath reads (GET) and writes (PUT) a Markdown source.
	ChromeEditPath = "/_vellum/edit"
	// ChromeEditorPrefix serves the embedded CodeMirror bundle.
	ChromeEditorPrefix = "/_vellum/editor/"
	// EditorEntry is the bundle's entry module inside ChromeEditorPrefix.
	EditorEntry = "editor.js"

	// StampHeader carries a source stamp (see sourceStamp): the GET
	// response's current stamp, the PUT request's expected stamp, and the
	// PUT or 409 response's stamp after the write or conflict.
	StampHeader = "X-Vellum-Stamp"
	// StampParam carries the same stamp in the query, for a page whose
	// request headers do not reach the server (T3's plugin gateway
	// forwards only Accept and Content-Type).
	StampParam = "stamp"
	// StampLineParam, set to "1" on a GET, puts "stamp <stamp>\n" before
	// the source in the body, for a page that cannot read response headers.
	StampLineParam = "stampline"

	// maxEditBytes bounds a PUT body. Markdown this large is not edited
	// in a browser; the bound keeps a runaway client from filling memory.
	maxEditBytes = 32 << 20
)

// editorTree is the CodeMirror bundle built by editor/build.mjs.
//
//go:embed editor
var editorTree stdembed.FS

var (
	editorFS      fs.FS
	editorVersion string
)

func init() {
	sub, err := fs.Sub(editorTree, "editor")
	if err != nil {
		panic("viewer: editor bundle missing: " + err.Error())
	}
	editorFS = sub
	// Content hash of the whole tree: the entry URL carries it, so a new
	// build is never served from a stale browser cache.
	h := sha256.New()
	err = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(sub, p)
		if err != nil {
			return err
		}
		_, _ = io.WriteString(h, p+"\x00")
		_, _ = h.Write(data)
		return nil
	})
	if err != nil {
		panic("viewer: editor bundle unreadable: " + err.Error())
	}
	editorVersion = hex.EncodeToString(h.Sum(nil))[:12]
}

// editorEntryURL is the versioned URL chrome.js imports.
func editorEntryURL() string {
	return ChromeEditorPrefix + EditorEntry + "?v=" + editorVersion
}

func (s *Server) handleEditorAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, ChromeEditorPrefix)
	if name == "" || !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(editorFS, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Set explicitly: module scripts require a JavaScript MIME type, and
	// Windows' registry can map .js to text/plain.
	switch path.Ext(name) {
	case ".js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	// Chunks are content-hashed and the entry is requested with the tree
	// hash, so any URL that resolves names exactly one build.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	absPath, err := queryAbsPath(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !isMarkdownPath(absPath) {
		http.Error(w, "not a markdown path", http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	if r.Method == http.MethodGet {
		// The stamp is the open file's, taken before the read: a write in
		// between shows up as a conflict on save, never as a lost update.
		data, info, err := s.readServed(absPath)
		if servedStatus(w, r, err) {
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set(StampHeader, sourceStamp(info))
		if r.URL.Query().Get(StampLineParam) == "1" {
			_, _ = io.WriteString(w, "stamp "+sourceStamp(info)+"\n")
		}
		_, _ = w.Write(data)
		return
	}

	expected := requestStamp(r)
	if expected == "" {
		http.Error(w, "missing "+StampHeader, http.StatusPreconditionRequired)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxEditBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "document too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := s.statServed(absPath)
	if servedStatus(w, r, err) {
		return
	}
	// Optimistic concurrency: the client names the version it edited.
	// Anything else on disk (another editor, git checkout) wins until the
	// user chooses to overwrite, which they do by resending with the
	// stamp this conflict reports.
	if current := sourceStamp(info); current != expected {
		stampConflict(w, current, "the file changed on disk since it was opened")
		return
	}
	after, err := s.writeServed(absPath, body)
	if servedStatus(w, r, err) {
		return
	}
	stamp := sourceStamp(after)
	w.Header().Set(StampHeader, stamp)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "stamp "+stamp+"\n")
}

// requestStamp is the source stamp a write names: StampHeader, or
// StampParam when the header is absent.
func requestStamp(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(StampHeader)); v != "" {
		return v
	}
	return strings.TrimSpace(r.URL.Query().Get(StampParam))
}

// stampConflict answers 409 with the stamp the client should hold now, in
// StampHeader and as the body's last line ("stamp <stamp>"), so a client
// that cannot read response headers still learns it.
func stampConflict(w http.ResponseWriter, stamp, msg string) {
	w.Header().Set(StampHeader, stamp)
	http.Error(w, msg+"\nstamp "+stamp, http.StatusConflict)
}
