# vellum agent guide

vellum is a Markdown-to-PDF tool that renders GitHub-flavoured Markdown
(with KaTeX math and Mermaid diagrams) to typeset PDFs via Prince. It
runs either as a command-line tool or as an MCP (Model Context Protocol)
server over stdio.

## Preferred invocation

When running inside an AI agent, prefer the MCP interface over the CLI.
Start vellum with `vellum --mcp` and call the `convert` tool. The CLI
(`vellum <input.md>`) is fine for interactive use but the MCP tool
returns structured results (converted paths, per-file errors) that are
easier to consume programmatically.

## Tool schema

The MCP server exposes one tool:

- `convert({ files: [{ input, output? }] })`
  - `input` — absolute path to a `.md` file (required)
  - `output` — absolute path for the output PDF (optional; defaults to
    the input path with its extension replaced by `.pdf`)

Example call:

```json
{
  "name": "convert",
  "arguments": {
    "files": [
      {"input": "/abs/path/to/report.md"},
      {"input": "/abs/path/to/spec.md", "output": "/abs/path/to/out/spec.pdf"}
    ]
  }
}
```

Response shape:

```json
{
  "converted": ["/abs/path/to/report.pdf", "/abs/path/to/out/spec.pdf"],
  "errors": []
}
```

## Input rules

- Input paths must be absolute. Relative paths are resolved against the
  server's working directory but this is fragile — always pass absolute
  paths.
- Input files should have a `.md` extension.
- The caller decides the output path. If omitted, vellum writes
  `<input>.pdf` next to the input file.
- Multiple files can be converted in a single call; each is processed
  independently and errors are reported per-file.

## Mermaid scale hint

Mermaid diagrams can overflow the PDF page when they are dense. To
scale a specific diagram, add a `vellum:scale` HTML comment immediately
before the fenced code block:

    <!-- vellum:scale 0.75 -->
    ```mermaid
    graph LR
      A --> B --> C
    ```

The scale is a CSS scale factor (1.0 = default). Values below 1.0
shrink the diagram; values above 1.0 enlarge it. Only use this when a
diagram does not fit; most diagrams render correctly at 1.0.

## Security notes

- vellum invokes three external binaries: `prince` (HTML to PDF),
  `node` (KaTeX math rendering), and `mmdc` (Mermaid diagrams). All
  rendering happens locally; no data is sent to external services.
- Prince's JavaScript engine is not enabled by default, so arbitrary
  JS in HTML will not execute during typesetting.
- KaTeX runs in `throwOnError: false` mode, so malformed math
  expressions render as an error span rather than crashing the build.
- Mermaid and math rendering are performed server-side before Prince
  sees the document, so the final HTML contains only static SVG/HTML.

## Error handling

If a conversion fails, the error message includes the underlying
tool's stderr output (from prince, node, or mmdc). Report it verbatim
to the user — the stderr text is the most useful diagnostic.

Common failure modes:

- A required dependency is missing. vellum checks `prince`, `node`,
  and `mmdc` on PATH at startup and lists any missing tools with
  install instructions.
- The `katex` node package is not installed globally. Fix with
  `npm install -g katex`.
- A Mermaid diagram has invalid syntax. The `mmdc` error text is
  included in the returned error.
- Prince cannot fit content onto a page. This usually means an oversized
  image, table, or Mermaid diagram — try the `vellum:scale` hint.
