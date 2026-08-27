#!/usr/bin/env bash
# Build release tarballs into dist/ (darwin/arm64 + linux/amd64 + linux/arm64).
set -euo pipefail
source "$(dirname "$0")/release-common.sh"

VERSION="$(release_version)"
DIST="$ROOT/dist"
BUILD="$DIST/build"

rm -rf "$DIST"
mkdir -p "$BUILD"

build_one() {
	local goos=$1 goarch=$2 cgo=$3 asset_os=$4 asset_arch=$5
	local out asset

	out="$BUILD/vellum"
	asset="vellum-${VERSION}-${asset_os}-${asset_arch}.tar.gz"

	CGO_ENABLED="$cgo" GOOS="$goos" GOARCH="$goarch" \
		go build -trimpath -ldflags="-s -w" -o "$out" ./cmd/vellum
	tar -czf "$DIST/$asset" -C "$BUILD" vellum -C "$ROOT" LICENSE README.md
	rm -f "$out"
	echo "wrote dist/$asset"
}

build_one darwin arm64 1 darwin arm64
build_one linux amd64 0 linux amd64
build_one linux arm64 0 linux arm64

echo "release-package: v${VERSION} → dist/"
