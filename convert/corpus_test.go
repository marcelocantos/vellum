// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/marcelocantos/vellum/internal/testdeps"
)

// The corpus oracle asserts imports against hand-authored ground truth,
// never against a reference implementation's output. Round-tripping a
// fixture through pandoc in both directions would verify pandoc's
// self-consistency and pass even if both directions were symmetrically
// wrong, so the manifests carry the expectations instead (🎯T20).

type manifest struct {
	Name       string `yaml:"name"`
	Provenance struct {
		Source       string `yaml:"source"`
		Producer     string `yaml:"producer"`
		ProducerNote string `yaml:"producer_note"`
		Produced     string `yaml:"produced"`
		Limitation   string `yaml:"limitation"`
	} `yaml:"provenance"`
	Fixtures    []fixture   `yaml:"fixtures"`
	GroundTruth groundTruth `yaml:"ground_truth"`
}

type fixture struct {
	File   string `yaml:"file"`
	Format string `yaml:"format"`
	// Headings, when set, lists exactly the ATX headings this
	// producer's output must yield. Producers differ: LibreOffice's
	// docx export keeps both levels while its odt export demotes the
	// first H1, so the expectation belongs per fixture, not per case.
	Headings      []string `yaml:"headings"`
	ExpectAssets  int      `yaml:"expect_assets"`
	KnownFailures []struct {
		Property string `yaml:"property"`
		Reason   string `yaml:"reason"`
	} `yaml:"known_failures"`
}

// fails reports whether this fixture is a recorded known-failure for the
// named property. Known failures are asserted to still fail, so a fixed
// upstream defect surfaces as a test failure rather than passing
// silently — a ratchet in both directions.
func (f fixture) fails(property string) bool {
	for _, kf := range f.KnownFailures {
		if kf.Property == property {
			return true
		}
	}
	return false
}

type groundTruth struct {
	TextContains        []string `yaml:"text_contains"`
	DocumentOrder       []string `yaml:"document_order"`
	HeadingsRecoverable bool     `yaml:"headings_recoverable"`
	Images              int      `yaml:"images"`
	Emphasis            struct {
		Bold   []string `yaml:"bold"`
		Italic []string `yaml:"italic"`
	} `yaml:"emphasis"`
}

const corpusRoot = "testdata/corpus"

func loadCorpus(t *testing.T) []manifest {
	t.Helper()
	entries, err := os.ReadDir(corpusRoot)
	if err != nil {
		t.Fatalf("reading corpus: %v", err)
	}
	var out []manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(corpusRoot, e.Name(), "manifest.yaml")
		b, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var m manifest
		if err := yaml.Unmarshal(b, &m); err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		if m.Name == "" {
			t.Fatalf("%s: manifest has no name", path)
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		t.Fatal("corpus is empty; the oracle would vacuously pass")
	}
	return out
}

// importFixture runs a corpus file through the shipped convert path and
// returns the resulting Markdown.
func importFixture(t *testing.T, m manifest, f fixture) string {
	t.Helper()
	path := filepath.Join(corpusRoot, m.Name, f.File)
	res, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaFile, Path: path, Format: f.Format},
		To:   Endpoint{Media: MediaContent, Format: FormatMarkdown},
	})
	if err != nil {
		t.Fatalf("importing %s: %v", path, err)
	}
	if res.Content == "" {
		t.Fatalf("importing %s produced empty content", path)
	}
	return res.Content
}

func eachFixture(t *testing.T, fn func(t *testing.T, m manifest, f fixture, md string)) {
	testdeps.Need(t, "pandoc")
	for _, m := range loadCorpus(t) {
		for _, f := range m.Fixtures {
			t.Run(m.Name+"/"+f.File, func(t *testing.T) {
				fn(t, m, f, importFixture(t, m, f))
			})
		}
	}
}

// TestCorpus_TextPreserved is the load-bearing property: whatever else
// import does to structure, no authored text may vanish.
func TestCorpus_TextPreserved(t *testing.T) {
	eachFixture(t, func(t *testing.T, m manifest, f fixture, md string) {
		for _, want := range m.GroundTruth.TextContains {
			if !strings.Contains(md, want) {
				t.Errorf("imported Markdown is missing %q", want)
			}
		}
	})
}

// TestCorpus_DocumentOrder checks the ground-truth markers appear as a
// subsequence, leaving intervening content and formatting free.
func TestCorpus_DocumentOrder(t *testing.T) {
	eachFixture(t, func(t *testing.T, m manifest, f fixture, md string) {
		ordered := isSubsequence(md, m.GroundTruth.DocumentOrder)
		if f.fails("document_order") {
			if ordered {
				t.Errorf("document_order is recorded as a known failure but now passes; " +
					"upstream appears fixed — drop the known_failures entry from the manifest")
			}
			return
		}
		if !ordered {
			t.Errorf("ground-truth markers are out of document order\nmarkers: %v\ngot:\n%s",
				m.GroundTruth.DocumentOrder, md)
		}
	})
}

func isSubsequence(haystack string, markers []string) bool {
	pos := 0
	for _, mk := range markers {
		i := strings.Index(haystack[pos:], mk)
		if i < 0 {
			return false
		}
		pos += i + len(mk)
	}
	return true
}

