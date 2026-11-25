// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

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

// A viewer PDF shows a document's remote images the way its page does,
// but the PDF engine fetches nothing (convert.WriteSelfContainedPDF), so
// the viewer fetches them itself and inlines them.
//
// Each request says what it is: a cross-site, no-cors image load, the
// fetch metadata a browser sends for the page's own <img>. A viewer that
// receives one (this one, reached by any name or through a tunnel, or
// by a redirect) refuses it as a page's subresource and never takes it
// for the user opening a document, which is how a request without fetch
// metadata is read (see openedByUser).
const (
	// remoteImageTimeout bounds one image, as WeasyPrint's default did
	// when the engine fetched them.
	remoteImageTimeout = 10 * time.Second
	// remoteImagesBudget bounds every remote image of one PDF together;
	// what has not arrived by then is left out.
	remoteImagesBudget = 30 * time.Second
	// remoteImageMaxBytes bounds one image's body.
	remoteImageMaxBytes = 16 << 20
	// remoteImageMaxRedirects bounds the redirects one image follows.
	remoteImageMaxRedirects = 5

	secFetchSiteCrossSite = "cross-site"
	secFetchModeNoCORS    = "no-cors"
	secFetchDestImage     = "image"
)

var errNotAnImage = errors.New("not an image")

var remoteImageClient = &http.Client{
	Timeout: remoteImageTimeout,
	// Go follows only http and https, and sends a request's headers
	// (the fetch metadata included) again on each hop.
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= remoteImageMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", remoteImageMaxRedirects)
		}
		return nil
	},
}

// fetchRemoteImage returns the bytes and media type of the http or https
// image at rawURL. A response that is not an image (by its Content-Type)
// is refused, as the page's <img> would show nothing for it.
func fetchRemoteImage(ctx context.Context, rawURL string) ([]byte, string, error) {
	return fetchRemote(ctx, rawURL, "image/*", "image/", remoteImageMaxBytes)
}

// fetchRemote GETs rawURL as a page's image load would, and returns its
// body when its media type starts with want, refusing anything else
// before reading it.
func fetchRemote(ctx context.Context, rawURL, accept, want string, maxBytes int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return nil, "", fmt.Errorf("%s: not http or https", rawURL)
	}
	req.Header.Set(secFetchSiteHeader, secFetchSiteCrossSite)
	req.Header.Set(secFetchModeHeader, secFetchModeNoCORS)
	req.Header.Set(secFetchDestHeader, secFetchDestImage)
	req.Header.Set("Accept", accept)
	resp, err := remoteImageClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s: %s", rawURL, resp.Status)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, want) {
		return nil, "", fmt.Errorf("%s: %w", rawURL, errNotAnImage)
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
