bin.install "vellum" => "vellum-bin"
(bin/"vellum").write <<~SH
  #!/bin/sh
  # Prepend the canonical tool dirs so node, mmdc, and prince
  # resolve regardless of how vellum is launched (terminal,
  # MCP client with a stripped PATH inherited from launchd, …).
  export PATH="#{HOMEBREW_PREFIX}/bin:#{HOMEBREW_PREFIX}/sbin:/usr/local/bin:$HOME/.cargo/bin:$HOME/.local/bin:$HOME/.py/bin:$HOME/go/bin:$PATH"
  exec "#{opt_bin}/vellum-bin" "$@"
SH
(bin/"vellum").chmod 0755
