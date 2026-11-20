// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	_ "embed"
	"html"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/marcelocantos/vellum/internal/hint"
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
	// ChromeKaTeXPrefix serves the embedded KaTeX stylesheet and fonts, so
	// a viewed math document loads nothing from the network.
	ChromeKaTeXPrefix = "/_vellum/katex/"
	FaviconICOPath    = "/favicon.ico"
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

// chromePage describes one served page for injectChrome.
type chromePage struct {
	// SourcePath is the Markdown file the page shows.
	SourcePath string
	// Nonce is this response's script nonce (see documentCSP). The
	// viewer's own scripts carry it; nothing else on the page may run.
	Nonce string
	// Waiting marks the placeholder page for a file that does not exist
	// yet: no editor, and the watch reloads when the file appears.
	Waiting bool
}

// injectChrome wraps convert HTML with view-server chrome (toolbar, TOC
// shell, lightbox). The convert-cache file is left untouched; callers
// apply this per response.
func injectChrome(htmlDoc string, page *chromePage) string {
	if htmlDoc == "" {
		return htmlDoc
	}
	htmlDoc = addBodyClass(htmlDoc, "vellum-view")
	if loc := headCloseRe.FindStringIndex(htmlDoc); loc != nil {
		htmlDoc = htmlDoc[:loc[0]] + chromeFaviconLink() + chromeThemeInitScript(page.Nonce) + htmlDoc[loc[0]:]
	}
	open := bodyOpenRe.FindStringIndex(htmlDoc)
	close := bodyCloseRe.FindStringIndex(htmlDoc)
	if open == nil || close == nil || close[0] < open[1] {
		return htmlDoc
	}
	inner := annotateTaskCheckboxes(htmlDoc[open[1]:close[0]])
	wrapped := chromeOpen(page.SourcePath, page.Waiting) + inner + chromeClose()
	htmlDoc = htmlDoc[:open[1]] + wrapped + htmlDoc[close[0]:]
	if loc := headCloseRe.FindStringIndex(htmlDoc); loc != nil {
		htmlDoc = htmlDoc[:loc[0]] + chromeStyleTag() + htmlDoc[loc[0]:]
	}
	if loc := bodyCloseRe.FindStringIndex(htmlDoc); loc != nil {
		htmlDoc = htmlDoc[:loc[0]] + chromeScriptTag(page.Nonce) + htmlDoc[loc[0]:]
	}
	return htmlDoc
}

func chromeStyleTag() string {
	return "<style>\n" + chromeCSS + "\n" + chromaDarkThemeCSS + "</style>\n"
}

func chromeFaviconLink() string {
	return `<link rel="icon" href="` + ChromeFaviconPath + `" type="image/svg+xml">` + "\n"
}

func chromeThemeInitScript(nonce string) string {
	return `<script nonce="` + html.EscapeString(nonce) + `">(function(){try{var k='vellum-theme',t=localStorage.getItem(k),o=['dark','system','light'];document.documentElement.dataset.vellumTheme=o.indexOf(t)>=0?t:'system';}catch(e){document.documentElement.dataset.vellumTheme='system';}})();</script>` + "\n"
}

func chromeScriptTag(nonce string) string {
	return `<script nonce="` + html.EscapeString(nonce) + `">` + "\n" + chromeJS + "\n</script>\n"
}

func pencilIconSVG() string {
	return `<svg class="vellum-icon" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21.17 6.81a2.83 2.83 0 0 0-4-4L3.84 16.17a2 2 0 0 0-.5.83l-1.32 4.35a.5.5 0 0 0 .62.62l4.35-1.32a2 2 0 0 0 .83-.5Z"/><path d="m15 5 4 4"/></svg>`
}

func copyIconSVG() string {
	return `<svg class="vellum-icon" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/></svg>`
}

// pdfIconSVG is a page with "PDF" lettered across it: the generic PDF
// glyph, not Adobe's mark. The page's stroke is masked out behind the
// letters so they read at toolbar size.
func pdfIconSVG() string {
	return `<svg class="vellum-icon vellum-icon-lg" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><defs><mask id="vellum-pdf-gap" maskUnits="userSpaceOnUse" x="0" y="0" width="24" height="24"><rect x="0" y="0" width="24" height="24" fill="#fff"/><rect x="0" y="11.5" width="24" height="8" fill="#000"/></mask></defs><g mask="url(#vellum-pdf-gap)"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/></g><text x="12" y="18.6" text-anchor="middle" textLength="23" lengthAdjust="spacingAndGlyphs" font-family="-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Helvetica Neue', Arial, sans-serif" font-size="8.6" font-weight="800" fill="currentColor" stroke="none">PDF</text></svg>`
}

func chromeOpen(sourcePath string, waiting bool) string {
	name := filepath.Base(sourcePath)
	attrs := `data-source="` + html.EscapeString(sourcePath) + `"`
	edit := ""
	if waiting {
		attrs += ` data-waiting="1"`
	} else {
		// A waiting page has no file yet, so nothing to edit.
		attrs += ` data-editor="` + html.EscapeString(editorEntryURL()) + `"`
		edit = `<button type="button" class="vellum-icon-btn" data-vellum="edit" title="Edit the Markdown source (E)" aria-label="Edit the Markdown source" aria-haspopup="dialog" aria-controls="vellum-editor">` + pencilIconSVG() + `</button>`
	}
	return `<div class="vellum-chrome" ` + attrs + `>` +
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
		`<button type="button" class="vellum-icon-btn" data-vellum="pdf" title="Download as PDF" aria-label="Download as PDF">` + pdfIconSVG() + `</button>` +
		edit +
		`<button type="button" class="vellum-icon-btn" data-vellum="clipboard" title="Copy the rendered document to the clipboard" aria-label="Copy the rendered document to the clipboard">` + copyIconSVG() + `</button>` +
		`<button type="button" class="vellum-icon-btn" data-vellum="reveal" data-file-manager="` + hint.FileManager() + `" title="Show in ` + hint.FileManager() + `" aria-label="Show in ` + hint.FileManager() + `">` +
		`<img class="vellum-icon vellum-icon-lg vellum-fm-icon" src="` + ChromeFileManagerIconPath + `" width="16" height="16" alt=""></button>` +
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
		`<div class="vellum-editor" id="vellum-editor" hidden role="dialog" aria-modal="true" aria-labelledby="vellum-editor-title">` +
		`<div class="vellum-editor-panel">` +
		`<div class="vellum-editor-bar">` +
		`<span class="vellum-editor-title" id="vellum-editor-title"></span>` +
		`<span class="vellum-editor-state" id="vellum-editor-state" aria-live="polite"></span>` +
		`<div class="vellum-editor-actions">` +
		`<button type="button" data-vellum="ed-save" title="Save (⌘S / Ctrl+S)">Save</button>` +
		`<button type="button" data-vellum="ed-close" title="Close the editor (Esc)">Done</button>` +
		`</div></div>` +
		`<div class="vellum-editor-banner" id="vellum-editor-banner" hidden role="alert"></div>` +
		`<div class="vellum-editor-host" id="vellum-editor-host"></div>` +
		`</div></div>` +
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
