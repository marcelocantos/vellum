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

	"gopkg.in/yaml.v3"

	"github.com/marcelocantos/vellum/convert"
)

// Config is the on-disk vellum configuration.
type Config struct {
	// Backend names the default renderer ("weasyprint" or "prince"). Empty
	// resolves to convert.DefaultBackend (WeasyPrint).
	Backend string `yaml:"backend,omitempty"`
	// Style holds default style overrides applied to every conversion.
	Style *convert.Style `yaml:"style,omitempty"`
}

// Path returns the resolved config file path. Honors XDG_CONFIG_HOME and
// falls back to ~/.config/vellum/config.yaml.
func Path() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "vellum", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "vellum", "config.yaml"), nil
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
