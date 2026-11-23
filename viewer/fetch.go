// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package viewer

import "github.com/marcelocantos/vellum/internal/remoteimage"

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

	secFetchSiteCrossSite = remoteimage.SiteCrossSite
	secFetchDestImage     = remoteimage.DestImage
)
