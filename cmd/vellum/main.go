// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/marcelocantos/vellum/config"
	"github.com/marcelocantos/vellum/convert"
	"github.com/marcelocantos/vellum/docs"
	vellummcp "github.com/marcelocantos/vellum/mcp"
	"github.com/marcelocantos/vellum/viewer"
)

const version = "0.11.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "convert":
			return runConvert(args[1:])
		case "import":
			return runImport(args[1:])
		case "view":
			return runView(args[1:])
		case "install-viewer":
			return runInstallViewer(args[1:])
		case "uninstall-viewer":
			return runUninstallViewer(args[1:])
		}
	}

	// Manual arg parsing to allow flags anywhere (Go's flag package
	// stops at the first non-flag argument).
	var (
		showHelp      bool
		showHelpAgent bool
		showVersion   bool
		mcpMode       bool
		toClipboard   bool
		openMode      bool
		asPDF         bool
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
		case a == "--open" || a == "-open":
			openMode = true
		case a == "--pdf" || a == "-pdf":
			// Accepted with --open / view; rejected for other modes below.
			asPDF = true
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
		case a == "-":
			// Stdin sentinel for --to-clipboard (and similar). Must not
			// fall through to the unknown-flag branch.
			positional = append(positional, a)
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

	if openMode {
		// --open is a flag form of `vellum view`.
		viewArgs := append([]string{}, positional...)
		if asPDF {
			viewArgs = append([]string{"--pdf"}, viewArgs...)
		}
		if backend != "" {
			viewArgs = append([]string{"--backend", backend}, viewArgs...)
		}
		return runView(viewArgs)
	}
	if asPDF {
		return fmt.Errorf("--pdf is only valid with --open or the view subcommand")
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
		return fmt.Errorf("no input specified (provide a .md file or '-' for stdin)")
	}
	if len(args) > 1 {
		return fmt.Errorf("--to-clipboard accepts a single input (file path or '-')")
	}
	if output != "" {
		return fmt.Errorf("--to-clipboard and -o are mutually exclusive")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	req := &convert.Request{
		To:      convert.Endpoint{Media: convert.MediaClipboard},
		Style:   cfg.Style,
		Backend: effectiveBackend(backendFlag, cfg.Backend),
	}
	if args[0] == "-" {
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		if len(src) == 0 {
			return fmt.Errorf("--to-clipboard: stdin is empty")
		}
		req.From = convert.Endpoint{Media: convert.MediaContent, Content: string(src)}
	} else {
		req.From = convert.Endpoint{Media: convert.MediaFile, Path: args[0]}
	}
	if _, err := convert.Run(context.Background(), req); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Copied to clipboard.")
	return nil
}

func runView(args []string) error {
	var (
		showHelp   bool
		asPDF      bool
		backend    string
		positional []string
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-help":
			showHelp = true
		case a == "--pdf" || a == "-pdf":
			asPDF = true
		case a == "--backend":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires an argument", a)
			}
			i++
			backend = args[i]
		case strings.HasPrefix(a, "--backend="):
			backend = a[len("--backend="):]
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag for view: %s", a)
		default:
			positional = append(positional, a)
		}
	}
	if showHelp {
		printViewUsage()
		return nil
	}
	if len(positional) == 0 {
		printViewUsage()
		return fmt.Errorf("view: no input file specified")
	}
	if len(positional) > 1 {
		return fmt.Errorf("view: only one input file at a time")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	format := viewer.FormatHTML
	if asPDF {
		format = viewer.FormatPDF
	}
	path, err := viewer.View(context.Background(), positional[0], &viewer.ViewOptions{
		Format:  format,
		Style:   cfg.Style,
		Backend: effectiveBackend(backend, cfg.Backend),
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, path)
	return nil
}

func runInstallViewer(args []string) error {
	for _, a := range args {
		switch a {
		case "--help", "-help":
			printInstallViewerUsage()
			return nil
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag for install-viewer: %s", a)
			}
			return fmt.Errorf("install-viewer takes no arguments")
		}
	}
	appPath, err := viewer.InstallViewer(nil)
	if err != nil {
		return err
	}
	fmt.Printf("Installed %s and registered as default Markdown viewer.\n", appPath)
	fmt.Println("Double-click a .md file to open it rendered. Re-run after brew upgrades if the launcher path goes stale.")
	return nil
}

func runUninstallViewer(args []string) error {
	for _, a := range args {
		switch a {
		case "--help", "-help":
			printUninstallViewerUsage()
			return nil
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag for uninstall-viewer: %s", a)
			}
			return fmt.Errorf("uninstall-viewer takes no arguments")
		}
	}
	if err := viewer.UninstallViewer(nil); err != nil {
		return err
	}
	fmt.Println("Removed Vellum Viewer.app. Previous default Markdown handler is not restored automatically — set it via Finder Get Info if needed.")
	return nil
}

