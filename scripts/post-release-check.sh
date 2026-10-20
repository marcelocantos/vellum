#!/usr/bin/env bash
# Post-release check for Colossus: the step after `brew upgrade vellum`.
# A failure here is a broken release.
#
# Restarts the supervisord program that runs the *installed* vellum
# (vellum-view-release: /opt/homebrew/bin/vellum serve-view on
# 127.0.0.1:18744, beside the dev build in vellum-view on 18742), checks it
# stays up with the same pid, and checks that MCP initialize on its /mcp
# reports the expected version.
#
# usage: scripts/post-release-check.sh [expected-version]
#   expected-version defaults to `/opt/homebrew/bin/vellum --version`.
# env:
#   VELLUM_RELEASE_PROGRAM  supervisord program (default vellum-view-release)
#   VELLUM_RELEASE_ADDR     host:port it serves (default 127.0.0.1:18744)
#   VELLUM_RELEASE_SETTLE   seconds it must stay up (default 60)
set -uo pipefail

PROGRAM="${VELLUM_RELEASE_PROGRAM:-vellum-view-release}"
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

command -v supervisorctl >/dev/null || fail "supervisorctl not on PATH"

# pid of $PROGRAM if RUNNING, else empty.
running_pid() {
	supervisorctl status "$PROGRAM" 2>/dev/null |
		awk '$2 == "RUNNING" { for (i = 3; i <= NF; i++) if ($i == "pid") { sub(",", "", $(i + 1)); print $(i + 1) } }'
}

echo "post-release-check: restarting ${PROGRAM} (expect vellum ${EXPECTED} on ${ADDR})"
supervisorctl restart "$PROGRAM" >/dev/null 2>&1 || true
pid="$(running_pid)"
[[ -n "$pid" ]] || fail "${PROGRAM} is not RUNNING after restart: $(supervisorctl status "$PROGRAM" 2>&1)"
echo "post-release-check: ${PROGRAM} RUNNING pid ${pid}; waiting ${SETTLE}s"

sleep "$SETTLE"
pid2="$(running_pid)"
[[ -n "$pid2" ]] || fail "${PROGRAM} stopped within ${SETTLE}s: $(supervisorctl status "$PROGRAM" 2>&1)"
[[ "$pid2" == "$pid" ]] || fail "${PROGRAM} restarted within ${SETTLE}s (pid ${pid} → ${pid2})"
echo "post-release-check: still RUNNING pid ${pid2} after ${SETTLE}s"

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

echo "post-release-check: PASS (${PROGRAM} pid ${pid}, vellum ${EXPECTED} on ${ADDR})"
