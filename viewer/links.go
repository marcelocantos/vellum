// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// hrefAttrRe matches href="..." / href='...' in HTML (attribute order flexible
// enough for goldmark output). Captures the quote and the raw target.
var hrefAttrRe = regexp.MustCompile(`(?i)(\bhref\s*=\s*)(["'])([^"']*)(["'])`)

// rewriteMarkdownHrefs rewrites relative .md / .markdown hrefs in html so they
// point at same-origin view-server URLs for the resolved absolute path.
// Fragments (#section) are preserved. Absolute http(s)/mailto/etc. links and
// non-Markdown targets are left unchanged.
func rewriteMarkdownHrefs(html, sourcePath, origin string) string {
	dir := filepath.Dir(sourcePath)
	origin = strings.TrimRight(origin, "/")
	return hrefAttrRe.ReplaceAllStringFunc(html, func(m string) string {
		sub := hrefAttrRe.FindStringSubmatch(m)
		if len(sub) != 5 {
			return m
		}
		prefix, q, raw, q2 := sub[1], sub[2], sub[3], sub[4]
		target, frag, ok := markdownLinkTarget(raw)
		if !ok {
			return m
		}
		var abs string
		if filepath.IsAbs(target) {
			abs = filepath.Clean(target)
		} else {
			abs = filepath.Clean(filepath.Join(dir, target))
		}
		newHref := origin + pathURL(abs)
		if frag != "" {
			newHref += "#" + frag
		}
		return prefix + q + newHref + q2
	})
}

// markdownLinkTarget reports whether raw is a Markdown file link (relative or
// absolute path, optional fragment). External schemes are rejected.
func markdownLinkTarget(raw string) (path, frag string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return "", "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	if u.Scheme != "" && u.Scheme != "file" {
		return "", "", false
	}
	pathPart := raw
	if u.Scheme == "file" {
		pathPart = u.Path
		frag = u.Fragment
	} else {
		pathPart, frag = splitFragment(raw)
	}
	pathPart = strings.TrimSpace(pathPart)
	if pathPart == "" {
		return "", "", false
	}
	ext := strings.ToLower(filepath.Ext(pathPart))
	if ext != ".md" && ext != ".markdown" {
		return "", "", false
	}
	return pathPart, frag, true
}

func splitFragment(s string) (path, frag string) {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

var baseHrefRe = regexp.MustCompile(`(?i)(<base\s+[^>]*\bhref\s*=\s*)(["'])([^"']*)(["'])`)

// ensureBaseHref sets or replaces the first <base href> in html.
func ensureBaseHref(html, base string) string {
	if baseHrefRe.MatchString(html) {
		replaced := false
		return baseHrefRe.ReplaceAllStringFunc(html, func(m string) string {
			if replaced {
				return m
			}
			replaced = true
			sub := baseHrefRe.FindStringSubmatch(m)
			if len(sub) != 5 {
				return m
			}
			return sub[1] + sub[2] + base + sub[4]
		})
	}
	lower := strings.ToLower(html)
	if i := strings.Index(lower, "<head>"); i >= 0 {
		ins := i + len("<head>")
		return html[:ins] + `<base href="` + base + `">` + html[ins:]
	}
	return `<base href="` + base + `">` + html
}

// pathURL encodes an absolute filesystem path as a URL path (leading slash,
// each segment PathEscape'd) so it can sit under the view-server origin.
func pathURL(absPath string) string {
	absPath = filepath.Clean(absPath)
	if absPath == "" {
		return "/"
	}
	// filepath on Windows uses `\`; HTTP paths always use `/`.
	slash := filepath.ToSlash(absPath)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	parts := strings.Split(slash, "/")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}
