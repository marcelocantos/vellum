// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/vellum/clipboard"
	"github.com/marcelocantos/vellum/internal/testdeps"
)

// The media axis and the format axis are orthogonal in Run: ingest
// switches on from.Media alone and runOne on to.Media alone, with the
// two halves communicating through a single Markdown string. These
// tests exercise the 4x4 media grid at one canonical format; format
// behaviour is table-tested separately against the pure inference
// functions rather than by multiplying the two axes together (🎯T20).

// fakeClipboard installs in-memory stand-ins for the pasteboard seams
// and returns handles to what the sinks captured.
type fakeClipboard struct {
	readable     []byte
	readFormat   string
	readRefs     []string
	wroteHTML    string
	wroteRefs    []string
	writeCalled  bool
	refsRecorded bool
}

func installFakeClipboard(t *testing.T, fc *fakeClipboard) {
	t.Helper()
	prevWrite, prevRead := clipboardWriteFn, clipboardReadImportableFn
	prevReadRefs, prevWriteRefs := clipboardReadFileRefsFn, clipboardWriteFileRefsFn
	t.Cleanup(func() {
		clipboardWriteFn, clipboardReadImportableFn = prevWrite, prevRead
		clipboardReadFileRefsFn, clipboardWriteFileRefsFn = prevReadRefs, prevWriteRefs
	})

	clipboardWriteFn = func(p clipboard.Payload) error {
		fc.wroteHTML = p.HTML
		fc.writeCalled = true
		return nil
	}
	clipboardReadImportableFn = func() ([]byte, string, error) {
		return fc.readable, fc.readFormat, nil
	}
	clipboardReadFileRefsFn = func() ([]string, error) { return fc.readRefs, nil }
	clipboardWriteFileRefsFn = func(p clipboard.FileRefPayload) error {
		fc.wroteRefs = p.Paths
		fc.refsRecorded = true
		return nil
	}
}

// TestMediaMatrix covers all 16 from-media x to-media pairs. Before the
// clipboard seam existed only 3 were reachable through Run.
func TestMediaMatrix(t *testing.T) {
	const marker = "matrixMarker"
	sources := []Media{MediaFile, MediaContent, MediaClipboard, MediaFileReference}
	sinks := []Media{MediaFile, MediaContent, MediaClipboard, MediaFileReference}

	for _, from := range sources {
		for _, to := range sinks {
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				// Clipboard sources import through pandoc; the other
				// three stay on the pure-Go path.
				if from == MediaClipboard {
					testdeps.Need(t, "pandoc")
				}

				dir := t.TempDir()
				inPath := filepath.Join(dir, "in.md")
				if err := os.WriteFile(inPath, []byte("# Title\n\n"+marker+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}

				fc := &fakeClipboard{
					readable:   []byte("<p>" + marker + "</p>"),
					readFormat: FormatHTML,
					readRefs:   []string{inPath},
				}
				installFakeClipboard(t, fc)

				req := &Request{To: Endpoint{Media: to, Format: FormatMarkdown}}
				switch from {
				case MediaFile:
					req.From = Endpoint{Media: MediaFile, Path: inPath}
				case MediaContent:
					req.From = Endpoint{Media: MediaContent, Content: "# Title\n\n" + marker + "\n"}
				case MediaClipboard:
					req.From = Endpoint{Media: MediaClipboard}
				case MediaFileReference:
					req.From = Endpoint{Media: MediaFileReference}
				}
				// Sinks that write files need an explicit destination
				// whenever the source carries no path of its own.
				if to == MediaFile || to == MediaFileReference {
					req.To.Path = filepath.Join(dir, "out.md")
				}

				res, err := Run(context.Background(), req)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				if res.FromMedia != from || res.ToMedia != to {
					t.Errorf("result media = %s->%s, want %s->%s",
						res.FromMedia, res.ToMedia, from, to)
				}

				switch to {
				case MediaContent:
					if !strings.Contains(res.Content, marker) {
						t.Errorf("content sink lost the marker: %q", res.Content)
					}
				case MediaClipboard:
					if !fc.writeCalled {
						t.Error("clipboard sink never wrote to the pasteboard")
					}
					if !strings.Contains(fc.wroteHTML, marker) {
						t.Errorf("clipboard HTML lost the marker: %q", fc.wroteHTML)
					}
				case MediaFile, MediaFileReference:
					if len(res.Paths) != 1 {
						t.Fatalf("paths = %v, want exactly one", res.Paths)
					}
					b, rerr := os.ReadFile(res.Paths[0])
					if rerr != nil {
						t.Fatal(rerr)
					}
					if !strings.Contains(string(b), marker) {
						t.Errorf("file sink lost the marker: %q", b)
					}
					if to == MediaFileReference {
						if !fc.refsRecorded {
							t.Error("file_reference sink never wrote a pasteboard reference")
						}
						if len(fc.wroteRefs) != 1 || fc.wroteRefs[0] != res.Paths[0] {
							t.Errorf("pasteboard refs = %v, want [%s]", fc.wroteRefs, res.Paths[0])
						}
					}
				}
			})
		}
	}
}

