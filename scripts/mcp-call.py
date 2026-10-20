#!/usr/bin/env python3
"""Minimal streamable-HTTP MCP client for scripts/verify-release.sh.

usage: mcp-call.py URL METHOD [PARAMS_JSON]

Runs initialize + notifications/initialized (carrying Mcp-Session-Id when
the server issues one), then METHOD, and prints the JSON-RPC `result` of
that call as JSON on stdout. METHOD "initialize" prints the initialize
result itself. Exits 1 on transport or JSON-RPC error. Stdlib only.
"""
import json
import sys
import urllib.request

PROTOCOL = "2025-06-18"


def post(url, payload, session=None):
    headers = {
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
        "MCP-Protocol-Version": PROTOCOL,
    }
    if session:
        headers["Mcp-Session-Id"] = session
    req = urllib.request.Request(url, data=json.dumps(payload).encode(), headers=headers)
    with urllib.request.urlopen(req, timeout=180) as resp:
        sid = resp.headers.get("Mcp-Session-Id") or session
        body = resp.read().decode()
    if "id" not in payload:
        return None, sid
    # JSON, or SSE with the reply in a `data:` line.
    candidates = [l[5:].strip() for l in body.splitlines() if l.startswith("data:")] or [body]
    for c in candidates:
        try:
            msg = json.loads(c)
        except ValueError:
            continue
        if msg.get("id") == payload["id"]:
            return msg, sid
    raise RuntimeError("no JSON-RPC reply in: " + body[:500])


def main():
    if len(sys.argv) < 3:
        print(__doc__, file=sys.stderr)
        return 2
    url, method = sys.argv[1], sys.argv[2]
    params = json.loads(sys.argv[3]) if len(sys.argv) > 3 else {}
    init = {
        "jsonrpc": "2.0", "id": 1, "method": "initialize",
        "params": {"protocolVersion": PROTOCOL, "capabilities": {},
                   "clientInfo": {"name": "vellum-verify-release", "version": "0"}},
    }
    try:
        msg, sid = post(url, init)
        if "error" in msg:
            raise RuntimeError(json.dumps(msg["error"]))
        if method == "initialize":
            print(json.dumps(msg["result"]))
            return 0
        post(url, {"jsonrpc": "2.0", "method": "notifications/initialized"}, sid)
        msg, _ = post(url, {"jsonrpc": "2.0", "id": 2, "method": method, "params": params}, sid)
        if "error" in msg:
            raise RuntimeError(json.dumps(msg["error"]))
        print(json.dumps(msg["result"]))
        return 0
    except Exception as e:  # noqa: BLE001 — any failure is a check failure
        print("mcp-call: %s" % e, file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