// TestCorpus_EmphasisPreserved checks inline emphasis survives as
// Markdown emphasis rather than being flattened to plain text.
func TestCorpus_EmphasisPreserved(t *testing.T) {
	eachFixture(t, func(t *testing.T, m manifest, f fixture, md string) {
		for _, w := range m.GroundTruth.Emphasis.Bold {
			if !regexp.MustCompile(`\*\*` + regexp.QuoteMeta(w) + `\*\*`).MatchString(md) {
				t.Errorf("%q is not bold in the imported Markdown", w)
			}
		}
		for _, w := range m.GroundTruth.Emphasis.Italic {
			if !regexp.MustCompile(`(^|[^*])\*` + regexp.QuoteMeta(w) + `\*([^*]|$)`).MatchString(md) {
				t.Errorf("%q is not italic in the imported Markdown", w)
			}
		}
	})
}

// TestCorpus_HeadingRecoverability pins what each producer's files can
// support. Producers that write only visual formatting must not appear
// to yield headings, and producers that write semantic styles must.
func TestCorpus_HeadingRecoverability(t *testing.T) {
	headingLine := regexp.MustCompile(`(?m)^#{1,6} +(.*)$`)
	eachFixture(t, func(t *testing.T, m manifest, f fixture, md string) {
		var got []string
		for _, mt := range headingLine.FindAllStringSubmatch(md, -1) {
			got = append(got, strings.TrimSpace(mt[1]))
		}
		if want := m.GroundTruth.HeadingsRecoverable; (len(got) > 0) != want {
			t.Errorf("headings_recoverable=%v but imported Markdown yielded %v", want, got)
		}
		if f.Headings == nil {
			return
		}
		if strings.Join(got, "|") != strings.Join(f.Headings, "|") {
			t.Errorf("headings = %v, want %v", got, f.Headings)
		}
	})
}

// TestCorpus_MediaExtracted covers the #20 extraction path: an embedded
// picture must land on disk and be referenced from the Markdown, so a
// link an agent follows resolves to a real file.
func TestCorpus_MediaExtracted(t *testing.T) {
	testdeps.Need(t, "pandoc")
	for _, m := range loadCorpus(t) {
		for _, f := range m.Fixtures {
			if f.ExpectAssets == 0 {
				continue
			}
			t.Run(m.Name+"/"+f.File, func(t *testing.T) {
				path := filepath.Join(corpusRoot, m.Name, f.File)
				t.Setenv("VELLUM_IMPORT_CACHE", t.TempDir())
				res, err := Run(context.Background(), &Request{
					From: Endpoint{Media: MediaFile, Path: path, Format: f.Format},
					To:   Endpoint{Media: MediaContent, Format: FormatMarkdown},
				})
				if err != nil {
					t.Fatalf("importing %s: %v", path, err)
				}
				if len(res.Assets) != f.ExpectAssets {
					t.Fatalf("assets = %v, want %d", res.Assets, f.ExpectAssets)
				}
				for _, a := range res.Assets {
					info, serr := os.Stat(a)
					if serr != nil {
						t.Errorf("asset %s not on disk: %v", a, serr)
						continue
					}
					if info.Size() == 0 {
						t.Errorf("asset %s is empty", a)
					}
				}
				// The Markdown must actually point at what was
				// extracted, so a link an agent follows resolves.
				// pandoc emits raw <img> rather than ![](…) when the
				// source carries sizing attributes, so match on the
				// path rather than on Markdown image syntax.
				for _, a := range res.Assets {
					if !strings.Contains(res.Content, a) {
						t.Errorf("imported Markdown does not reference extracted asset %s:\n%s",
							a, res.Content)
					}
				}
			})
		}
	}
}

// TestCorpus_ProvenanceIsUntainted is the guard on the guard: it fails
// if a fixture is ever produced by a tool that also sits in the import
// path, which would turn the oracle into a round-trip self-check.
func TestCorpus_ProvenanceIsUntainted(t *testing.T) {
	tainted := []string{"pandoc", "weasyprint", "prince", "vellum"}
	for _, m := range loadCorpus(t) {
		producer := strings.ToLower(m.Provenance.Producer)
		if producer == "" {
			t.Errorf("%s: manifest declares no producer", m.Name)
			continue
		}
		for _, bad := range tainted {
			if strings.Contains(producer, bad) {
				t.Errorf("%s: fixtures produced by %q, which is in the import path; "+
					"the oracle would be verifying round-trip closure, not correctness",
					m.Name, m.Provenance.Producer)
			}
		}
		if m.Provenance.Source == "" {
			t.Errorf("%s: manifest declares no authored source", m.Name)
		}
	}
}

// TestUnknownExtensionIsNotSilentlyMarkdown pins the .doc defect the
// corpus surfaced: an unrecognised extension falls through
// formatFromExt to "" and ingest defaults to Markdown, so binary
// content is returned verbatim with a nil error.
func TestUnknownExtensionIsNotSilentlyMarkdown(t *testing.T) {
	path := filepath.Join(corpusRoot, "visual", "visual.doc")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("fixture absent: %v", err)
	}
	res, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaFile, Path: path},
		To:   Endpoint{Media: MediaContent, Format: FormatMarkdown},
	})
	if err != nil {
		return // Rejecting it outright is the desired behaviour.
	}
	if res != nil && strings.ContainsRune(res.Content, 0) {
		t.Errorf("legacy .doc was read as Markdown: %d bytes of binary returned with a nil error; "+
			"formatFromExt has no .doc case and ingest defaults to markdown", len(res.Content))
	}
}