// TestClipboardSourceErrors covers the failure arms the seam made
// reachable: an empty pasteboard on each clipboard-backed source.
func TestClipboardSourceErrors(t *testing.T) {
	t.Run("no file references", func(t *testing.T) {
		fc := &fakeClipboard{}
		installFakeClipboard(t, fc)
		_, err := Run(context.Background(), &Request{
			From: Endpoint{Media: MediaFileReference},
			To:   Endpoint{Media: MediaContent},
		})
		if err == nil || !strings.Contains(err.Error(), "no file references") {
			t.Fatalf("err = %v, want a no-file-references error", err)
		}
	})

	t.Run("nothing importable", func(t *testing.T) {
		fc := &fakeClipboard{}
		installFakeClipboard(t, fc)
		_, err := Run(context.Background(), &Request{
			From: Endpoint{Media: MediaClipboard},
			To:   Endpoint{Media: MediaContent},
		})
		if err == nil || !strings.Contains(err.Error(), "no RTF, HTML, or PDF") {
			t.Fatalf("err = %v, want an empty-clipboard error", err)
		}
	})
}

// TestFormatInferenceGrid table-tests the sink-format defaulting rules
// across the whole media grid. These are pure functions, so the full
// cross product is cheap here rather than through Run.
func TestFormatInferenceGrid(t *testing.T) {
	cases := []struct {
		media    Media
		toPath   string
		toFormat string
		fromFmt  string
		want     string
	}{
		// Sink defaults with no explicit format.
		{MediaContent, "", "", FormatMarkdown, FormatMarkdown},
		{MediaClipboard, "", "", FormatMarkdown, FormatRich},
		{MediaFile, "", "", FormatMarkdown, FormatPDF},
		{MediaFileReference, "", "", FormatMarkdown, FormatPDF},

		// Rich-text sources default to a Markdown sink; HTML stays HTML.
		{MediaFile, "", "", "docx", FormatMarkdown},
		{MediaFile, "", "", FormatRTF, FormatMarkdown},
		{MediaFile, "", "", "epub", FormatMarkdown},
		{MediaFile, "", "", FormatHTML, FormatHTML},

		// An output extension outranks the source-driven default.
		{MediaFile, "out.html", "", FormatMarkdown, FormatHTML},
		{MediaFile, "out.md", "", FormatMarkdown, FormatMarkdown},
		{MediaFile, "out.pdf", "", "docx", FormatPDF},

		// An explicit format outranks everything.
		{MediaFile, "out.html", FormatPDF, FormatMarkdown, FormatPDF},
		{MediaClipboard, "", FormatHTML, FormatMarkdown, FormatHTML},
		{MediaContent, "", "md", FormatMarkdown, FormatMarkdown},
	}
	for _, c := range cases {
		got := inferToFormat(Endpoint{Media: c.media, Path: c.toPath, Format: c.toFormat}, c.fromFmt)
		if got != c.want {
			t.Errorf("inferToFormat(media=%s path=%q format=%q, from=%s) = %q, want %q",
				c.media, c.toPath, c.toFormat, c.fromFmt, got, c.want)
		}
	}
}

// TestPDFSinkRestrictions pins that PDF is rejected for the two sinks
// that cannot carry binary content.
func TestPDFSinkRestrictions(t *testing.T) {
	for _, m := range []Media{MediaContent, MediaClipboard} {
		if err := checkDisallowed(m, FormatPDF); err == nil {
			t.Errorf("%s + pdf should be rejected", m)
		}
	}
	for _, m := range []Media{MediaFile, MediaFileReference} {
		if err := checkDisallowed(m, FormatPDF); err != nil {
			t.Errorf("%s + pdf should be allowed: %v", m, err)
		}
	}
}
