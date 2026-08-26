#!/bin/sh
# Start vellum serve-view for supervisord. Prefers a repo-root ./vellum build,
# then the Homebrew install, then whatever is on PATH.
set -e

ROOT="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"

# Supervisord starts programs with a stripped environment. Match the Homebrew
# vellum wrapper PATH so mmdc, node, katex, prince, … resolve.
if [ -z "${HOME:-}" ]; then
  HOME="$(eval echo ~"$(id -un)")"
  export HOME
fi
export USER="${USER:-$(id -un)}"
export PATH="/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:${HOME}/.cargo/bin:${HOME}/.local/bin:${HOME}/.py/bin:${HOME}/go/bin:/usr/bin:/bin:/usr/sbin:/sbin"

if [ -x "$ROOT/vellum" ]; then
  exec "$ROOT/vellum" serve-view
fi
if [ -x "/opt/homebrew/opt/vellum/bin/vellum" ]; then
  exec /opt/homebrew/opt/vellum/bin/vellum serve-view
fi
exec vellum serve-view
