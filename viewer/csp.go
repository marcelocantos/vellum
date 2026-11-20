// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// Content-Security-Policy is the second line of defence behind the
// sanitiser (convert.Options.Sanitize): if a document's markup ever got
// through it, the browser still refuses to run it. Only the viewer's own
// scripts run, each carrying a per-response nonce, plus the editor bundle
// from its fixed path. Inline handlers and javascript: URLs are inline
// script, so the policy refuses them too.
//
// The origin also serves files from the folders a document lives in, so
// 'self' is not used for script: it would admit a .js file that sits next
// to a document.
const cspHeader = "Content-Security-Policy"

// lockdownCSP is set on every response first. Anything that is not one of
// the viewer's pages (a raw file, a fragment, the source text) renders, if
// a browser navigates to it, as an inert document in an opaque origin.
const lockdownCSP = "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; frame-ancestors 'none'; sandbox"

// homeCSP is the homepage's policy: it has no script at all.
const homeCSP = "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// nonceBytes is the entropy of a script nonce (128 bits).
const nonceBytes = 16

// newNonce returns a fresh base64 script nonce for one response.
func newNonce() string {
	b := make([]byte, nonceBytes)
	// crypto/rand.Read never returns an error on supported platforms.
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// documentCSP is the policy for a rendered document (and the page that
// waits for a missing one). Images, fonts and styles may come from
// anywhere a document links them (README badges, config fonts); scripts
// and connections may not.
func documentCSP(r *http.Request, nonce string) string {
	script := "'nonce-" + nonce + "'"
	connect := "'self'"
	if host := cspHost(r); host != "" {
		scheme, ws := "http://", "ws://"
		if r.TLS != nil {
			scheme, ws = "https://", "wss://"
		}
		// A path source admits the editor bundle and its chunks, and
		// nothing else on this origin.
		script += " " + scheme + host + ChromeEditorPrefix
		connect += " " + ws + host
	} else {
		// No host-source can name this origin (an IPv6 literal); fall
		// back to the whole origin for the editor bundle.
		script += " 'self'"
	}
	return "default-src 'none'" +
		"; script-src " + script +
		"; style-src 'self' 'unsafe-inline' https:" +
		"; img-src * data: blob:" +
		"; font-src 'self' data: https:" +
		"; connect-src " + connect +
		"; base-uri 'self'" +
		"; form-action 'none'" +
		"; frame-ancestors 'none'"
}

// cspHost is r.Host rebuilt from parts known to be safe inside a header
// value (localRequestAllowed has already required a loopback name or IP):
// "localhost" or an IPv4 literal, and a numeric port. Anything else
// returns "".
func cspHost(r *http.Request) string {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		host, port = r.Host, ""
	}
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || ip.To4() == nil {
			return ""
		}
		host = ip.String()
	} else {
		host = "localhost"
	}
	if port == "" {
		return host
	}
	if n, err := strconv.Atoi(port); err != nil || n <= 0 || n > 65535 {
		return ""
	}
	return host + ":" + port
}
