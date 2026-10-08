// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/marcelocantos/vellum/embed"
)

// katexHead returns the <head> snippet that styles KaTeX output. The
// stylesheet is never fetched from the network: href names where the
// consumer finds vellum's own copy. The view server passes its
// /_vellum/katex/ route; every other sink links the copy that
// KaTeXAssetDir lays out under the user cache directory, which
// WeasyPrint, Prince and a browser opening the file all read as file://.
func katexHead(href string) (string, error) {
	if href == "" {
		dir, err := KaTeXAssetDir()
		if err != nil {
			return "", err
		}
		u := url.URL{Scheme: "file", Path: filepath.Join(dir, embed.KaTeXCSSName)}
		href = u.String()
	}
	return `<link rel="stylesheet" href="` + href + `">`, nil
}

var katexAssetDir = sync.OnceValues(materialiseKaTeX)

// KaTeXAssetDir returns the directory holding vellum's copy of the KaTeX
// stylesheet and fonts for this binary's KaTeX version, writing it under
// os.UserCacheDir (vellum/katex/<version>) on first use. The directory is
// complete once it exists: it is built beside its final name and renamed
// into place, so a concurrent vellum never sees a partial copy.
func KaTeXAssetDir() (string, error) {
	return katexAssetDir()
}

func materialiseKaTeX() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("katex assets: resolving user cache dir: %w", err)
	}
	root := filepath.Join(base, "vellum", "katex")
	final := filepath.Join(root, embed.KaTeXVersion)
	if _, err := os.Stat(filepath.Join(final, embed.KaTeXCSSName)); err == nil {
		return final, nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("katex assets: %w", err)
	}
	tmp, err := os.MkdirTemp(root, "."+embed.KaTeXVersion+".")
	if err != nil {
		return "", fmt.Errorf("katex assets: %w", err)
	}
	defer os.RemoveAll(tmp)
	if err := os.CopyFS(tmp, embed.KaTeX()); err != nil {
		return "", fmt.Errorf("katex assets: copying: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		// Another process finished first; its copy is identical.
		if _, statErr := os.Stat(filepath.Join(final, embed.KaTeXCSSName)); statErr == nil {
			return final, nil
		}
		return "", fmt.Errorf("katex assets: %w", err)
	}
	return final, nil
}

// katexFontRefs lists the font files the vendored stylesheet references
// (paths relative to the stylesheet), for the self-consistency test and
// the asset listing.
func katexFontRefs() ([]string, error) {
	var refs []string
	err := fs.WalkDir(embed.KaTeX(), embed.KaTeXFontsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			refs = append(refs, p)
		}
		return nil
	})
	return refs, err
}
