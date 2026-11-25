#!/bin/sh
# Start vellum serve-view for launchd. Prefers a repo-root ./vellum build,
# then the Homebrew install, then whatever is on PATH.
set -e

ROOT="$(CDPATH='' cd "$(dirname "$0")/.." && pwd)"

# launchd starts programs with a stripped environment. Match the Homebrew
# vellum wrapper PATH so mmdc, node, katex, prince, … resolve.
if [ -z "${HOME:-}" ]; then
  HOME="$(eval echo ~"$(id -un)")"
  export HOME
fi
export USER="${USER:-$(id -un)}"
export PATH="/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:${HOME}/.cargo/bin:${HOME}/.local/bin:${HOME}/.py/bin:${HOME}/go/bin:/usr/bin:/bin:/usr/sbin:/sbin"

# The T3 Code listener defaults to the view port plus one, which bullseye
# holds on this machine (127.0.0.1:18743).
export VELLUM_T3_ADDR="${VELLUM_T3_ADDR:-127.0.0.1:18747}"

if [ -x "$ROOT/vellum" ]; then
  # Give the checkout's build a stable signature before running it
  # (🎯T86). The Go linker signs ad hoc as "a.out", so macOS identifies
  # the build by its hash, and every rebuild is a new app to its privacy
  # controls: on 24-25 Nov 2026 four rebuilds raised four Removable
  # Volumes prompts. While one waits unanswered, every process's access
  # to that volume waits behind it, mnemo's backups included. Signed with
  # the release's Developer ID under its own identifier, an Allow given
  # once holds across rebuilds. Failing to sign is not fatal; the viewer
  # starts as before and the log says why.
  SIGN_IDENTITY="" SIGN_TEAM="" SIGN_IDENTIFIER=""
  # shellcheck source=scripts/release-signing.sh
  [ -r "$ROOT/scripts/release-signing.sh" ] && . "$ROOT/scripts/release-signing.sh"
  DEV_IDENTIFIER="$SIGN_IDENTIFIER.dev"
  sig="$(codesign --display --verbose=2 "$ROOT/vellum" 2>&1 || true)"
  if [ -z "$SIGN_IDENTITY" ]; then
    echo "run-view: $ROOT/scripts/release-signing.sh names no signing identity;" \
      "running $ROOT/vellum as built" >&2
  elif ! codesign --verify --strict "$ROOT/vellum" >/dev/null 2>&1 ||
    ! printf '%s\n' "$sig" | grep -qx "Identifier=$DEV_IDENTIFIER" ||
    ! printf '%s\n' "$sig" | grep -qx "TeamIdentifier=$SIGN_TEAM"; then
    if out="$(codesign --force --timestamp=none --sign "$SIGN_IDENTITY" \
      --identifier "$DEV_IDENTIFIER" "$ROOT/vellum" 2>&1)"; then
      echo "run-view: signed $ROOT/vellum as $DEV_IDENTIFIER (team $SIGN_TEAM)"
    else
      echo "run-view: could not sign $ROOT/vellum as $DEV_IDENTIFIER; macOS may ask" \
        "for its privacy permissions again after each rebuild: $out" >&2
    fi
  fi
  exec "$ROOT/vellum" serve-view
fi
if [ -x "/opt/homebrew/opt/vellum/bin/vellum" ]; then
  exec /opt/homebrew/opt/vellum/bin/vellum serve-view
fi
exec vellum serve-view
