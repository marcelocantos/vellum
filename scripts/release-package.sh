#!/usr/bin/env bash
# Build release archives into dist/: darwin-arm64 tarball (Homebrew) and
# windows-arm64 / windows-amd64 zips (scoop, manual install). No Linux.
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
	tar -czf "$DIST/$asset" -C "$BUILD" vellum -C "$ROOT" LICENSE README.md NOTICE
	rm -f "$out"
	echo "wrote dist/$asset"
}

build_zip() {
	local goarch=$1
	local dir asset
	dir="$BUILD/windows-$goarch"
	asset="vellum-${VERSION}-windows-${goarch}.zip"
	mkdir -p "$dir"
	CGO_ENABLED=0 GOOS=windows GOARCH="$goarch" \
		go build -trimpath -ldflags="-s -w" -o "$dir/vellum.exe" ./cmd/vellum
	cp "$ROOT/LICENSE" "$ROOT/README.md" "$ROOT/NOTICE" "$dir/"
	(cd "$dir" && zip -q -X "$DIST/$asset" vellum.exe LICENSE README.md NOTICE)
	rm -rf "$dir"
	echo "wrote dist/$asset"
}

build_one darwin arm64 1 darwin arm64
build_zip arm64
build_zip amd64
(cd "$DIST" && shasum -a 256 vellum-"${VERSION}"-* >"checksums-${VERSION}.txt")
echo "wrote dist/checksums-${VERSION}.txt"

echo "release-package: v${VERSION} → dist/"
