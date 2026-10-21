// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/marcelocantos/vellum/internal/testdeps"
)

var (
	pdfStreamRe = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)
	// A link annotation's destination, either named (WeasyPrint:
	// /Dest (options)) or explicit (Prince: /Dest [page /XYZ x y z]).
	pdfNamedLinkRe = regexp.MustCompile(`/Dest\s*\(([^)]*)\)`)
	pdfArrayLinkRe = regexp.MustCompile(`/Dest\s*\[\s*\d+\s+\d+\s+R\s*/XYZ\s+([-\d.]+)\s+([-\d.]+)`)
	// An entry of the /Dests name tree: (name) [page /XYZ x y z].
	pdfNameEntryRe = regexp.MustCompile(`\(([^)]*)\)\s*\[\s*\d+\s+\d+\s+R\s*/XYZ\s+([-\d.]+)\s+([-\d.]+)`)
	bboxWordRe     = regexp.MustCompile(`<word xMin="([-\d.]+)" yMin="([-\d.]+)"[^>]*>([^<]*)</word>`)
	bboxPageRe     = regexp.MustCompile(`<page width="[-\d.]+" height="([-\d.]+)">`)
)

// pdfObjects returns the PDF's bytes with every Flate stream inflated and
// appended, so annotations and name trees kept in object streams can be
// matched as text without a PDF library.
func pdfObjects(t *testing.T, pdf []byte) string {
	t.Helper()
	var buf bytes.Buffer
	buf.Write(pdf)
	for _, m := range pdfStreamRe.FindAllSubmatch(pdf, -1) {
		r, err := zlib.NewReader(bytes.NewReader(m[1]))
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(r)
		buf.WriteByte('\n')
		buf.Write(b)
	}
	return buf.String()
}

// 🎯T49: a same-page link [x](#id) in the PDF is an internal GoTo link
// whose destination is the heading it names, on both backends.
func TestConvert_PDFFragmentLinksJumpToHeadings(t *testing.T) {
	testdeps.Need(t, "pdftotext")
	src := "# Anchor Test\n\nJump to [Options](#options) or [the end](#end-matter).\n\n" +
		"## Overview\n\nText.\n\n## Options\n\nOption text.\n\n## End Matter\n\nDone.\n"
	headings := map[string]string{"options": "Options", "end-matter": "End"}
	for _, backend := range backends(t) {
		t.Run(backend, func(t *testing.T) {
			dir := t.TempDir()
			in := filepath.Join(dir, "doc.md")
			out := filepath.Join(dir, "doc.pdf")
			if err := os.WriteFile(in, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := Convert(context.Background(), in, out, &Options{Backend: backend}); err != nil {
				t.Fatalf("Convert: %v", err)
			}
			pdf, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			objs := pdfObjects(t, pdf)
			if regexp.MustCompile(`/URI\s*\([^)]*#`).MatchString(objs) {
				t.Errorf("a fragment link became an external URI action")
			}

			// Destination y (PDF user space, origin bottom-left) of every link.
			var linkYs []float64
			named := map[string]float64{}
			for _, m := range pdfNameEntryRe.FindAllStringSubmatch(objs, -1) {
				y, _ := strconv.ParseFloat(m[3], 64)
				named[m[1]] = y
			}
			links := pdfLinkDicts(objs)
			if len(links) == 0 {
				t.Fatalf("no link annotations in the PDF")
			}
			for _, m := range pdfNamedLinkRe.FindAllStringSubmatch(links, -1) {
				y, ok := named[m[1]]
				if !ok {
					t.Errorf("link to named destination %q, which the PDF does not define", m[1])
					continue
				}
				linkYs = append(linkYs, y)
			}
			for _, m := range pdfArrayLinkRe.FindAllStringSubmatch(links, -1) {
				y, _ := strconv.ParseFloat(m[2], 64)
				linkYs = append(linkYs, y)
			}
			if len(linkYs) == 0 {
				t.Fatalf("no internal link annotations in the PDF")
			}

			// Heading positions from the text layer (origin top-left).
			bbox, err := exec.Command("pdftotext", "-bbox", out, "-").Output()
			if err != nil {
				t.Fatalf("pdftotext -bbox: %v", err)
			}
			pm := bboxPageRe.FindSubmatch(bbox)
			if pm == nil {
				t.Fatalf("no page in bbox output")
			}
			pageH, _ := strconv.ParseFloat(string(pm[1]), 64)
			words := bboxWordRe.FindAllSubmatch(bbox, -1)
			for id, word := range headings {
				// The heading is the last occurrence (the first is the link text).
				top := math.NaN()
				for _, w := range words {
					if string(w[3]) == word {
						top, _ = strconv.ParseFloat(string(w[2]), 64)
					}
				}
				if math.IsNaN(top) {
					t.Fatalf("heading %q not in text layer", word)
				}
				hit := false
				for _, y := range linkYs {
					if math.Abs((pageH-y)-top) < 8 {
						hit = true
					}
				}
				if !hit {
					t.Errorf("no link lands on heading #%s (top %.1f); destinations (top-down) %v", id, top, topDown(pageH, linkYs))
				}
			}
		})
	}
}

// pdfLinkDicts returns the dictionaries of every /Subtype /Link
// annotation, one per line. Outline (bookmark) entries carry /Dest too, so
// the destinations must be read from link annotations only.
func pdfLinkDicts(objs string) string {
	var out bytes.Buffer
	re := regexp.MustCompile(`/Subtype\s*/Link\b`)
	for _, loc := range re.FindAllStringIndex(objs, -1) {
		// Walk back to the "<<" that opens this dictionary.
		depth, start := 0, -1
		for i := loc[0] - 1; i > 0; i-- {
			if objs[i-1:i+1] == ">>" {
				depth++
				i--
			} else if objs[i-1:i+1] == "<<" {
				if depth == 0 {
					start = i - 1
					break
				}
				depth--
				i--
			}
		}
		if start < 0 {
			continue
		}
		depth = 0
		for i := start; i+1 < len(objs); i++ {
			switch objs[i : i+2] {
			case "<<":
				depth++
				i++
			case ">>":
				depth--
				i++
				if depth == 0 {
					out.WriteString(objs[start : i+1])
					out.WriteByte('\n')
					i = len(objs)
				}
			}
		}
	}
	return out.String()
}

func topDown(h float64, ys []float64) []float64 {
	out := make([]float64, len(ys))
	for i, y := range ys {
		out[i] = math.Round((h-y)*10) / 10
	}
	return out
}
