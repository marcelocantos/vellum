depends_on "mermaid-cli"
depends_on "node"
depends_on "pandoc"
depends_on "poppler"
depends_on "weasyprint"

service do
  run [opt_bin/"vellum", "serve-view"]
  keep_alive true
  log_path var/"log/vellum-view.log"
  error_log_path var/"log/vellum-view.log"
  working_dir var
end

def caveats
  <<~EOS
    vellum is installed with WeasyPrint as the default renderer (BSD-3, open-source).

    To opt into Prince (proprietary, slightly better typography), either:
      - install it from https://www.princexml.com/download/ (free with watermark
        for non-commercial use; commercial licence required otherwise), and
      - set "backend: prince" in ~/.config/vellum/config.yaml, or pass
        --backend prince on the CLI, or supply "backend": "prince" in an
        MCP tool call.

    KaTeX is needed for math rendering:
      npm install -g katex

    Pandoc handles rich-text import (RTF, DOCX, HTML, … → Markdown
    with media extraction). Poppler (pdftoppm/pdftotext) handles PDF
    import (page images + text). Both are formula dependencies.

    Localhost daemon (Markdown view + HTTP MCP at /mcp):
      brew services start vellum
    Binds 127.0.0.1:18742 only. Override with VELLUM_VIEW_ADDR.
    Confirm with: lsof -iTCP:18742 -sTCP:LISTEN
    Do not probe /mcp with bare curl.
    MCP clients: http://127.0.0.1:18742/mcp
      claude mcp add --scope user --transport http vellum http://127.0.0.1:18742/mcp
    Restart the agent session after registration.
  EOS
end
