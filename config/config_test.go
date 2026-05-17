// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Missing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load returned nil Config")
	}
	if cfg.Style != nil {
		t.Errorf("missing file: want nil Style, got %+v", cfg.Style)
	}
}

func TestLoad_File(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	vellumDir := filepath.Join(dir, "vellum")
	if err := os.MkdirAll(vellumDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `style:
  font_size: 13px
  page_margin: 1.2cm
`
	if err := os.WriteFile(filepath.Join(vellumDir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Style == nil {
		t.Fatal("Style is nil")
	}
	if cfg.Style.FontSize != "13px" {
		t.Errorf("FontSize: want 13px, got %q", cfg.Style.FontSize)
	}
	if cfg.Style.PageMargin != "1.2cm" {
		t.Errorf("PageMargin: want 1.2cm, got %q", cfg.Style.PageMargin)
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	vellumDir := filepath.Join(dir, "vellum")
	if err := os.MkdirAll(vellumDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vellumDir, "config.yaml"), []byte("style: [not, a, map\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); err == nil {
		t.Error("Load: expected error for malformed YAML, got nil")
	}
}

func TestPath_XDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/somewhere")
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	want := "/tmp/somewhere/vellum/config.yaml"
	if p != want {
		t.Errorf("Path: want %q, got %q", want, p)
	}
}
