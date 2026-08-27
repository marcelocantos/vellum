#!/usr/bin/env bash
# Update the Homebrew tap declared in tapper.yaml for an existing GitHub release.
set -euo pipefail
source "$(dirname "$0")/release-common.sh"

TAG="${1:-$(release_tag)}"
command -v tapper >/dev/null || {
	echo "tapper not on PATH — go install github.com/marcelocantos/tapper/cmd/tapper@latest" >&2
	exit 1
}
exec tapper push --version "$TAG"
