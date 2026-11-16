#!/bin/sh
# Install a vellum LaunchAgent: render launchd/<label>.plist into
# ~/Library/LaunchAgents and (re)load it in the user's GUI domain. launchd
# runs the program directly, so macOS privacy prompts (TCC) name it rather
# than a supervisor. If an old supervisord program of the same name exists,
# this stops it and removes its config first.
#
#   launchd/install.sh                      dev viewer (vellum-view, 18742):
#                                           launchd/run-view.sh, which prefers
#                                           the checkout's ./vellum build
#   launchd/install.sh vellum-view-release  release viewer (18744): the
#                                           Homebrew /opt/homebrew/bin/vellum
#
#   LAUNCHD_SKIP_LOAD=1  render only; do not touch launchd or supervisord
#
# vellum-view-release runs with HOME at ~/.local/state/vellum-release/home.
# serve-view has no cache-dir flag; its view/import/KaTeX caches live under
# os.UserCacheDir() (~/Library/Caches/vellum) and are keyed by source path,
# mtime and size, so the two servers would otherwise serve each other's
# renders. Config (~/.config/vellum) and mmdc's Chromium (~/.cache/puppeteer)
# stay shared through XDG_CONFIG_HOME and PUPPETEER_CACHE_DIR. The Homebrew
# formula's service block cannot set that HOME, hence a separate agent.
# Port 18744 because 18743 is bullseye's default MCP port. Post-release
# check: scripts/post-release-check.sh.
set -eu

NAME="${1:-vellum-view}"
case "$NAME" in
  vellum-view | vellum-view-release) ;;
  *)
    echo "usage: $0 [vellum-view|vellum-view-release]" >&2
    exit 2
    ;;
esac
LABEL="com.marcelocantos.$NAME"
REPO="$(CDPATH='' cd "$(dirname "$0")/.." && pwd)"
: "${HOME:?HOME is not set}"
AGENTS="$HOME/Library/LaunchAgents"
DEST="$AGENTS/$LABEL.plist"
DOMAIN="gui/$(id -u)"

mkdir -p "$AGENTS" "$HOME/.local/var/log"
if [ "$NAME" = vellum-view-release ]; then
  # launchd does not create WorkingDirectory or the private HOME.
  mkdir -p "$HOME/.local/state/vellum-release/home" \
    "$HOME/.local/state/vellum-release/import-cache"
fi
tmp="$(mktemp "$AGENTS/.$LABEL.XXXXXX")"
trap 'rm -f "$tmp"' EXIT HUP INT TERM
sed -e "s|@REPO@|$REPO|g" -e "s|@HOME@|$HOME|g" -e "s|@USER@|$(id -un)|g" \
  "$REPO/launchd/$LABEL.plist" > "$tmp"
plutil -lint "$tmp" >/dev/null
chmod 644 "$tmp"
mv "$tmp" "$DEST"
echo "rendered $DEST"

if [ "${LAUNCHD_SKIP_LOAD:-}" = 1 ]; then
  exit 0
fi

old=/opt/homebrew/etc/supervisor.d/$NAME.ini
if [ -f "$old" ]; then
  if command -v supervisorctl >/dev/null 2>&1; then
    supervisorctl stop "$NAME" || true
  fi
  rm -f "$old"
  if command -v supervisorctl >/dev/null 2>&1; then
    supervisorctl update >/dev/null || true
  fi
  echo "removed supervisord program $NAME"
fi

# The Homebrew service (serve-view) would compete for 127.0.0.1:18742.
if [ "$NAME" = vellum-view ] && command -v brew >/dev/null 2>&1; then
  brew services stop vellum 2>/dev/null || true
fi

launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
# bootstrap can briefly fail while the old instance is still being torn down.
n=0
until launchctl bootstrap "$DOMAIN" "$DEST" 2>/dev/null; do
  n=$((n + 1))
  if [ "$n" -ge 10 ]; then
    launchctl bootstrap "$DOMAIN" "$DEST"
  fi
  sleep 1
done
launchctl print "$DOMAIN/$LABEL" | grep -E '^	(state|pid) = '
