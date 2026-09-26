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
	ChromePrefix         = "/_vellum/"
	ChromePDFPath        = "/_vellum/pdf"
	ChromeClipboardPath  = "/_vellum/clipboard"
	ChromeRevealPath     = "/_vellum/reveal"
	ChromeWatchPath      = "/_vellum/watch"
	ChromeTaskTogglePath = "/_vellum/task-toggle"
	ChromeFaviconPath    = "/_vellum/favicon.svg"
	FaviconICOPath       = "/favicon.ico"
)

//go:embed chrome.css
var chromeCSS string

//go:embed chrome.js
var chromeJS string

//go:embed favicon.svg
var faviconSVG string

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
	return injectChromeInto(htmlDoc, sourcePath, false)
}

func injectChromeInto(htmlDoc, sourcePath string, waiting bool) string {
	if htmlDoc == "" {
		return htmlDoc
	}
	htmlDoc = addBodyClass(htmlDoc, "vellum-view")
	if loc := headCloseRe.FindStringIndex(htmlDoc); loc != nil {
		htmlDoc = htmlDoc[:loc[0]] + chromeFaviconLink() + chromeThemeInitScript() + htmlDoc[loc[0]:]
	}
	open := bodyOpenRe.FindStringIndex(htmlDoc)
	close := bodyCloseRe.FindStringIndex(htmlDoc)
	if open == nil || close == nil || close[0] < open[1] {
		return htmlDoc
	}
	inner := annotateTaskCheckboxes(htmlDoc[open[1]:close[0]])
	wrapped := chromeOpen(sourcePath, waiting) + inner + chromeClose()
	htmlDoc = htmlDoc[:open[1]] + wrapped + htmlDoc[close[0]:]
	if loc := headCloseRe.FindStringIndex(htmlDoc); loc != nil {
		htmlDoc = htmlDoc[:loc[0]] + chromeStyleTag() + htmlDoc[loc[0]:]
	}
	if loc := bodyCloseRe.FindStringIndex(htmlDoc); loc != nil {
		htmlDoc = htmlDoc[:loc[0]] + chromeScriptTag() + htmlDoc[loc[0]:]
	}
	return htmlDoc
}

func chromeStyleTag() string {
	return "<style>\n" + chromeCSS + "\n" + chromaDarkThemeCSS + "</style>\n"
}

func chromeFaviconLink() string {
	return `<link rel="icon" href="` + ChromeFaviconPath + `" type="image/svg+xml">` + "\n"
}

func chromeThemeInitScript() string {
	return `<script>(function(){try{var k='vellum-theme',t=localStorage.getItem(k),o=['dark','system','light'];document.documentElement.dataset.vellumTheme=o.indexOf(t)>=0?t:'system';}catch(e){document.documentElement.dataset.vellumTheme='system';}})();</script>` + "\n"
}

func chromeScriptTag() string {
	return "<script>\n" + chromeJS + "\n</script>\n"
}

func downloadIconSVG() string {
	return `<svg class="vellum-icon" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3v12"/><path d="m7 10 5 5 5-5"/><path d="M5 21h14"/></svg>`
}

func chromeOpen(sourcePath string, waiting bool) string {
	name := filepath.Base(sourcePath)
	attrs := `data-source="` + html.EscapeString(sourcePath) + `"`
	if waiting {
		attrs += ` data-waiting="1"`
	}
	return `<div class="vellum-chrome" ` + attrs + `">` +
		`<aside class="vellum-toc" id="vellum-toc">` +
		`<div class="vellum-toc-header">` +
		`<span class="vellum-toc-title">Contents</span>` +
		`<div class="vellum-toc-actions">` +
		`<button type="button" data-vellum="toc-collapse" title="Collapse all" aria-label="Collapse all">↖</button>` +
		`<button type="button" data-vellum="toc-expand" title="Expand all" aria-label="Expand all">↘</button>` +
		`</div></div>` +
		`<nav class="vellum-toc-nav" id="vellum-toc-nav" aria-label="Table of contents"></nav>` +
		`</aside>` +
		`<div class="vellum-toc-splitter" id="vellum-toc-splitter" role="separator" aria-orientation="vertical" aria-label="Resize table of contents" tabindex="0"></div>` +
		`<div class="vellum-main">` +
		`<header class="vellum-toolbar">` +
		`<button type="button" class="vellum-toc-toggle-btn" data-vellum="toc-toggle" title="Hide table of contents" aria-label="Hide table of contents">«</button>` +
		`<button type="button" data-vellum="pdf" title="Download a PDF of this document" aria-label="Download PDF">PDF` + downloadIconSVG() + `</button>` +
		`<button type="button" data-vellum="clipboard" title="Copy the rendered document to the clipboard">Copy</button>` +
		`<button type="button" data-vellum="reveal" title="Reveal the source file in Finder">Show in Finder</button>` +
		`<div class="vellum-toolbar-end">` +
		`<span class="vellum-docname">` + html.EscapeString(name) + `</span>` +
		`<button type="button" class="vellum-theme-btn" data-vellum="theme" title="Theme: System">◐</button>` +
		`</div>` +
		`<span class="vellum-status" id="vellum-status" hidden></span>` +
		`</header>` +
		`<div class="vellum-scroll" id="vellum-scroll" tabindex="0" role="region" aria-label="Document">` +
		`<article class="vellum-article">`
}

func chromeClose() string {
	return `</article></div></div>` +
		`<div class="vellum-consent" id="vellum-consent" hidden role="dialog" aria-modal="true" aria-labelledby="vellum-consent-title">` +
		`<div class="vellum-consent-card">` +
		`<p id="vellum-consent-title">Toggling a checkbox edits the Markdown file on disk.</p>` +
		`<div class="vellum-consent-actions">` +
		`<button type="button" data-vellum="consent-session">This session</button>` +
		`<button type="button" data-vellum="consent-always">Always allow</button>` +
		`<button type="button" data-vellum="consent-cancel">Cancel</button>` +
		`</div></div></div>` +
		`<div class="vellum-consent" id="vellum-gone" hidden role="dialog" aria-modal="true" aria-labelledby="vellum-gone-title">` +
		`<div class="vellum-consent-card">` +
		`<p id="vellum-gone-title">This file was deleted.</p>` +
		`<div class="vellum-consent-actions">` +
		`<button type="button" data-vellum="gone-dismiss">Dismiss</button>` +
		`</div></div></div></div>` +
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
