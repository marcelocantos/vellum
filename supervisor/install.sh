#!/bin/sh
# Render supervisor/vellum-view.ini into supervisor.d and (re)start vellum-view.
set -e

REPO="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
CONF_DIR="${SUPERVISOR_CONF_DIR:-/opt/homebrew/etc/supervisor.d}"
DEST="$CONF_DIR/vellum-view.ini"
TEMPLATE="$REPO/supervisor/vellum-view.ini"

mkdir -p "$CONF_DIR"
mkdir -p "${HOME}/.local/var/log"
chmod +x "$REPO/supervisor/run-view.sh"
rm -f "$DEST"

sed "s|@REPO@|$REPO|g" "$TEMPLATE" >"$DEST"

if command -v brew >/dev/null 2>&1; then
  brew services stop vellum 2>/dev/null || true
fi

supervisorctl reread
supervisorctl update
supervisorctl restart vellum-view 2>/dev/null || supervisorctl start vellum-view

echo "vellum-view installed at $DEST (from $TEMPLATE)"
supervisorctl status vellum-view
