#!/usr/bin/env bash
# Create GitHub release with dist/ assets, update the Homebrew tap and the
# scoop manifest (bucket/vellum.json), upgrade locally.
# Run after cv gate (cv release runs gate first).
set -euo pipefail
source "$(dirname "$0")/release-common.sh"

TAG="$(release_tag)"
VERSION="$(release_version)"
NOTES_FILE=""
SKIP_TAP=false
SKIP_BREW=false

usage() {
	echo "usage: $0 [--notes FILE] [--skip-tap] [--skip-brew]" >&2
	exit 2
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--notes)
		NOTES_FILE=$2
		shift 2
		;;
	--skip-tap)
		SKIP_TAP=true
		shift
		;;
	--skip-brew)
		SKIP_BREW=true
		shift
		;;
	-h | --help)
		usage
		;;
	*)
		echo "unknown argument: $1" >&2
		usage
		;;
	esac
done

require_gh

for f in "dist/vellum-${VERSION}-darwin-arm64.tar.gz" \
	"dist/vellum-${VERSION}-windows-arm64.zip" \
	"dist/vellum-${VERSION}-windows-amd64.zip"; do
	[[ -f "$f" ]] || {
		echo "missing $f — run cv release-dist first" >&2
		exit 1
	}
done

if gh release view "$TAG" >/dev/null 2>&1; then
	echo "release-publish: ${TAG} already exists on GitHub" >&2
	exit 1
fi

notes_tmp=""
if [[ -z "$NOTES_FILE" && -f ".release-notes-${TAG}.md" ]]; then
	NOTES_FILE=".release-notes-${TAG}.md"
fi
if [[ -z "$NOTES_FILE" ]]; then
	notes_tmp="$(mktemp)"
	NOTES_FILE="$notes_tmp"
	if prev="$(git describe --tags --abbrev=0 2>/dev/null)"; then
		git log "${prev}..HEAD" --pretty=format:'- %s' >"$NOTES_FILE" || true
	fi
	[[ -s "$NOTES_FILE" ]] || echo "Release ${TAG}." >"$NOTES_FILE"
fi

if [[ -n "$notes_tmp" ]]; then
	trap 'rm -f "$notes_tmp"' EXIT
fi

echo "release-publish: creating ${TAG} …" >&2
gh release create "$TAG" --title "$TAG" --notes-file "$NOTES_FILE" \
	dist/*.tar.gz dist/*.zip "dist/checksums-${VERSION}.txt"
git fetch --tags

if [[ "$SKIP_TAP" == false ]]; then
	"$(dirname "$0")/release-tap.sh" "$TAG"
fi

"$(dirname "$0")/release-scoop.sh"
echo "release-publish: commit bucket/vellum.json (scoop manifest) after cv gate" >&2

if [[ "$SKIP_BREW" == false ]]; then
	echo "release-publish: brew upgrade …" >&2
	brew update
	brew upgrade marcelocantos/tap/vellum 2>/dev/null || brew install marcelocantos/tap/vellum
	got="$(vellum --version 2>/dev/null || true)"
	if [[ "$got" != "$VERSION" ]]; then
		echo "release-publish: expected vellum --version ${VERSION}, got ${got:-<missing>}" >&2
		exit 1
	fi
	# A release that fails end-to-end on the installed binary is broken.
	echo "release-publish: make verify-release VERSION=${VERSION} …" >&2
	"$(dirname "$0")/verify-release.sh" "$VERSION"
else
	echo "release-publish: brew skipped — after brew upgrade vellum, run: make verify-release VERSION=${VERSION}" >&2
fi

echo "release-publish: ${TAG} → https://github.com/marcelocantos/vellum/releases/tag/${TAG}"
