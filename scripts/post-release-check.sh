#!/usr/bin/env bash
# Post-release check for Colossus: the step after `brew upgrade vellum`.
# A failure here is a broken release.
#
# Restarts the LaunchAgent that runs the *installed* vellum
# (com.marcelocantos.vellum-view-release: /opt/homebrew/bin/vellum
# serve-view on 127.0.0.1:18744, beside the dev build in vellum-view on
# 18742; launchd/install.sh vellum-view-release), checks it stays up with
# the same pid, and checks that MCP initialize on its /mcp reports the
# expected version.
#
# usage: scripts/post-release-check.sh [expected-version]
#   expected-version defaults to `/opt/homebrew/bin/vellum --version`.
# env:
#   VELLUM_RELEASE_PROGRAM  program; the LaunchAgent label is
#                           com.marcelocantos.<program> (default vellum-view-release)
#   VELLUM_RELEASE_ADDR     host:port it serves (default 127.0.0.1:18744)
#   VELLUM_RELEASE_SETTLE   seconds it must stay up (default 60)
set -uo pipefail

PROGRAM="${VELLUM_RELEASE_PROGRAM:-vellum-view-release}"
LABEL="com.marcelocantos.${PROGRAM}"
TARGET="gui/$(id -u)/${LABEL}"
ADDR="${VELLUM_RELEASE_ADDR:-127.0.0.1:18744}"
SETTLE="${VELLUM_RELEASE_SETTLE:-60}"

fail() {
	echo "post-release-check: FAIL — $*" >&2
	exit 1
}

if [[ $# -gt 1 ]]; then
	echo "usage: $0 [expected-version]" >&2
	exit 2
fi
EXPECTED="${1:-}"
if [[ -z "$EXPECTED" ]]; then
	EXPECTED="$(/opt/homebrew/bin/vellum --version 2>/dev/null)" ||
		fail "/opt/homebrew/bin/vellum --version did not run"
fi
[[ -n "$EXPECTED" ]] || fail "no expected version"

command -v launchctl >/dev/null || fail "launchctl not on PATH"

# pid of $LABEL if running, else empty.
running_pid() {
	launchctl list | awk -v l="$LABEL" '$3 == l && $1 != "-" { print $1 }'
}

# One-line launchd state of $LABEL, for failure messages.
status() {
	launchctl print "$TARGET" 2>&1 |
		awk 'NR == 1 { first = $0 }
			/^\t(state|pid|last exit code) = / { sub(/^\t/, ""); out = out $0 "; " }
			END { print (out != "" ? out : first) }'
}

# The pid after a restart, waiting up to 10s for launchd to spawn it.
wait_pid() {
	local p i
	for ((i = 0; i < 20; i++)); do
		p="$(running_pid)"
		if [[ -n "$p" ]]; then
			printf '%s\n' "$p"
			return
		fi
		sleep 0.5
	done
}

echo "post-release-check: restarting ${LABEL} (expect vellum ${EXPECTED} on ${ADDR})"
launchctl kickstart -k "$TARGET" >/dev/null 2>&1 ||
	fail "launchctl kickstart -k ${TARGET} failed: $(status)"
pid="$(wait_pid)"
[[ -n "$pid" ]] || fail "${LABEL} is not running after restart: $(status)"
echo "post-release-check: ${LABEL} running pid ${pid}; waiting ${SETTLE}s"

sleep "$SETTLE"
pid2="$(running_pid)"
[[ -n "$pid2" ]] || fail "${LABEL} stopped within ${SETTLE}s: $(status)"
[[ "$pid2" == "$pid" ]] || fail "${LABEL} restarted within ${SETTLE}s (pid ${pid} → ${pid2})"
echo "post-release-check: still running pid ${pid2} after ${SETTLE}s"

body='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"post-release-check","version":"0"}}}'
resp="$(curl -sS -m 15 -X POST "http://${ADDR}/mcp" \
	-H 'Content-Type: application/json' \
	-H 'Accept: application/json, text/event-stream' \
	-d "$body" 2>&1)" || fail "MCP initialize on http://${ADDR}/mcp failed: ${resp}"

# The reply is JSON or a single SSE `data:` event carrying JSON.
got="$(printf '%s\n' "$resp" | python3 -c '
import json, sys
text = sys.stdin.read()
lines = [l[5:].strip() for l in text.splitlines() if l.startswith("data:")] or [text]
for l in lines:
    try:
        info = json.loads(l)["result"]["serverInfo"]
    except Exception:
        continue
    print(info.get("name", ""), info.get("version", ""))
    break
')"
[[ -n "$got" ]] || fail "no serverInfo in MCP initialize reply: ${resp}"
echo "post-release-check: MCP serverInfo: ${got}"
[[ "$got" == "vellum ${EXPECTED}" ]] || fail "expected serverInfo 'vellum ${EXPECTED}', got '${got}'"

echo "post-release-check: PASS (${LABEL} pid ${pid}, vellum ${EXPECTED} on ${ADDR})"
