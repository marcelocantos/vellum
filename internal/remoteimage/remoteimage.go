// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package remoteimage fetches a document's http and https images for a
// sink whose engine fetches nothing itself: the viewer's PDF, the CLI
// and MCP PDF (FetchAsset, which also accepts a stylesheet or a font),
// and the CLI and MCP pptx and RTF sinks (pandoc runs sandboxed there).
//
// Each request says what it is: a cross-site, no-cors image load, the
// fetch metadata a browser sends for a page's own <img>. A vellum viewer
// that receives one (reached by any name, through a tunnel, or by a
// redirect) refuses it as a page's subresource and never takes it for
// the user opening a document, which is how the viewer reads a request
// without fetch metadata (🎯T67).
package remoteimage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

const (
	// Timeout bounds one image, as WeasyPrint's default did when the
	// engine fetched them.
	Timeout = 10 * time.Second
	// Budget bounds every remote image of one document together; what
	// has not arrived by then is left out.
	Budget = 30 * time.Second
	// MaxBytes bounds one image's body.
	MaxBytes = 16 << 20
	// MaxRedirects bounds the redirects one image follows.
	MaxRedirects = 5

	SiteHeader    = "Sec-Fetch-Site"
	ModeHeader    = "Sec-Fetch-Mode"
	DestHeader    = "Sec-Fetch-Dest"
	SiteCrossSite = "cross-site"
	ModeNoCORS    = "no-cors"
	DestImage     = "image"
)

// ErrNotAnImage is returned for a response whose Content-Type is not an
// image type.
var ErrNotAnImage = errors.New("not an image")

var client = &http.Client{
	Timeout: Timeout,
	// Go follows only http and https, and sends a request's headers
	// (the fetch metadata included) again on each hop.
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= MaxRedirects {
			return fmt.Errorf("stopped after %d redirects", MaxRedirects)
		}
		return nil
	},
}

// Fetch returns the bytes and media type of the http or https image at
// rawURL. A response that is not an image (by its Content-Type) is
// refused, as a page's <img> would show nothing for it.
func Fetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	return fetch(ctx, rawURL, "image/*", func(mediaType string) bool {
		return strings.HasPrefix(mediaType, "image/")
	}, MaxBytes)
}

// FetchAsset is Fetch for the resources a PDF page names: an image, a
// stylesheet or a font. The CLI and MCP PDF engines fetch nothing
// themselves, so a remote stylesheet reaches the PDF only through here,
// with the same cross-site fetch metadata as an image.
func FetchAsset(ctx context.Context, rawURL string) ([]byte, string, error) {
	return fetch(ctx, rawURL, "image/*,text/css,font/woff2,font/woff,*/*;q=0.1", acceptAsset, MaxBytes)
}

func acceptAsset(mediaType string) bool {
	if strings.HasPrefix(mediaType, "image/") || strings.HasPrefix(mediaType, "font/") || mediaType == "text/css" {
		return true
	}
	switch mediaType {
	case "application/font-woff", "application/font-sfnt", "application/vnd.ms-fontobject",
		"application/x-font-ttf", "application/x-font-opentype", "application/x-font-woff":
		return true
	}
	return false
}

func fetch(ctx context.Context, rawURL, accept string, ok func(mediaType string) bool, maxBytes int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return nil, "", fmt.Errorf("%s: not http or https", rawURL)
	}
	req.Header.Set(SiteHeader, SiteCrossSite)
	req.Header.Set(ModeHeader, ModeNoCORS)
	req.Header.Set(DestHeader, DestImage)
	req.Header.Set("Accept", accept)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s: %s", rawURL, resp.Status)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !ok(mediaType) {
		return nil, "", fmt.Errorf("%s: %w", rawURL, ErrNotAnImage)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > maxBytes {
		return nil, "", fmt.Errorf("%s: larger than %d bytes", rawURL, maxBytes)
	}
	return body, mediaType, nil
}

// FetchMatch is Fetch with the caller's Accept header, media-type prefix
// and size limit. The viewer uses it to measure one remote SVG
// (image/svg+xml, no larger than what Measure will read) on the same
// client, with the same cross-site image fetch metadata, as Fetch.
func FetchMatch(ctx context.Context, rawURL, accept, want string, maxBytes int64) ([]byte, string, error) {
	return fetch(ctx, rawURL, accept, func(mediaType string) bool {
		return strings.HasPrefix(mediaType, want)
	}, maxBytes)
}