func printViewUsage() {
	fmt.Print(`Usage: vellum view [options] <file.md>
       vellum --open [options] <file.md>

Render a Markdown file to a cache location and open it in the OS default
viewer. Never writes a PDF/HTML next to the source file. Unchanged
sources hit the cache (keyed by absolute path + mtime).

Options:
  --help              Show this help
  --pdf               Render PDF (high fidelity) instead of HTML
  --backend <name>    PDF backend: "weasyprint" (default) or "prince"
                      (only relevant with --pdf)

Examples:
  vellum view notes.md            # HTML → browser (fast default)
  vellum view --pdf notes.md      # PDF → Preview
  vellum --open notes.md          # flag form of view

Cache lives under the user cache dir (…/Caches/vellum/view on macOS).
Pruned on each view: entries older than 7 days are dropped; if total
size still exceeds 50 MB, oldest entries are evicted until under cap.
`)
}

func printInstallViewerUsage() {
	fmt.Print(`Usage: vellum install-viewer

Generate ~/Applications/Vellum Viewer.app, register it with Launch
Services, and set it as the default handler for Markdown (.md, etc.).

The app executable is a small Cocoa binary (compiled with clang at
install time) that receives open-document Apple Events from Launch
Services and runs 'vellum --open'. A shell-script launcher cannot
receive those events — double-click would silently do nothing.

Requires macOS and Xcode Command Line Tools (clang). Setting the
default handler requires 'duti' on PATH (brew install duti). Without
duti the bundle is still installed and can be chosen via Finder Get
Info → Open with.

Re-run after 'brew upgrade vellum' so LSEnvironment points at the new
CLI path. Debug log: ~/Library/Logs/vellum-viewer.log
`)
}

func printUninstallViewerUsage() {
	fmt.Print(`Usage: vellum uninstall-viewer

Remove ~/Applications/Vellum Viewer.app. Does not restore the previous
default Markdown handler — use Finder Get Info to reassign if needed.
`)
}

func runConvert(args []string) error {
	var (
		showHelp   bool
		fromMedia  string
		toMedia    string
		fromFmt    string
		toFmt      string
		output     string
		backend    string
		positional []string
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--help" || a == "-help":
			showHelp = true
		case a == "--from":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires an argument", a)
			}
			i++
			fromMedia = args[i]
		case strings.HasPrefix(a, "--from="):
			fromMedia = a[len("--from="):]
		case a == "--to":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires an argument", a)
			}
			i++
			toMedia = args[i]
		case strings.HasPrefix(a, "--to="):
			toMedia = a[len("--to="):]
		case a == "--format":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires an argument", a)
			}
			i++
			fromFmt = args[i]
		case strings.HasPrefix(a, "--format="):
			fromFmt = a[len("--format="):]
		case a == "--to-format":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires an argument", a)
			}
			i++
			toFmt = args[i]
		case strings.HasPrefix(a, "--to-format="):
			toFmt = a[len("--to-format="):]
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
		case a == "-":
			positional = append(positional, a)
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag for convert: %s", a)
		default:
			positional = append(positional, a)
		}
	}
	if showHelp {
		printConvertUsage()
		return nil
	}
	if fromMedia == "" || toMedia == "" {
		printConvertUsage()
		return fmt.Errorf("convert: --from and --to are required")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	req := &convert.Request{
		From:    convert.Endpoint{Media: convert.Media(fromMedia), Format: fromFmt},
		To:      convert.Endpoint{Media: convert.Media(toMedia), Format: toFmt, Path: output},
		Style:   cfg.Style,
		Backend: effectiveBackend(backend, cfg.Backend),
	}

	switch convert.Media(fromMedia) {
	case convert.MediaFile:
		if len(positional) == 0 {
			return fmt.Errorf("convert: from file requires a path")
		}
		if len(positional) == 1 {
			req.From.Path = positional[0]
		} else {
			req.From.Paths = positional
		}
	case convert.MediaContent:
		var src []byte
		if len(positional) == 0 || (len(positional) == 1 && positional[0] == "-") {
			src, err = io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
		} else if len(positional) == 1 {
			src, err = os.ReadFile(positional[0])
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("convert: from content accepts at most one path or '-'")
		}
		if len(src) == 0 {
			return fmt.Errorf("convert: empty content")
		}
		req.From.Content = string(src)
	case convert.MediaClipboard, convert.MediaFileReference:
		if len(positional) > 0 {
			return fmt.Errorf("convert: from %s does not take a path", fromMedia)
		}
	default:
		return fmt.Errorf("convert: unknown --from media %q", fromMedia)
	}

	res, err := convert.Run(context.Background(), req)
	if printErr := printRunResult(res); printErr != nil && err == nil {
		return printErr
	}
	return err
}

func printRunResult(res *convert.Result) error {
	if res == nil {
		return nil
	}
	if res.Content != "" {
		fmt.Print(res.Content)
		if !strings.HasSuffix(res.Content, "\n") {
			fmt.Println()
		}
	}
	for _, p := range res.Paths {
		fmt.Println(p)
	}
	if res.Content == "" && len(res.Paths) == 0 {
		switch res.ToMedia {
		case convert.MediaClipboard:
			fmt.Fprintln(os.Stderr, "Copied to clipboard.")
		case convert.MediaFileReference:
			fmt.Fprintln(os.Stderr, "Placed file reference on clipboard.")
		}
	}
	if res.MediaDir != "" {
		fmt.Fprintln(os.Stderr, "Media dir:", res.MediaDir)
	}
	for _, a := range res.Assets {
		fmt.Fprintln(os.Stderr, "Asset:", a)
	}
	for _, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "Error: %s\n", e)
	}
	return nil
}

