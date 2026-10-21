// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"html"
	"net/http"
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

// anchorTagRe matches the start tag of an element whose href navigates
// (<a> in HTML or inline SVG, <area>). <use>, <link> and friends are
// deliberately excluded: their fragment references are same-document by
// definition and must stay as written.
var anchorTagRe = regexp.MustCompile(`(?is)<(?:a|area)\b[^>]*>`)

// tagHrefRe matches one href (or SVG xlink:href) attribute inside a start
// tag: double-quoted, single-quoted or unquoted.
var tagHrefRe = regexp.MustCompile(`(?i)(\s(?:xlink:)?href\s*=\s*)(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)

// rewriteFragmentHrefs prefixes every fragment-only link (href="#id")
// with docPath, the document's own URL path, so it resolves to the page
// itself rather than against <base href> (the source directory). docPath
// is a URL path ("/a/b%20c/note.md"); it is HTML-escaped into the
// attribute. The fragment is kept exactly as written: it is already in
// attribute form and matches the heading id the browser looks for.
func rewriteFragmentHrefs(page, docPath string) string {
	prefix := html.EscapeString(docPath)
	return anchorTagRe.ReplaceAllStringFunc(page, func(tag string) string {
		return tagHrefRe.ReplaceAllStringFunc(tag, func(attr string) string {
			sub := tagHrefRe.FindStringSubmatch(attr)
			if len(sub) != 5 {
				return attr
			}
			val, q := sub[2], `"`
			if sub[3] != "" {
				val, q = sub[3], `'`
			} else if sub[4] != "" {
				val = sub[4]
			}
			if !strings.HasPrefix(val, "#") {
				return attr
			}
			return sub[1] + q + prefix + val + q
		})
	})
}

// documentURLPath is the escaped path (and query, if any) the browser
// used for this page, so a rewritten fragment link differs from the
// current URL only in its fragment and the browser scrolls in place
// instead of reloading.
func documentURLPath(r *http.Request) string {
	p := r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		p += "?" + r.URL.RawQuery
	}
	return p
}
