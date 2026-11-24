#!/usr/bin/env bash
# Build release archives into dist/: darwin-arm64 tarball (Homebrew) and
# windows-arm64 / windows-amd64 zips (scoop, manual install). No Linux.
#
# The macOS binary is signed with the Developer ID in the login keychain
# (hardened runtime, secure timestamp). Packaging fails without it: an
# unsigned release is never written.
#
# env:
#   VELLUM_SIGN_IDENTITY  codesign identity (default: SIGN_IDENTITY in
#                         release-signing.sh). Its team must be SIGN_TEAM.
set -euo pipefail
source "$(dirname "$0")/release-common.sh"

VERSION="$(release_version)"
DIST="$ROOT/dist"
BUILD="$DIST/build"

rm -rf "$DIST"
mkdir -p "$BUILD"

# sign_darwin BINARY: sign it, then read the signature back. The linker
# leaves an ad-hoc signature named "a.out" with no team; macOS keys privacy
# grants on the signing team, so only a team-signed binary keeps them from
# one release to the next.
sign_darwin() {
	local bin=$1 ident desc
	ident="${VELLUM_SIGN_IDENTITY:-$SIGN_IDENTITY}"
	if ! security find-identity -v -p codesigning | grep -qF "\"$ident\""; then
		echo "release-package: no codesigning identity \"$ident\" in the keychain; refusing to package an unsigned binary" >&2
		exit 1
	fi
	codesign --force --options runtime --timestamp \
		--identifier "$SIGN_IDENTIFIER" --sign "$ident" "$bin"
	codesign --verify --strict --verbose=2 "$bin"
	desc="$(codesign --display --verbose=2 "$bin" 2>&1)"
	if ! grep -qx "TeamIdentifier=$SIGN_TEAM" <<<"$desc" ||
		! grep -qx "Identifier=$SIGN_IDENTIFIER" <<<"$desc" ||
		! grep -q '^CodeDirectory .*flags=.*runtime' <<<"$desc" ||
		! grep -q '^Timestamp=' <<<"$desc"; then
		echo "release-package: signature is not team $SIGN_TEAM, identifier $SIGN_IDENTIFIER, hardened and timestamped:" >&2
		echo "$desc" >&2
		exit 1
	fi
	echo "signed $(basename "$bin"): $ident"
}

build_one() {
	local goos=$1 goarch=$2 cgo=$3 asset_os=$4 asset_arch=$5
	local out asset

	out="$BUILD/vellum"
	asset="vellum-${VERSION}-${asset_os}-${asset_arch}.tar.gz"

	# darwin cgo builds use Apple's clang unless CC is set: Homebrew llvm's
	# clang (often first on PATH) can target an SDK CLT does not ship.
	local ccenv=()
	if [[ "$goos" == darwin && -z "${CC:-}" ]]; then
		ccenv=(CC=/usr/bin/clang)
	fi
	env ${ccenv[@]+"${ccenv[@]}"} CGO_ENABLED="$cgo" GOOS="$goos" GOARCH="$goarch" \
		go build -trimpath -ldflags="-s -w" -o "$out" ./cmd/vellum
	if [[ "$goos" == darwin ]]; then
		sign_darwin "$out"
	fi
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
