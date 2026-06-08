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
	"github.com/marcelocantos/vellum/importer"
	vellummcp "github.com/marcelocantos/vellum/mcp"
)

const version = "0.5.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// `vellum import …` is a separate subcommand with its own arg parsing.
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "import" {
		return runImport(args[1:])
	}

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

func runImport(args []string) error {
	var (
		showHelp      bool
		fromClipboard bool
		output        string
		format        string
		positional    []string
	)

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-help":
			showHelp = true
		case a == "--from-clipboard" || a == "-from-clipboard":
			fromClipboard = true
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
		case a == "--from":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires an argument", a)
			}
			i++
			format = args[i]
		case strings.HasPrefix(a, "--from="):
			format = a[len("--from="):]
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag for import: %s", a)
		default:
			positional = append(positional, a)
		}
	}

	if showHelp {
		printImportUsage()
		return nil
	}

	if fromClipboard && len(positional) > 0 {
		return fmt.Errorf("import: --from-clipboard and a file path are mutually exclusive")
	}
	if !fromClipboard && len(positional) == 0 {
		printImportUsage()
		return fmt.Errorf("import: no input specified (provide a file path or --from-clipboard)")
	}
	if !fromClipboard && len(positional) > 1 {
		return fmt.Errorf("import: only one input file at a time")
	}

	if err := importer.CheckDep(); err != nil {
		return err
	}

	ctx := context.Background()

	var md string
	if fromClipboard {
		data, detected, err := clipboard.ReadRichText()
		if err != nil {
			return fmt.Errorf("clipboard: %w", err)
		}
		if len(data) == 0 {
			return fmt.Errorf("clipboard: no RTF or HTML content found")
		}
		if format == "" {
			format = detected
		}
		md, err = importer.ImportBytes(ctx, data, format)
		if err != nil {
			return err
		}
	} else {
		absInput, err := filepath.Abs(positional[0])
		if err != nil {
			return err
		}
		md, err = importer.ImportFile(ctx, absInput, format)
		if err != nil {
			return fmt.Errorf("%s: %w", positional[0], err)
		}
	}

	if output == "" {
		fmt.Print(md)
		return nil
	}
	absOutput, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(absOutput); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(absOutput, []byte(md), 0o644)
}

func printImportUsage() {
	fmt.Print(`Usage: vellum import [options] <file>
       vellum import --from-clipboard [options]

Read a rich-text file (or the system clipboard's rich-text content) and
write GitHub-Flavoured Markdown to stdout (or to -o).

Options:
  --help              Show this help
  --from-clipboard    Read the system clipboard's rich-text content
                      (RTF preferred, HTML fallback). macOS only currently.
  --from <fmt>        Input format override (e.g., rtf, docx, html, odt,
                      epub, latex). Defaults to pandoc's auto-detection
                      based on file extension.
  -o <path>           Write Markdown to <path> instead of stdout.

Examples:
  vellum import doc.rtf
  vellum import doc.docx -o doc.md
  vellum import --from-clipboard > snippet.md
  vellum import --from-clipboard -o snippet.md

Requires pandoc on PATH (https://pandoc.org/).
`)
}

func printUsage() {
	fmt.Print(`Usage: vellum [options] <input.md...>
       vellum --mcp
       vellum import [options] <file>

Document preparation — Markdown to PDF (default direction) and rich-text
to Markdown (via the import subcommand).

Options:
  --help              Show this help message
  --help-agent        Show this help plus the embedded agent guide
  --version           Print version number
  --mcp               Run as an MCP (Model Context Protocol) server on stdio
  --to-clipboard      Render and place RTF + HTML + plain text on the
                      system clipboard (single input file; macOS only)
  -o <path>           Output PDF path (single input file only)
  --backend <name>    Renderer backend: "weasyprint" (default) or "prince"

Subcommands:
  import              Read a rich-text file (RTF, DOCX, HTML, ODT, EPUB,
                      LaTeX, …) or the system clipboard and write
                      GitHub-Flavoured Markdown. See "vellum import --help".

Examples:
  vellum report.md                       # produces report.pdf
  vellum -o out.pdf report.md            # explicit output path
  vellum ch1.md ch2.md ch3.md            # batch conversion
  vellum --to-clipboard slack.md         # ready to paste into Slack/Mail
  vellum import doc.docx                 # → Markdown on stdout
  vellum import --from-clipboard         # → Markdown from clipboard

Renderer (default WeasyPrint, optional Prince) must be on PATH for PDF
output. pandoc must be on PATH for the import subcommand.
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