func printConvertUsage() {
	fmt.Print(`Usage: vellum convert --from <media> --to <media> [path|-] [options]

Media-orthogonal conversion. Media: file | content | clipboard | file_reference.

Options:
  --from <media>      Source medium (required)
  --to <media>        Sink medium (required)
  --format <fmt>      Source format override (markdown, html, rtf, docx, …)
  --to-format <fmt>   Sink format override (markdown, html, pdf, …)
  -o <path>           Output path (file / file_reference sinks)
  --backend <name>    PDF backend: weasyprint (default) or prince
  --help              Show this help

Examples:
  vellum convert --from file --to file report.md -o report.pdf
  vellum convert --from file --to clipboard report.md
  echo '# Hi' | vellum convert --from content --to clipboard
  vellum convert --from clipboard --to content
  vellum convert --from file --to content notes.docx
  vellum convert --from file --to file_reference report.md -o report.pdf
  vellum convert --from file_reference --to content

Shorthand (also available):
  vellum report.md                  → --from file --to file
  vellum --to-clipboard report.md   → --from file --to clipboard
  vellum import doc.docx            → --from file --to content
  vellum import --from-clipboard    → --from clipboard --to content

Disallowed: --to content|clipboard with --to-format pdf.
clipboard and file_reference require macOS.
`)
}

func runImport(args []string) error {
	// Thin alias → convert --from file|clipboard --to content|file.
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

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	req := &convert.Request{Style: cfg.Style, Backend: cfg.Backend}
	if fromClipboard {
		req.From = convert.Endpoint{Media: convert.MediaClipboard, Format: format}
	} else {
		req.From = convert.Endpoint{Media: convert.MediaFile, Path: positional[0], Format: format}
	}
	if output == "" {
		req.To = convert.Endpoint{Media: convert.MediaContent, Format: convert.FormatMarkdown}
	} else {
		req.To = convert.Endpoint{Media: convert.MediaFile, Path: output, Format: convert.FormatMarkdown}
	}
	res, err := convert.Run(context.Background(), req)
	if printErr := printRunResult(res); printErr != nil && err == nil {
		return printErr
	}
	return err
}

func printImportUsage() {
	fmt.Print(`Usage: vellum import [options] <file>
       vellum import --from-clipboard [options]

Alias for: vellum convert --from file|clipboard --to content|file
(with --to-format markdown). Prefer 'vellum convert' for new scripts.

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
       vellum convert --from <media> --to <media> [path|-]
       vellum --mcp
       vellum import [options] <file>
       vellum view [options] <file.md>
       vellum install-viewer | uninstall-viewer

Document preparation — media-orthogonal conversion (file, content,
clipboard, file_reference), Markdown viewing, and MCP server.

Options:
  --help              Show this help message
  --help-agent        Show this help plus the embedded agent guide
  --version           Print version number
  --mcp               Run as an MCP (Model Context Protocol) server on stdio
  --to-clipboard      Sugar: file|stdin → clipboard (macOS)
  --open              Alias for 'view': render to cache and open (macOS)
  -o <path>           Output path (single input file only)
  --backend <name>    Renderer backend: "weasyprint" (default) or "prince"

Subcommands:
  convert             Media-orthogonal conversion (--from / --to). See
                      "vellum convert --help".
  import              Alias: rich-text → Markdown. See "vellum import --help".
  view                Render a Markdown file to cache and open it
                      (HTML default; --pdf for PDF). See "vellum view --help".
  install-viewer      Install Vellum Viewer.app as the default .md handler
  uninstall-viewer    Remove Vellum Viewer.app

Examples:
  vellum report.md                       # produces report.pdf
  vellum convert --from file --to clipboard report.md
  echo '# Hi' | vellum convert --from content --to clipboard
  vellum convert --from clipboard --to content
  vellum import doc.docx                 # → Markdown on stdout
  vellum view notes.md                   # rendered HTML in browser

Renderer (default WeasyPrint, optional Prince) must be on PATH for PDF
output. pandoc must be on PATH for rich-text import paths.
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

	req := &convert.Request{
		Style:   cfg.Style,
		Backend: backendName,
	}
	if len(args) == 1 {
		req.From = convert.Endpoint{Media: convert.MediaFile, Path: args[0]}
		req.To = convert.Endpoint{Media: convert.MediaFile, Path: output, Format: convert.FormatPDF}
	} else {
		req.From = convert.Endpoint{Media: convert.MediaFile, Paths: args}
		req.To = convert.Endpoint{Media: convert.MediaFile, Format: convert.FormatPDF}
	}
	res, err := convert.Run(context.Background(), req)
	if printErr := printRunResult(res); printErr != nil && err == nil {
		return printErr
	}
	return err
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
