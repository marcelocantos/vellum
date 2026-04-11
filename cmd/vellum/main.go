// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/vellum/convert"
	"github.com/marcelocantos/vellum/docs"
	vellummcp "github.com/marcelocantos/vellum/mcp"
)

const version = "0.1.0"

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
		output        string
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
		return runMCP()
	}

	return runCLI(positional, output)
}

func printUsage() {
	fmt.Print(`Usage: vellum [options] <input.md...>
       vellum --mcp

Document preparation — convert Markdown to PDF via goldmark + Prince.

Options:
  --help         Show this help message
  --help-agent   Show this help plus the embedded agent guide
  --version      Print version number
  --mcp          Run as an MCP (Model Context Protocol) server on stdio
  -o <path>      Output PDF path (single input file only)

Examples:
  vellum report.md                   # produces report.pdf
  vellum -o out.pdf report.md        # explicit output path
  vellum ch1.md ch2.md ch3.md        # batch conversion

Requires Prince (https://www.princexml.com/) on PATH.
`)
}

func runCLI(args []string, output string) error {
	if len(args) == 0 {
		printUsage()
		return fmt.Errorf("no input files specified")
	}

	if output != "" && len(args) > 1 {
		return fmt.Errorf("-o flag is only allowed with a single input file")
	}

	if err := convert.CheckDeps(); err != nil {
		return err
	}

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

		if err := convert.Convert(ctx, absInput, outputPath, nil); err != nil {
			return fmt.Errorf("%s: %w", inputPath, err)
		}

		fmt.Println(outputPath)
	}

	return nil
}

func runMCP() error {
	if err := convert.CheckDeps(); err != nil {
		return err
	}
	return vellummcp.Serve(context.Background(), version)
}
