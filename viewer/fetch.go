// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import (
	"context"

	"github.com/marcelocantos/vellum/internal/remoteimage"
)

// A viewer PDF shows a document's remote images the way its page does,
// but the PDF engine fetches nothing (convert.WriteSelfContainedPDF), so
// the viewer fetches them itself and inlines them, through
// remoteimage.Fetch: each request says it is a cross-site image, so a
// viewer it reaches never takes it for the user opening a document (see
// openedByUser).
var fetchRemoteImage = remoteimage.Fetch

const (
	// remoteImagesBudget bounds every remote image of one PDF together.
	remoteImagesBudget = remoteimage.Budget
	// remoteImageTimeout bounds one image, including a remote SVG measured
	// for the page (fit_remote.go).
	remoteImageTimeout = remoteimage.Timeout

	secFetchSiteCrossSite = remoteimage.SiteCrossSite
	secFetchDestImage     = remoteimage.DestImage
)

// fetchRemote GETs rawURL as a page's image load would, and returns its
// body when its media type starts with want, refusing anything else
// before reading it. The SVG fit asks for image/svg+xml and a body no
// larger than svgfit.MaxBytes; other callers keep the image defaults
// through fetchRemoteImage.
func fetchRemote(ctx context.Context, rawURL, accept, want string, maxBytes int64) ([]byte, string, error) {
	return remoteimage.FetchMatch(ctx, rawURL, accept, want, maxBytes)
}
