// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package convert

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeAndInfer(t *testing.T) {
	if got := normalizeFormat("MD"); got != FormatMarkdown {
		t.Errorf("normalizeFormat(MD)=%q", got)
	}
	if got := formatFromExt("x.DOCX"); got != "docx" {
		t.Errorf("formatFromExt docx=%q", got)
	}
	to := Endpoint{Media: MediaContent}
	if got := inferToFormat(to, FormatMarkdown); got != FormatMarkdown {
		t.Errorf("content default=%q", got)
	}
	to = Endpoint{Media: MediaFile}
	if got := inferToFormat(to, FormatMarkdown); got != FormatPDF {
		t.Errorf("file from md default=%q", got)
	}
	to = Endpoint{Media: MediaFile}
	if got := inferToFormat(to, "docx"); got != FormatMarkdown {
		t.Errorf("file from docx default=%q", got)
	}
	to = Endpoint{Media: MediaClipboard}
	if got := inferToFormat(to, FormatMarkdown); got != FormatRich {
		t.Errorf("clipboard default=%q", got)
	}
}

func TestCheckDisallowed(t *testing.T) {
	if err := checkDisallowed(MediaContent, FormatPDF); err == nil {
		t.Fatal("expected content+pdf disallowed")
	}
	if err := checkDisallowed(MediaClipboard, FormatPDF); err == nil {
		t.Fatal("expected clipboard+pdf disallowed")
	}
	if err := checkDisallowed(MediaFile, FormatPDF); err != nil {
		t.Fatalf("file+pdf should be allowed: %v", err)
	}
}

func TestRunValidation(t *testing.T) {
	ctx := context.Background()
	_, err := Run(ctx, &Request{})
	if err == nil {
		t.Fatal("expected error for empty request")
	}
	_, err = Run(ctx, &Request{
		From: Endpoint{Media: MediaFile},
		To:   Endpoint{Media: MediaContent},
	})
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("expected path required, got %v", err)
	}
	_, err = Run(ctx, &Request{
		From: Endpoint{Media: MediaContent, Content: "# hi"},
		To:   Endpoint{Media: MediaContent, Format: FormatPDF},
	})
	if err == nil || !strings.Contains(err.Error(), "pdf") {
		t.Fatalf("expected pdf disallow, got %v", err)
	}
}

func TestRunContentToMarkdownFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.md")
	res, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaContent, Content: "# Title\n\nHello.\n"},
		To:   Endpoint{Media: MediaFile, Path: out, Format: FormatMarkdown},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Paths) != 1 || res.Paths[0] != out {
		t.Fatalf("paths=%v", res.Paths)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Hello") {
		t.Fatalf("content=%q", b)
	}
}

func TestRunFileMarkdownToHTMLContent(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.md")
	if err := os.WriteFile(in, []byte("# Hi\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaFile, Path: in},
		To:   Endpoint{Media: MediaContent, Format: FormatHTML},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "Body") {
		t.Fatalf("html=%q", res.Content)
	}
	if res.FromFormat != FormatMarkdown {
		t.Errorf("from_format=%q", res.FromFormat)
	}
	if res.ToFormat != FormatHTML {
		t.Errorf("to_format=%q", res.ToFormat)
	}
}

func TestRunContentToContentIdentity(t *testing.T) {
	const md = "# Only\n"
	res, err := Run(context.Background(), &Request{
		From: Endpoint{Media: MediaContent, Content: md},
		To:   Endpoint{Media: MediaContent},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != md {
		t.Fatalf("got %q", res.Content)
	}
}
