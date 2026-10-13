#!/usr/bin/env bash
# Write the scoop manifest bucket/vellum.json for the current version from
# the windows zips in dist/. The vellum repo doubles as its own scoop bucket:
#   scoop bucket add vellum https://github.com/marcelocantos/vellum
#   scoop install vellum/vellum
# tapper only speaks Homebrew, so scoop is handled here.
set -euo pipefail
source "$(dirname "$0")/release-common.sh"

VERSION="$(release_version)"
TAG="$(release_tag)"
base="https://github.com/marcelocantos/vellum/releases/download/${TAG}"
hash() {
	local f="dist/vellum-${VERSION}-windows-$1.zip"
	[[ -f "$f" ]] || { echo "missing $f — run cv release-dist first" >&2; exit 1; }
	shasum -a 256 "$f" | cut -d' ' -f1
}
arm64="$(hash arm64)"
amd64="$(hash amd64)"
mkdir -p bucket
cat >bucket/vellum.json <<JSON
{
    "version": "${VERSION}",
    "description": "Document preparation: Markdown to PDF/HTML/PowerPoint, rich-text import, clipboard, and a localhost Markdown viewer with MCP.",
    "homepage": "https://github.com/marcelocantos/vellum",
    "license": "Apache-2.0",
    "architecture": {
        "64bit": {
            "url": "${base}/vellum-${VERSION}-windows-amd64.zip",
            "hash": "${amd64}"
        },
        "arm64": {
            "url": "${base}/vellum-${VERSION}-windows-arm64.zip",
            "hash": "${arm64}"
        }
    },
    "bin": "vellum.exe",
    "suggest": {
        "pandoc": "main/pandoc",
        "nodejs": "main/nodejs-lts"
    },
    "notes": [
        "vellum also needs WeasyPrint (pip install weasyprint, with Pango from MSYS2), poppler,",
        "and npm install -g katex@0.16.45 @mermaid-js/mermaid-cli. Run 'vellum install-service'",
        "to start the view server at logon."
    ],
    "checkver": "github",
    "autoupdate": {
        "architecture": {
            "64bit": {
                "url": "https://github.com/marcelocantos/vellum/releases/download/v\$version/vellum-\$version-windows-amd64.zip"
            },
            "arm64": {
                "url": "https://github.com/marcelocantos/vellum/releases/download/v\$version/vellum-\$version-windows-arm64.zip"
            }
        }
    }
}
JSON
python3 -m json.tool bucket/vellum.json >/dev/null
echo "wrote bucket/vellum.json (${TAG})"
