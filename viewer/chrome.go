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

//go:embed theme.css
var themeCSS string

//go:embed chrome.css
var chromeCSS string

//go:embed chrome.js
var chromeJS string

//go:embed favicon.svg
var faviconSVG string

var (
	headOpenRe  = regexp.MustCompile(`(?i)<head[^>]*>`)
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
	// Stamp is the source stamp the page was rendered from. A task toggle
	// sends it back; the server refuses the toggle if the file moved on.
	Stamp string
	// Embedded marks a page shown inside T3 Code (t3.go). Its URLs are
	// relative to the T3 listener's root, and it has no homepage link and
	// no action that works on the machine the viewer runs on.
	Embedded bool
}

// apiRoot is where the viewer's own routes are, as this page writes them.
func (p *chromePage) apiRoot() string {
	if p.Embedded {
		return t3Root + strings.TrimPrefix(ChromePrefix, "/")
	}
	return ChromePrefix
}

// injectChrome wraps convert HTML with view-server chrome (toolbar, TOC
// shell, lightbox). The convert-cache file is left untouched; callers
// apply this per response.
func injectChrome(htmlDoc string, page *chromePage) string {
	if htmlDoc == "" {
		return htmlDoc
	}
	htmlDoc = addBodyClass(htmlDoc, "vellum-view")
	if page.Embedded {
		// Before anything else in the head, so it governs every script.
		if loc := headOpenRe.FindStringIndex(htmlDoc); loc != nil {
			htmlDoc = htmlDoc[:loc[1]] + embeddedScriptPolicy(page.Nonce) + htmlDoc[loc[1]:]
		}
	}
	if loc := headCloseRe.FindStringIndex(htmlDoc); loc != nil {
		htmlDoc = htmlDoc[:loc[0]] + chromeFaviconLink(page) + chromeThemeInitScript(page.Nonce) + htmlDoc[loc[0]:]
	}
	open := bodyOpenRe.FindStringIndex(htmlDoc)
	close := bodyCloseRe.FindStringIndex(htmlDoc)
	if open == nil || close == nil || close[0] < open[1] {
		return htmlDoc
	}
	inner := htmlDoc[open[1]:close[0]]
	wrapped := chromeOpen(page) + inner + chromeClose()
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
	// The dark syntax colours are for the screen, like the rest of the
	// theme: print keeps the document's own light highlighting.
	return "<style>\n" + themeCSS + "\n" + chromeCSS + "\n@media screen {\n" + chromaDarkThemeCSS + "}\n</style>\n"
}

func chromeFaviconLink(page *chromePage) string {
	return `<link rel="icon" href="` + page.apiRoot() + strings.TrimPrefix(ChromeFaviconPath, ChromePrefix) + `" type="image/svg+xml">` + "\n"
}

// embeddedScriptPolicy restates the viewer's script rule inside the page.
// T3's gateway drops the viewer's Content-Security-Policy header and sends
// its own, which allows any inline script; a <meta> policy is enforced in
// addition to it. Only scripts carrying this response's nonce run, and
// 'strict-dynamic' lets them import the editor bundle, whose gateway URL
// the server cannot name.
func embeddedScriptPolicy(nonce string) string {
	return `<meta http-equiv="Content-Security-Policy" content="script-src 'nonce-` + html.EscapeString(nonce) + `' 'strict-dynamic'; base-uri 'none'">` + "\n"
}

func chromeThemeInitScript(nonce string) string {
	return `<script nonce="` + html.EscapeString(nonce) + `">(function(){try{var k='vellum-theme',t=localStorage.getItem(k),o=['dark','system','light'];document.documentElement.dataset.vellumTheme=o.indexOf(t)>=0?t:'system';}catch(e){document.documentElement.dataset.vellumTheme='system';}})();</script>` + "\n"
}

func chromeScriptTag(nonce string) string {
	return `<script nonce="` + html.EscapeString(nonce) + `">` + "\n" + chromeJS + "\n</script>\n"
}

// lineIcon is a 16px line icon drawn from 24-unit paths, in the current
// text colour.
func lineIcon(class, paths string) string {
	if class != "" {
		class = " " + class
	}
	return `<svg class="vellum-icon` + class + `" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`
}

const (
	iconSidebar     = `<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/>`
	iconPencil      = `<path d="M21.17 6.81a2.83 2.83 0 0 0-4-4L3.84 16.17a2 2 0 0 0-.5.83l-1.32 4.35a.5.5 0 0 0 .62.62l4.35-1.32a2 2 0 0 0 .83-.5Z"/><path d="m15 5 4 4"/>`
	iconCopy        = `<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>`
	iconSun         = `<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>`
	iconMoon        = `<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>`
	iconHalf        = `<circle cx="12" cy="12" r="9"/><path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor" stroke="none"/>`
	iconCollapseAll = `<path d="m7 20 5-5 5 5"/><path d="m7 4 5 5 5-5"/>`
	iconExpandAll   = `<path d="m7 15 5 5 5-5"/><path d="m7 9 5-5 5 5"/>`
	iconMinus       = `<path d="M5 12h14"/>`
	iconPlus        = `<path d="M5 12h14"/><path d="M12 5v14"/>`
	iconFit         = `<path d="M8 3H5a2 2 0 0 0-2 2v3"/><path d="M21 8V5a2 2 0 0 0-2-2h-3"/><path d="M3 16v3a2 2 0 0 0 2 2h3"/><path d="M16 21h3a2 2 0 0 0 2-2v-3"/>`
	iconClose       = `<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`
)

