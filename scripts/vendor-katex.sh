#!/usr/bin/env bash
# Copy KaTeX's stylesheet and woff2 fonts from the installed katex npm
# package into embed/katex/, so vellum ships the CSS that matches the
# renderer it shells out to (convert/katex.go resolves the same package).
#
# Usage: scripts/vendor-katex.sh [path/to/node_modules/katex]
#
# Only woff2 is vendored; the woff and ttf fallbacks are dropped from the
# @font-face rules because every consumer (WeasyPrint, Prince, browsers)
# reads woff2. TestKaTeXAssetsSelfContained checks the result.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
pkg="${1:-$(npm root -g)/katex}"
dist="$pkg/dist"
dest="$root/embed/katex"

[[ -f "$dist/katex.min.css" ]] || { echo "no katex.min.css under $dist" >&2; exit 1; }
version="$(node -e 'console.log(require(process.argv[1]).version)' "$pkg/package.json")"

tmp="$(mktemp -d "$root/embed/.katex.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/fonts"
sed -E 's/,url\(fonts\/[^)]*\.woff\) format\("woff"\),url\(fonts\/[^)]*\.ttf\) format\("truetype"\)//g' \
  "$dist/katex.min.css" > "$tmp/katex.min.css"
cp "$dist"/fonts/*.woff2 "$tmp/fonts/"
cp "$pkg/LICENSE" "$tmp/LICENSE"
printf '%s\n' "$version" > "$tmp/VERSION"

rm -rf "$dest"
mv "$tmp" "$dest"
trap - EXIT
echo "vendored KaTeX $version into embed/katex ($(ls "$dest/fonts" | wc -l | tr -d ' ') fonts)"
