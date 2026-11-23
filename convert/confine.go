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
//   - PDF: InlineResources with docFiles.read and remote assets fetched
//     by vellum, then an engine that fetches nothing (writePDF).
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
// leaves the folder. Each folder is pinned to the directory inode it
// had when the conversion started, so replacing that path with a
// symlink afterwards is refused.
type docFiles struct {
	roots []pinnedRoot
}

// pinnedRoot is one folder a conversion may read. path is the absolute
// prefix file names are compared with. open is that folder with
// symlinks resolved, which is what os.Root opens. info is the directory
// itself at pin time.
type pinnedRoot struct {
	path string
	open string
	info os.FileInfo
}

func newDocFiles(dirs ...string) docFiles {
	var d docFiles
	for _, dir := range dirs {
		d = d.withDir(dir)
	}
	return d
}

// withDir pins dir if it exists and is not already pinned. A directory
// created during the conversion (the output folder of a content
// conversion) is pinned after it exists, before any file is read.
func (d docFiles) withDir(dir string) docFiles {
	p, ok := pinRoot(dir)
	if !ok {
		return d
	}
	for _, e := range d.roots {
		if e.open == p.open {
			return d
		}
	}
	d.roots = append(d.roots, p)
	return d
}

func pinRoot(dir string) (pinnedRoot, bool) {
	if dir == "" {
		return pinnedRoot{}, false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return pinnedRoot{}, false
	}
	open, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return pinnedRoot{}, false
	}
	info, err := os.Lstat(open)
	if err != nil || !info.IsDir() {
		return pinnedRoot{}, false
	}
	return pinnedRoot{path: abs, open: open, info: info}, true
}

// read returns the bytes of the file at abs when it lies inside one of
// the folders.
func (d docFiles) read(abs string) ([]byte, error) {
	abs = filepath.Clean(abs)
	for _, root := range d.roots {
		rel, err := filepath.Rel(root.path, abs)
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

func readInRoot(root pinnedRoot, rel string) ([]byte, error) {
	r, err := os.OpenRoot(root.open)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	got, err := r.Stat(".")
	if err != nil {
		return nil, err
	}
	// The path was replaced with a symlink to another directory after
	// the folder was pinned. os.Root would follow that symlink; the
	// inode check refuses it.
	if !os.SameFile(root.info, got) {
		return nil, fmt.Errorf("%s: %w", root.open, errOutsideDocument)
	}
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
		return nil, fmt.Errorf("%s: not a regular file", filepath.Join(root.open, rel))
	}
	return io.ReadAll(f)
}

// inlineLocal turns every resource a rendered page names into a data:
// URL. Local files are read through files. http and https images,
// stylesheets and fonts are fetched by vellum (remoteimage.FetchAsset),
// which sends cross-site fetch metadata, and the PDF engine then
// fetches nothing (writePDF).
func inlineLocal(ctx context.Context, page, baseDir string, files docFiles) (string, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, remoteimage.Budget)
	defer cancel()
	return InlineResources(fetchCtx, page, baseDir, &InlineArgs{Read: files.read, Fetch: remoteimage.FetchAsset})
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
