// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package config loads user configuration for vellum from a YAML file.
//
// The file is expected at $XDG_CONFIG_HOME/vellum/config.yaml when
// XDG_CONFIG_HOME is set, otherwise $HOME/.config/vellum/config.yaml.
// A missing file is not an error; Load returns an empty Config.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/marcelocantos/vellum/convert"
	"github.com/marcelocantos/vellum/internal/xdg"
)

// Config is the on-disk vellum configuration.
type Config struct {
	// Backend names the default renderer ("weasyprint" or "prince"). Empty
	// resolves to convert.DefaultBackend (WeasyPrint).
	Backend string `yaml:"backend,omitempty"`
	// Template names the default PowerPoint reference deck for pptx
	// output: a path to a .pptx file, a bare name under
	// ~/.config/vellum/templates/, or empty for pandoc's default deck.
	Template string `yaml:"template,omitempty"`
	// Style holds default style overrides applied to every conversion.
	Style *convert.Style `yaml:"style,omitempty"`
	// Viewer configures the view server (vellum serve-view).
	Viewer *Viewer `yaml:"viewer,omitempty"`
}

// Viewer is the view server's part of the configuration.
type Viewer struct {
	// Folders are served from the start, with everything beneath them, so
	// no document in them needs opening first. Each is an absolute path;
	// a leading "~" is the home folder. "/" serves everything. Without
	// this the server serves only folders a document was opened from.
	Folders []string `yaml:"folders,omitempty"`
}

// homePrefix is how a configured path names the home folder.
const homePrefix = "~"

// ViewerFolders returns Viewer.Folders as absolute, cleaned paths. A
// folder that is not absolute once "~" is expanded is an error. Whether
// a folder exists is left to the server: a config file shared between
// machines may name folders only one of them has.
func (c *Config) ViewerFolders() ([]string, error) {
	if c.Viewer == nil {
		return nil, nil
	}
	var out []string
	for _, f := range c.Viewer.Folders {
		p := strings.TrimSpace(f)
		rest, tilde := strings.CutPrefix(p, homePrefix)
		if tilde && (rest == "" || rest[0] == '/' || rest[0] == filepath.Separator) {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("viewer.folders: %q: %w", f, err)
			}
			p = filepath.Join(home, rest)
		}
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("viewer.folders: %q is not an absolute path", f)
		}
		out = append(out, filepath.Clean(p))
	}
	return out, nil
}

// Path returns the resolved config file path. Honors XDG_CONFIG_HOME and
// falls back to ~/.config/vellum/config.yaml (%APPDATA%\vellum\config.yaml
// on Windows).
func Path() (string, error) {
	dir, err := xdg.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// Load reads and parses the config file. A missing file is not an error:
// Load returns &Config{} in that case.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
}
