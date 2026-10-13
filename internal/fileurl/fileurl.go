// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package fileurl converts between filesystem paths and file:// URLs on
// both Unix and Windows. A Windows path C:\a b\c becomes
// file:///C:/a%20b/c, the form browsers, WeasyPrint and Prince accept;
// naive "file://"+path would put the drive letter in the host.
package fileurl

import (
	"net/url"
	"path/filepath"
	"strings"
)

// FromPath returns the file:// URL for an absolute path. A trailing
// separator on path is preserved, so directory base URLs still resolve
// relative references inside the directory.
func FromPath(path string) string {
	slash := filepath.ToSlash(path)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash // C:/x -> /C:/x
	}
	u := url.URL{Scheme: "file", Path: slash}
	return u.String()
}

// ToPath returns the filesystem path for a file:// URL, and false when
// ref is not a file URL.
func ToPath(ref string) (string, bool) {
	if !strings.HasPrefix(strings.ToLower(ref), "file://") {
		return "", false
	}
	u, err := url.Parse(ref)
	if err != nil {
		return filepath.FromSlash(strings.TrimPrefix(ref, "file://")), true
	}
	p := u.Path
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:] // /C:/x -> C:/x
	}
	return filepath.FromSlash(p), true
}
