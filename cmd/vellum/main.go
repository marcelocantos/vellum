// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/vellum/clipboard"
	"github.com/marcelocantos/vellum/config"
	"github.com/marcelocantos/vellum/convert"
	"github.com/marcelocantos/vellum/docs"
	vellummcp "github.com/marcelocantos/vellum/mcp"
)

const version = "0.4.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Manual arg parsing to allow flags anywhere (Go's flag package
	// stops at the first non-flag argument).
	var (
		showHelp      bool
		showHelpAgent bool
		showVersion   bool
		mcpMode       bool
		toClipboard   bool
		output        string
		backend       string
		positional    []string
	)

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-help":
			showHelp = true
		case a == "--help-agent" || a == "-help-agent":
			showHelpAgent = true
		case a == "--version" || a == "-version":
			showVersion = true
		case a == "--mcp" || a == "-mcp":
			mcpMode = true
		case a == "--to-clipboard" || a == "-to-clipboard":
			toClipboard = true
		case a == "-o" || a == "--output":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires an argument", a)
			}
			i++
			output = args[i]
		case strings.HasPrefix(a, "-o="):
			output = a[len("-o="):]
		case strings.HasPrefix(a, "--output="):
			output = a[len("--output="):]
		case a == "--backend":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires an argument", a)
			}
			i++
			backend = args[i]
		case strings.HasPrefix(a, "--backend="):
			backend = a[len("--backend="):]
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag: %s", a)
		default:
			positional = append(positional, a)
		}
	}

	if showHelp {
		printUsage()
		return nil
	}

	if showHelpAgent {
		printUsage()
		fmt.Println()
		fmt.Print(docs.AgentGuide)
		if !strings.HasSuffix(docs.AgentGuide, "\n") {
			fmt.Println()
		}
		return nil
	}

	if showVersion {
		fmt.Println(version)
		return nil
	}

	if mcpMode {
		return runMCP(backend)
	}

	if toClipboard {
		return runClipboard(positional, output, backend)
	}

	return runCLI(positional, output, backend)
}

// effectiveBackend resolves the backend name as: CLI flag > config > default.
// Returns the empty string when nothing is set so callers can use it directly
// — convert.ResolveBackend treats "" as DefaultBackend.
func effectiveBackend(flag, fromConfig string) string {
	if flag != "" {
		return flag
	}
	return fromConfig
}

func runClipboard(args []string, output, backendFlag string) error {
	if len(args) == 0 {
		printUsage()
		return fmt.Errorf("no input files specified")
	}
	if len(args) > 1 {
		return fmt.Errorf("--to-clipboard accepts a single input file")
	}
	if output != "" {
		return fmt.Errorf("--to-clipboard and -o are mutually exclusive")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	backendName := effectiveBackend(backendFlag, cfg.Backend)

	ctx := context.Background()
	absInput, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}

	html, err := convert.RenderFile(ctx, absInput, &convert.Options{Style: cfg.Style, Backend: backendName})
	if err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}

	if err := clipboard.Write(clipboard.Payload{HTML: html}); err != nil {
		return fmt.Errorf("clipboard: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Copied to clipboard.")
	return nil
}

func printUsage() {
	fmt.Print(`Usage: vellum [options] <input.md...>
       vellum --mcp

Document preparation — convert Markdown to PDF via goldmark + Prince.

Options:
  --help           Show this help message
  --help-agent     Show this help plus the embedded agent guide
  --version        Print version number
  --mcp            Run as an MCP (Model Context Protocol) server on stdio
  --to-clipboard   Render and place RTF + HTML + plain text on the system
                   clipboard (single input file; macOS only currently)
  -o <path>        Output PDF path (single input file only)

Examples:
  vellum report.md                   # produces report.pdf
  vellum -o out.pdf report.md        # explicit output path
  vellum ch1.md ch2.md ch3.md        # batch conversion
  vellum --to-clipboard slack.md     # ready to paste into Slack/Mail

Requires Prince (https://www.princexml.com/) on PATH.
`)
}

func runCLI(args []string, output, backendFlag string) error {
	if len(args) == 0 {
		printUsage()
		return fmt.Errorf("no input files specified")
	}

	if output != "" && len(args) > 1 {
		return fmt.Errorf("-o flag is only allowed with a single input file")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	backendName := effectiveBackend(backendFlag, cfg.Backend)

	if err := convert.CheckDeps(backendName); err != nil {
		return err
	}

	opts := &convert.Options{Style: cfg.Style, Backend: backendName}

	ctx := context.Background()

	for _, inputPath := range args {
		absInput, err := filepath.Abs(inputPath)
		if err != nil {
			return err
		}

		outputPath := output
		if outputPath == "" {
			outputPath = strings.TrimSuffix(absInput, filepath.Ext(absInput)) + ".pdf"
		} else {
			outputPath, err = filepath.Abs(outputPath)
			if err != nil {
				return err
			}
		}

		if err := convert.Convert(ctx, absInput, outputPath, opts); err != nil {
			return fmt.Errorf("%s: %w", inputPath, err)
		}

		fmt.Println(outputPath)
	}

	return nil
}

func runMCP(backendFlag string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// For the MCP server's dep check, use the configured default backend.
	// Per-call backend overrides (set via MCP tool input) still work — the
	// per-call value just becomes a runtime require, not a startup gate.
	backendName := effectiveBackend(backendFlag, cfg.Backend)
	if err := convert.CheckDeps(backendName); err != nil {
		return err
	}
	return vellummcp.Serve(context.Background(), version)
}
