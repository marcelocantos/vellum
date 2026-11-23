// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package remoteimage fetches a document's http and https images for a
// sink whose engine fetches nothing itself: the viewer's PDF, and the
// CLI and MCP pptx and RTF sinks (pandoc runs sandboxed there).
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
	req.Header.Set("Accept", "image/*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s: %s", rawURL, resp.Status)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "image/") {
		return nil, "", fmt.Errorf("%s: %w", rawURL, ErrNotAnImage)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > MaxBytes {
		return nil, "", fmt.Errorf("%s: larger than %d bytes", rawURL, MaxBytes)
	}
	return body, mediaType, nil
}
