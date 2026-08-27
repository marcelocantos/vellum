#!/usr/bin/env bash
# Shared helpers for local release (package, publish).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

release_version() {
	sed -n 's/^const version = "\([0-9][0-9.]*\)"/\1/p' cmd/vellum/main.go
}

release_tag() {
	printf 'v%s' "$(release_version)"
}

require_gh() {
	command -v gh >/dev/null || { echo "gh CLI required" >&2; exit 1; }
	gh auth status >/dev/null 2>&1 || { echo "gh auth login required" >&2; exit 1; }
}