// pdfIconSVG is a page with "PDF" lettered across it: the generic PDF
// glyph, not Adobe's mark. The page's stroke is masked out behind the
// letters so they read at toolbar size.
func pdfIconSVG() string {
	return `<svg class="vellum-icon" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><defs><mask id="vellum-pdf-gap" maskUnits="userSpaceOnUse" x="0" y="0" width="24" height="24"><rect x="0" y="0" width="24" height="24" fill="#fff"/><rect x="0" y="11.5" width="24" height="8" fill="#000"/></mask></defs><g mask="url(#vellum-pdf-gap)"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/></g><text x="12" y="18.6" text-anchor="middle" textLength="23" lengthAdjust="spacingAndGlyphs" font-family="-apple-system, BlinkMacSystemFont, 'Segoe UI', 'Helvetica Neue', Arial, sans-serif" font-size="8.6" font-weight="800" fill="currentColor" stroke="none">PDF</text></svg>`
}

// markSVG is the Vellum mark, favicon.svg's drawing: two strokes of
// translucent sheet crossing at the foot of a V.
func markSVG() string {
	return `<svg class="vellum-icon" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 32 32" fill="none" stroke-width="6.5" stroke-linecap="round">` +
		`<defs><mask id="vellum-mark-left" maskUnits="userSpaceOnUse" x="0" y="0" width="32" height="32"><path d="M7.5 8 16 24.5" stroke="#fff"/></mask></defs>` +
		`<path d="M7.5 8 16 24.5M24.5 8 16 24.5" stroke="#d99f86"/>` +
		`<path d="M24.5 8 16 24.5" stroke="#cb7a58" mask="url(#vellum-mark-left)"/></svg>`
}

// iconButton is a borderless toolbar button: label is both its tooltip
// and its accessible name.
func iconButton(action, label, extra, icon string) string {
	if extra != "" {
		extra = " " + extra
	}
	return `<button type="button" class="vellum-btn" data-vellum="` + action + `" title="` + label + `" aria-label="` + label + `"` + extra + `>` + icon + `</button>`
}

func chromeOpen(page *chromePage) string {
	name := html.EscapeString(filepath.Base(page.SourcePath))
	path := html.EscapeString(page.SourcePath)
	attrs := `data-source="` + path + `"`
	if page.Stamp != "" {
		attrs += ` data-stamp="` + html.EscapeString(page.Stamp) + `"`
	}
	if page.Embedded {
		attrs += ` data-embedded data-api="` + page.apiRoot() + `" data-docs="` + t3Root + strings.TrimPrefix(t3DocPrefix, "/") + `"`
	}
	fm := hint.FileManager()
	actions := ""
	if page.Waiting {
		// A waiting page has no file yet: nothing to edit, copy, print or
		// reveal.
		attrs += ` data-waiting="1"`
	} else {
		editor := page.apiRoot() + strings.TrimPrefix(editorEntryURL(), ChromePrefix)
		attrs += ` data-editor="` + html.EscapeString(editor) + `"`
		actions = iconButton("edit", "Edit the Markdown source (E)", `aria-haspopup="dialog" aria-controls="vellum-editor"`, lineIcon("", iconPencil))
		if !page.Embedded {
			// Copy, PDF and reveal act where the viewer runs. An embedded
			// page may be read on another machine, and its frame can
			// neither download nor reach that machine's clipboard.
			actions += iconButton("clipboard", "Copy the rendered document to the clipboard", "", lineIcon("", iconCopy)) +
				iconButton("pdf", "Download as PDF", "", pdfIconSVG()) +
				iconButton("reveal", "Show in "+fm, `data-file-manager="`+fm+`"`,
					`<img class="vellum-icon vellum-fm-icon" src="`+ChromeFileManagerIconPath+`" width="16" height="16" alt="">`)
		}
		actions += `<span class="vellum-sep" aria-hidden="true"></span>`
	}
	home := `<a class="vellum-btn vellum-home" href="/" title="Recent documents" aria-label="Recent documents">` + markSVG() + `</a>`
	if page.Embedded {
		// The T3 listener has no homepage.
		home = ""
	}
	return `<div class="vellum-chrome" ` + attrs + `>` +
		`<header class="vellum-toolbar">` +
		home +
		iconButton("toc-toggle", "Hide contents", `aria-controls="vellum-toc"`, lineIcon("", iconSidebar)) +
		`<button type="button" class="vellum-doc" data-vellum="top" title="` + path + `">` +
		`<span class="vellum-docname">` + name + `</span>` +
		`<span class="vellum-doctitle" aria-hidden="true"></span>` +
		`</button>` +
		`<div class="vellum-toolbar-end">` +
		actions +
		`<button type="button" class="vellum-btn vellum-theme-btn" data-vellum="theme" title="Theme: System" aria-label="Theme: System">` +
		lineIcon("vellum-icon-light", iconSun) + lineIcon("vellum-icon-dark", iconMoon) + lineIcon("vellum-icon-system", iconHalf) +
		`</button>` +
		`</div>` +
		`</header>` +
		`<div class="vellum-body">` +
		`<aside class="vellum-toc" id="vellum-toc">` +
		`<div class="vellum-toc-header">` +
		`<span class="vellum-toc-title">Contents</span>` +
		`<div class="vellum-toc-actions">` +
		iconButton("toc-collapse", "Collapse all", "", lineIcon("", iconCollapseAll)) +
		iconButton("toc-expand", "Expand all", "", lineIcon("", iconExpandAll)) +
		`</div></div>` +
		`<nav class="vellum-toc-nav" id="vellum-toc-nav" aria-label="Table of contents"></nav>` +
		`</aside>` +
		`<div class="vellum-toc-splitter" id="vellum-toc-splitter" role="separator" aria-orientation="vertical" aria-label="Resize table of contents" tabindex="0"></div>` +
		`<div class="vellum-toc-scrim" data-vellum="toc-close"></div>` +
		`<div class="vellum-main">` +
		`<div class="vellum-scroll" id="vellum-scroll" tabindex="0" role="region" aria-label="Document">` +
		`<article class="vellum-article">`
}

