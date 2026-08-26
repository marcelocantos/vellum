// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	_ "embed"
	"html"
	"path/filepath"
	"regexp"
	"strings"
)

// Chrome action paths on the view server (not filesystem paths).
const (
	ChromePrefix        = "/_vellum/"
	ChromePDFPath       = "/_vellum/pdf"
	ChromeClipboardPath = "/_vellum/clipboard"
	ChromeRevealPath    = "/_vellum/reveal"
)

//go:embed chrome.css
var chromeCSS string

//go:embed chrome.js
var chromeJS string

var (
	headCloseRe = regexp.MustCompile(`(?i)</head>`)
	bodyOpenRe  = regexp.MustCompile(`(?i)<body([^>]*)>`)
	bodyCloseRe = regexp.MustCompile(`(?i)</body>`)
	bodyClassRe = regexp.MustCompile(`(?i)(\bclass\s*=\s*)(["'])([^"']*)(["'])`)
)

// injectChrome wraps convert HTML with view-server chrome (toolbar, TOC
// shell, lightbox). The convert-cache file is left untouched; callers
// apply this per response.
func injectChrome(htmlDoc, sourcePath string) string {
	if htmlDoc == "" {
		return htmlDoc
	}
	htmlDoc = addBodyClass(htmlDoc, "vellum-view")
	if loc := headCloseRe.FindStringIndex(htmlDoc); loc != nil {
		htmlDoc = htmlDoc[:loc[0]] + chromeStyleTag() + htmlDoc[loc[0]:]
	}
	open := bodyOpenRe.FindStringIndex(htmlDoc)
	close := bodyCloseRe.FindStringIndex(htmlDoc)
	if open == nil || close == nil || close[0] < open[1] {
		return htmlDoc
	}
	inner := htmlDoc[open[1]:close[0]]
	wrapped := chromeOpen(sourcePath) + inner + chromeClose()
	htmlDoc = htmlDoc[:open[1]] + wrapped + htmlDoc[close[0]:]
	if loc := bodyCloseRe.FindStringIndex(htmlDoc); loc != nil {
		htmlDoc = htmlDoc[:loc[0]] + chromeScriptTag() + htmlDoc[loc[0]:]
	}
	return htmlDoc
}

func chromeStyleTag() string {
	return "<style>\n" + chromeCSS + "\n</style>\n"
}

func chromeScriptTag() string {
	return "<script>\n" + chromeJS + "\n</script>\n"
}

func chromeOpen(sourcePath string) string {
	name := filepath.Base(sourcePath)
	return `<div class="vellum-chrome" data-source="` + html.EscapeString(sourcePath) + `">` +
		`<aside class="vellum-toc" id="vellum-toc">` +
		`<div class="vellum-toc-header">` +
		`<span class="vellum-toc-title">Contents</span>` +
		`<div class="vellum-toc-actions">` +
		`<button type="button" data-vellum="toc-expand">Expand all</button>` +
		`<button type="button" data-vellum="toc-collapse">Collapse all</button>` +
		`</div></div>` +
		`<nav class="vellum-toc-nav" id="vellum-toc-nav" aria-label="Table of contents"></nav>` +
		`</aside>` +
		`<div class="vellum-main">` +
		`<header class="vellum-toolbar">` +
		`<button type="button" data-vellum="toc-toggle" title="Show or hide the table of contents">Contents</button>` +
		`<button type="button" data-vellum="pdf" title="Download a PDF of this document">Download PDF</button>` +
		`<button type="button" data-vellum="clipboard" title="Copy the rendered document to the clipboard">Copy</button>` +
		`<button type="button" data-vellum="reveal" title="Reveal the source file in Finder">Show in Finder</button>` +
		`<span class="vellum-docname">` + html.EscapeString(name) + `</span>` +
		`<span class="vellum-status" id="vellum-status" hidden></span>` +
		`</header>` +
		`<div class="vellum-scroll" id="vellum-scroll" tabindex="0" role="region" aria-label="Document">` +
		`<article class="vellum-article">`
}

func chromeClose() string {
	return `</article></div></div></div>` +
		`<div class="vellum-lightbox" id="vellum-lightbox" hidden role="dialog" aria-modal="true" aria-label="Figure viewer">` +
		`<div class="vellum-lightbox-bar">` +
		`<div>` +
		`<button type="button" data-vellum="lb-out" title="Zoom out">−</button> ` +
		`<button type="button" data-vellum="lb-in" title="Zoom in">+</button> ` +
		`<button type="button" data-vellum="lb-reset" title="Fit to window">Fit</button>` +
		`</div>` +
		`<span class="vellum-lightbox-hint" id="vellum-lightbox-hint">scroll to zoom · drag to pan · Esc to close</span>` +
		`<button type="button" data-vellum="lb-close">Close</button>` +
		`</div>` +
		`<div class="vellum-lightbox-stage" id="vellum-lightbox-stage">` +
		`<div class="vellum-lightbox-content" id="vellum-lightbox-content"></div>` +
		`</div></div>`
}

func addBodyClass(htmlDoc, class string) string {
	replaced := false
	return bodyOpenRe.ReplaceAllStringFunc(htmlDoc, func(m string) string {
		if replaced {
			return m
		}
		replaced = true
		if bodyClassRe.MatchString(m) {
			return bodyClassRe.ReplaceAllStringFunc(m, func(cm string) string {
				sub := bodyClassRe.FindStringSubmatch(cm)
				if len(sub) != 5 {
					return cm
				}
				existing := sub[3]
				if containsWord(existing, class) {
					return cm
				}
				if existing != "" {
					existing += " "
				}
				return sub[1] + sub[2] + existing + class + sub[4]
			})
		}
		return strings.Replace(m, ">", ` class="`+class+`">`, 1)
	})
}

func containsWord(s, word string) bool {
	for _, p := range strings.Fields(s) {
		if p == word {
			return true
		}
	}
	return false
}
