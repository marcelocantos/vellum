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
	yaml := `template: brand
style:
  font_size: 13px
  page_margin: 1.2cm
  toc: true
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
	if !cfg.Style.TOCOn() {
		t.Errorf("TOC: want on from config")
	}
	if cfg.Template != "brand" {
		t.Errorf("Template: want brand, got %q", cfg.Template)
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
	want := filepath.Join("/tmp/somewhere", "vellum", "config.yaml")
	if p != want {
		t.Errorf("Path: want %q, got %q", want, p)
	}
}

func TestViewerFolders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	abs := filepath.Join(home, "elsewhere")
	cases := []struct {
		name    string
		folders []string
		want    []string
		wantErr bool
	}{
		{name: "none"},
		{name: "absolute", folders: []string{abs}, want: []string{abs}},
		{name: "cleaned", folders: []string{abs + string(filepath.Separator) + "." + string(filepath.Separator)}, want: []string{abs}},
		{name: "home", folders: []string{"~"}, want: []string{home}},
		{name: "under home", folders: []string{"~/work", " ~/notes "}, want: []string{filepath.Join(home, "work"), filepath.Join(home, "notes")}},
		{name: "relative", folders: []string{"work"}, wantErr: true},
		{name: "a name that starts with a tilde", folders: []string{"~work"}, wantErr: true},
		{name: "empty", folders: []string{""}, wantErr: true},
	}
	for _, c := range cases {
		cfg := &Config{}
		if c.folders != nil {
			cfg.Viewer = &Viewer{Folders: c.folders}
		}
		got, err := cfg.ViewerFolders()
		if (err != nil) != c.wantErr {
			t.Errorf("%s: error %v, want error %v", c.name, err, c.wantErr)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			}
		}
	}
}

func TestLoad_ViewerFolders(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "vellum"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.VolumeName(dir) + string(filepath.Separator)
	doc := "viewer:\n  folders:\n    - '" + root + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "vellum", "config.yaml"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.ViewerFolders()
	if err != nil || len(got) != 1 || got[0] != root {
		t.Fatalf("got %v, %v; want [%s]", got, err, root)
	}
}
