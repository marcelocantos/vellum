// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/vellum/internal/remoteimage"
)

// A CLI or MCP conversion embeds files from the document's own folder
// and nowhere else. The engines and pandoc would read whatever the
// document names (an attachment, an image by absolute path, file: URL or
// ../), so none of them is given a file to read: every local file a
// sink embeds is read here, through docFiles, and handed over as a
// data: URL (render-boundaries report, D1 and D2).
//
//   - PDF: InlineResources with docFiles.read, then an engine that may
//     fetch http and https but reads no file (Backend.Render).
//   - RTF: InlineResources with docFiles.readImage and remote images
//     fetched by vellum, then pandoc --sandbox.
//   - pptx: each image of pandoc's own parse inlined the same way, then
//     pandoc --sandbox (writePPTX).

// errOutsideDocument refuses a file outside every folder a conversion
// may read.
var errOutsideDocument = errors.New("outside the document's folder")

// errNotImage refuses a file a sink would embed as a picture when it is
// not one.
var errNotImage = errors.New("not an image")

// docFiles is the folders one conversion may read: the document's own
// folder (for content, which has none, the output file's folder, where
// its relative names resolve) and the media folder an import extracted
// into. A file is read through os.Root, so neither ../ nor a symlink
// leaves the folder.
type docFiles struct {
	roots []string
}

func newDocFiles(dirs ...string) docFiles {
	var d docFiles
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		d.roots = append(d.roots, abs)
	}
	return d
}

// read returns the bytes of the file at abs when it lies inside one of
// the folders.
func (d docFiles) read(abs string) ([]byte, error) {
	abs = filepath.Clean(abs)
	for _, root := range d.roots {
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		b, err := readInRoot(root, rel)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	return nil, fmt.Errorf("%s: %w", abs, errOutsideDocument)
}

// readImage is read for sinks that embed only pictures (pptx, RTF).
func (d docFiles) readImage(abs string) ([]byte, error) {
	b, err := d.read(abs)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(mediaTypeFor(abs, b), "image/") {
		return nil, fmt.Errorf("%s: %w", abs, errNotImage)
	}
	return b, nil
}

func readInRoot(root, rel string) ([]byte, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", filepath.Join(root, rel))
	}
	return io.ReadAll(f)
}

// inlineLocal turns every resource a rendered page names into a data:
// URL read through files, leaving http and https for an engine that
// may fetch them (PDF).
func inlineLocal(ctx context.Context, page, baseDir string, files docFiles) (string, error) {
	return InlineResources(ctx, page, baseDir, &InlineArgs{Read: files.read, KeepRemote: true})
}

// inlineImages turns every resource a rendered page names into a data:
// URL: a local one only when it is an image inside files, a remote one
// fetched by vellum (remoteimage.Fetch). For pandoc --sandbox (RTF),
// which then reads nothing itself.
func inlineImages(ctx context.Context, page, baseDir string, files docFiles) (string, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, remoteimage.Budget)
	defer cancel()
	return InlineResources(fetchCtx, page, baseDir, &InlineArgs{Read: files.readImage, Fetch: remoteimage.Fetch})
}