func chromeClose() string {
	return `</article></div></div></div>` +
		`<span class="vellum-status" id="vellum-status" role="status" hidden></span>` +
		`<div class="vellum-consent" id="vellum-consent" hidden role="dialog" aria-modal="true" aria-labelledby="vellum-consent-title">` +
		`<div class="vellum-consent-card">` +
		`<div class="vellum-consent-title" id="vellum-consent-title">Change the file?</div>` +
		`<div class="vellum-consent-text">Ticking a checkbox edits the Markdown file on disk.</div>` +
		`<div class="vellum-consent-actions">` +
		`<button type="button" class="vellum-tbtn" data-vellum="consent-cancel">Cancel</button>` +
		`<button type="button" class="vellum-tbtn" data-vellum="consent-always">Always allow</button>` +
		`<button type="button" class="vellum-tbtn is-primary" data-vellum="consent-session">Allow this session</button>` +
		`</div></div></div>` +
		`<div class="vellum-consent" id="vellum-gone" hidden role="dialog" aria-modal="true" aria-labelledby="vellum-gone-title">` +
		`<div class="vellum-consent-card">` +
		`<div class="vellum-consent-title" id="vellum-gone-title">This file was deleted.</div>` +
		`<div class="vellum-consent-text">The page shows the document as it last was.</div>` +
		`<div class="vellum-consent-actions">` +
		`<button type="button" class="vellum-tbtn" data-vellum="gone-dismiss">Dismiss</button>` +
		`</div></div></div></div>` +
		`<div class="vellum-editor" id="vellum-editor" hidden role="dialog" aria-modal="true" aria-labelledby="vellum-editor-title">` +
		`<div class="vellum-editor-panel">` +
		`<div class="vellum-editor-bar">` +
		`<span class="vellum-editor-title" id="vellum-editor-title"></span>` +
		`<span class="vellum-editor-state" id="vellum-editor-state" aria-live="polite"></span>` +
		`<div class="vellum-editor-actions">` +
		`<button type="button" class="vellum-tbtn" data-vellum="ed-close" title="Close the editor (Esc)">Done</button>` +
		`<button type="button" class="vellum-tbtn is-primary" data-vellum="ed-save" title="Save (⌘S / Ctrl+S)">Save</button>` +
		`</div></div>` +
		`<div class="vellum-editor-banner" id="vellum-editor-banner" hidden role="alert"></div>` +
		`<div class="vellum-editor-host" id="vellum-editor-host"></div>` +
		`</div></div>` +
		`<div class="vellum-lightbox" id="vellum-lightbox" hidden role="dialog" aria-modal="true" aria-label="Figure viewer">` +
		`<div class="vellum-lightbox-bar">` +
		`<div class="vellum-lightbox-zoom">` +
		iconButton("lb-out", "Zoom out (−)", "", lineIcon("", iconMinus)) +
		`<span class="vellum-lightbox-level" id="vellum-lightbox-level"></span>` +
		iconButton("lb-in", "Zoom in (+)", "", lineIcon("", iconPlus)) +
		iconButton("lb-reset", "Fit to window (0)", "", lineIcon("", iconFit)) +
		`</div>` +
		iconButton("lb-close", "Close (Esc)", "", lineIcon("", iconClose)) +
		`</div>` +
		`<div class="vellum-lightbox-stage" id="vellum-lightbox-stage">` +
		`<div class="vellum-lightbox-content" id="vellum-lightbox-content"></div>` +
		`</div>` +
		`<div class="vellum-lightbox-hint">Scroll to zoom · drag to pan</div>` +
		`</div>`
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
