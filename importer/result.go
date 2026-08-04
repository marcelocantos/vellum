// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Result is Markdown plus any extracted media files.
type Result struct {
	Markdown string
	MediaDir string
	Assets   []string // absolute paths of media files under MediaDir
}

var (
	mdImageRe = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)
	htmlSrcRe = regexp.MustCompile(`(?i)(<img\b[^>]*\bsrc\s*=\s*)(["'])([^"']+)(["'])`)
)

// AbsolutizeMedia rewrites relative image targets in md so they resolve
// under mediaDir (or as absolute paths already). Returns the rewritten
// Markdown and the list of asset paths that exist on disk.
func AbsolutizeMedia(md, mediaDir string) (string, []string, error) {
	if mediaDir == "" {
		return md, nil, nil
	}
	absMedia, err := filepath.Abs(mediaDir)
	if err != nil {
		return md, nil, err
	}

	seen := map[string]struct{}{}
	var assets []string
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			seen[p] = struct{}{}
			assets = append(assets, p)
		}
	}

	resolve := func(ref string) string {
		ref = strings.TrimSpace(ref)
		if ref == "" || strings.HasPrefix(ref, "data:") {
			return ref
		}
		// Strip optional title: url "title"
		if i := strings.IndexAny(ref, " \t"); i >= 0 {
			ref = ref[:i]
		}
		ref = strings.Trim(ref, `"'`)
		if u, err := url.Parse(ref); err == nil && u.Scheme != "" && u.Scheme != "file" {
			return ref // http(s) etc.
		}
		if strings.HasPrefix(ref, "file://") {
			p := strings.TrimPrefix(ref, "file://")
			add(p)
			return p
		}
		var p string
		if filepath.IsAbs(ref) {
			p = ref
		} else {
			// Pandoc extract-media paths are relative to CWD or include the
			// media dir prefix; try mediaDir join first, then mediaDir/ref.
			cand := []string{
				filepath.Join(absMedia, ref),
				filepath.Join(absMedia, filepath.Base(ref)),
				ref,
			}
			// Also try stripping a leading media/ component under absMedia.
			for _, c := range cand {
				if ap, err := filepath.Abs(c); err == nil {
					if st, err := os.Stat(ap); err == nil && !st.IsDir() {
						p = ap
						break
					}
				}
			}
			if p == "" {
				p, _ = filepath.Abs(filepath.Join(absMedia, ref))
			}
		}
		add(p)
		return p
	}

	out := mdImageRe.ReplaceAllStringFunc(md, func(m string) string {
		sub := mdImageRe.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		inner := sub[1]
		// Preserve optional title after the path.
		pathPart := inner
		title := ""
		if i := strings.IndexAny(inner, " \t"); i >= 0 {
			pathPart = strings.TrimSpace(inner[:i])
			title = inner[i:]
		}
		np := resolve(pathPart)
		if title != "" {
			return strings.Replace(m, inner, np+title, 1)
		}
		return strings.Replace(m, pathPart, np, 1)
	})

	out = htmlSrcRe.ReplaceAllStringFunc(out, func(m string) string {
		sub := htmlSrcRe.FindStringSubmatch(m)
		if len(sub) < 5 {
			return m
		}
		np := resolve(sub[3])
		return sub[1] + sub[2] + np + sub[4]
	})

	// Also collect any files under mediaDir not referenced (page renders).
	_ = filepath.Walk(absMedia, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
			add(path)
		}
		return nil
	})

	return out, assets, nil
}

// CollectAssets lists image-like files under mediaDir.
func CollectAssets(mediaDir string) ([]string, error) {
	if mediaDir == "" {
		return nil, nil
	}
	abs, err := filepath.Abs(mediaDir)
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// EnsureMediaDir returns mediaDir or allocates a cache bundle for key.
func EnsureMediaDir(mediaDir, cacheKey string) (string, error) {
	if mediaDir != "" {
		if err := os.MkdirAll(mediaDir, 0o755); err != nil {
			return "", err
		}
		return filepath.Abs(mediaDir)
	}
	if cacheKey == "" {
		return "", fmt.Errorf("importer: mediaDir or cacheKey required")
	}
	return BundleDir(cacheKey)
}
