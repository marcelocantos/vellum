// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"net/http"
	"sync"
)

// ChromeFileManagerIconPath serves the icon on the reveal button: the
// platform file manager's own icon where the OS provides one as a file
// (Finder on macOS), else folderIconSVG.
const ChromeFileManagerIconPath = "/_vellum/file-manager-icon"

// folderIconSVG is the fallback reveal icon, drawn like the toolbar's
// other line icons.
const folderIconSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#54aeff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg>`

var (
	fileManagerIconOnce sync.Once
	fileManagerIconData []byte
	fileManagerIconType string
)

func fileManagerIcon() ([]byte, string) {
	fileManagerIconOnce.Do(func() {
		if data, ok := platformFileManagerIcon(); ok {
			fileManagerIconData, fileManagerIconType = data, "image/png"
			return
		}
		fileManagerIconData, fileManagerIconType = []byte(folderIconSVG), "image/svg+xml; charset=utf-8"
	})
	return fileManagerIconData, fileManagerIconType
}

func (s *Server) handleFileManagerIcon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, ctype := fileManagerIcon()
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}
