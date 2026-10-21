#!/usr/bin/env bash
# End-to-end verification of the INSTALLED vellum release on Colossus —
# the step after `brew upgrade vellum` (`make verify-release`). A failure
# is a broken release.
#
# Uses only the Homebrew install: /opt/homebrew/bin/vellum (its
# vellum-bin for the fail-fast check, see below) and the supervisord
# program vellum-view-release (127.0.0.1:18744). Never ./vellum.
#
# usage: scripts/verify-release.sh [expected-version]
#   expected-version: default $VERSION, else the latest git tag (v stripped).
# env: VELLUM_RELEASE_ADDR (default 127.0.0.1:18744), VELLUM_RELEASE_SETTLE
#   (seconds vellum-view-release must stay up, default 60), VERSION.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
VELLUM=/opt/homebrew/bin/vellum
# The brew wrapper prepends /opt/homebrew/bin to PATH, which would put
# weasyprint back; the fail-fast check runs the installed binary directly.
VELLUM_BIN=/opt/homebrew/opt/vellum/bin/vellum-bin
ADDR="${VELLUM_RELEASE_ADDR:-127.0.0.1:18744}"
FIXTURE="$ROOT/test/verify-release/doc.md"
DOCX="$ROOT/convert/testdata/corpus/styled/styled.docx"
PDF_PAGES=1 # doc.md renders to one page

EXPECTED="${1:-${VERSION:-}}"
if [[ -z "$EXPECTED" ]]; then
	EXPECTED="$(git -C "$ROOT" describe --tags --abbrev=0 2>/dev/null)"
fi
EXPECTED="${EXPECTED#v}"

W="$(mktemp -d "${TMPDIR:-/tmp}/vellum-verify.XXXXXX")"
trap 'rm -rf "$W"' EXIT
cp "$FIXTURE" "$W/doc.md"
cp "$DOCX" "$W/styled.docx"
cd "$W" || exit 1

pass=0 fail=0 skip=0
results=()
ok() { pass=$((pass + 1)); results+=("PASS  $1"); echo "PASS  $1"; }
bad() { fail=$((fail + 1)); results+=("FAIL  $1 — $2"); echo "FAIL  $1 — $2"; }
skp() { skip=$((skip + 1)); results+=("SKIP  $1 — $2"); echo "SKIP  $1 — $2"; }
# has FILE NEEDLE... : every needle occurs in FILE (fixed strings).
has() {
	local f=$1 n
	shift
	for n in "$@"; do grep -qF -- "$n" "$f" || { echo "missing '$n'"; return 1; }; done
}

echo "verify-release: expecting vellum ${EXPECTED:-<none>} (workdir $W)"
[[ -n "$EXPECTED" ]] || { bad "expected version" "no VERSION and no git tag"; EXPECTED=unknown; }

# --- 1. installed version ----------------------------------------------
got="$("$VELLUM" --version 2>&1)"
if [[ "$got" == "$EXPECTED" ]]; then ok "version: $VELLUM --version = $got"
else bad "version" "$VELLUM --version = '$got', want '$EXPECTED'"; fi

# --- 2. release service: restart, 60s liveness, MCP serverInfo ---------
if out="$(VELLUM_RELEASE_ADDR="$ADDR" "$HERE/post-release-check.sh" "$EXPECTED" 2>&1)"; then
	ok "service: $(echo "$out" | tail -1 | sed 's/^post-release-check: //')"
else
	echo "$out"
	bad "service" "$(echo "$out" | grep FAIL | tail -1 | sed 's/^post-release-check: //')"
fi

# --- 3. CLI ------------------------------------------------------------
cli() { "$VELLUM" convert --from file --to file "$@" >"$W/cli.log" 2>&1; }

if cli doc.md -o doc.pdf && [[ -s doc.pdf ]]; then
	pdftotext doc.pdf doc.pdf.txt
	pages="$(pdfinfo doc.pdf | awk '/^Pages:/ {print $2}')"
	imgs="$(pdfimages -list doc.pdf | awk 'NR > 2' | wc -l | tr -d ' ')"
	why=""
	[[ "$pages" == "$PDF_PAGES" ]] || why="pages=$pages want $PDF_PAGES"
	m="$(has doc.pdf.txt "Verify Release Fixture" "Second Heading" tableCellOmega codeSentinel42)" || why="$why $m"
	[[ "$imgs" -ge 1 ]] || why="$why no embedded image (mermaid PNG)"
	pdffonts doc.pdf | grep -q KaTeX || why="$why no KaTeX font embedded"
	python3 "$HERE/pdf-anchor-check.py" doc.pdf second-heading >"$W/anchor.log" 2>&1 || why="$why $(tail -1 "$W/anchor.log")"
	if [[ -z "$why" ]]; then ok "cli md→pdf: $pages page, text, $imgs image(s) (mermaid), KaTeX fonts, #second-heading link is internal"
	else bad "cli md→pdf" "$why"; fi
else bad "cli md→pdf" "$(tail -3 "$W/cli.log")"; fi

if cli doc.md -o doc.html && [[ -s doc.html ]]; then
	why=""
	m="$(has doc.html "Verify Release Fixture" tableCellOmega codeSentinel42 MermaidNodeAlpha 'class="katex"')" || why="$m"
	grep -qE 'https?://[^"]*(jsdelivr|unpkg|cdnjs)' doc.html && why="$why CDN reference"
	css="$(grep -oE 'href="[^"]*katex\.min\.css"' doc.html | head -1 | sed -e 's/^href="//' -e 's/"$//' -e 's|^file://||')"
	[[ -n "$css" && -s "$css" ]] || why="$why KaTeX stylesheet not a local file ('$css')"
	if [[ -z "$why" ]]; then ok "cli md→html: text, mermaid SVG, KaTeX offline ($css)"
	else bad "cli md→html" "$why"; fi
else bad "cli md→html" "$(tail -3 "$W/cli.log")"; fi

if cli doc.md -o doc.docx; then
	bad "cli md→docx" "a docx writer now exists — give this check a content assertion"
elif grep -qF 'does not support format "docx"' "$W/cli.log"; then
	skp "cli md→docx" "vellum has no DOCX writer (file sink refuses docx, as documented)"
else bad "cli md→docx" "$(tail -3 "$W/cli.log")"; fi

if cli doc.md -o doc.pptx && [[ -s doc.pptx ]]; then
	unzip -p doc.pptx 'ppt/slides/*.xml' >doc.pptx.xml 2>/dev/null
	if m="$(has doc.pptx.xml "Verify Release Fixture" "Second Heading" codeSentinel42)"; then
		ok "cli md→pptx: $(unzip -l doc.pptx | grep -c 'ppt/slides/slide[0-9]*\.xml') slides, text present"
	else bad "cli md→pptx" "$m"; fi
else bad "cli md→pptx" "$(tail -3 "$W/cli.log")"; fi

if "$VELLUM" convert --from file --to content styled.docx >docx.md 2>"$W/cli.log"; then
	if m="$(has docx.md "# Alpha Heading" "## Beta Section" cellKappa)"; then ok "cli docx→md: headings and table"
	else bad "cli docx→md" "$m"; fi
else bad "cli docx→md" "$(tail -3 "$W/cli.log")"; fi

shim="$W/path-no-weasyprint"
mkdir -p "$shim"
for f in /opt/homebrew/bin/*; do [[ "$(basename "$f")" == weasyprint ]] || ln -s "$f" "$shim/"; done
ffout="$(PATH="$shim:/usr/bin:/bin" "$VELLUM_BIN" doc.md -o ff.pdf 2>&1)"
ffcode=$?
if [[ $ffcode -ne 0 && "$ffout" == *'required dependency "weasyprint" not found on PATH'* && "$ffout" != *"exec:"* && ! -e ff.pdf ]]; then
	ok "fail-fast: no weasyprint → RequiredDeps error up front (exit $ffcode)"
else bad "fail-fast" "exit $ffcode: $ffout"; fi

# --- 4. HTTP MCP on the release port ------------------------------------
mcp() { "$HERE/mcp-call.py" "http://$ADDR/mcp" "$@"; }
# mcptext RESULT_JSON: tool text, with isError → exit 1
mcptext() { python3 -c 'import json,sys; r=json.load(sys.stdin); print("".join(c.get("text","") for c in r.get("content",[]))); sys.exit(1 if r.get("isError") else 0)'; }

if tl="$(mcp tools/list 2>&1)" && echo "$tl" | python3 -c 'import json,sys; sys.exit(0 if "convert" in [t["name"] for t in json.load(sys.stdin)["tools"]] else 1)'; then
	ok "mcp tools/list: convert"
else bad "mcp tools/list" "$tl"; fi

args="$(python3 -c 'import json,sys; print(json.dumps({"name":"convert","arguments":{"from":{"media":"content","content":open(sys.argv[1]).read()},"to":{"media":"content","format":"html"}}}))' doc.md)"
if r="$(mcp tools/call "$args" 2>&1)" && t="$(echo "$r" | mcptext)" && [[ "$t" == *tableCellOmega* && "$t" == *'class="katex"'* ]]; then
	ok "mcp md→html (content→content)"
else bad "mcp md→html" "${r:0:300}"; fi

args="$(python3 -c 'import json,sys; print(json.dumps({"name":"convert","arguments":{"from":{"media":"file","path":sys.argv[1]},"to":{"media":"file","path":sys.argv[2]}}}))' "$W/doc.md" "$W/mcp.pdf")"
if r="$(mcp tools/call "$args" 2>&1)" && echo "$r" | mcptext >/dev/null && [[ -s mcp.pdf ]] && pdftotext mcp.pdf - | grep -qF tableCellOmega; then
	ok "mcp md→pdf (file→file)"
else bad "mcp md→pdf" "${r:0:300}"; fi

args="$(python3 -c 'import json,sys; print(json.dumps({"name":"convert","arguments":{"from":{"media":"file","path":sys.argv[1]},"to":{"media":"content"}}}))' "$W/styled.docx")"
if r="$(mcp tools/call "$args" 2>&1)" && t="$(echo "$r" | mcptext)" && [[ "$t" == *"Alpha Heading"* && "$t" == *cellKappa* ]]; then
	ok "mcp docx→md (file→content)"
else bad "mcp docx→md" "${r:0:300}"; fi

# --- 5. viewer on the release port ---------------------------------------
view="$(curl -sL -o view.html -w '%{http_code} %{url_effective}' -m 60 "http://$ADDR$W/doc.md")"
code="${view%% *}"
if [[ "$code" == 200 ]] && m="$(has view.html "Verify Release Fixture" tableCellOmega "/_vellum/katex/")" &&
	! grep -qE 'https?://[^"]*(jsdelivr|unpkg|cdnjs)' view.html; then
	ok "viewer: rendered page, KaTeX linked from /_vellum/katex/, no CDN"
else bad "viewer page" "HTTP $code ${m:-}"; fi

# Same-page links must resolve (against the page's <base href>, as a
# browser does) to the document itself, not the source directory.
if r="$(python3 - "${view#* }" view.html <<'PY' 2>&1
import html, re, sys, urllib.parse
page_url, page = sys.argv[1], open(sys.argv[2], encoding="utf-8").read()
base = html.unescape(re.search(r'<base href="([^"]*)">', page).group(1))
want = urllib.parse.urldefrag(page_url)[0]
for raw in re.findall(r'<a\b[^>]*\shref="([^"]*)"', page):
    u = urllib.parse.urljoin(base, html.unescape(raw))
    doc, frag = urllib.parse.urldefrag(u)
    if frag == "second-heading":
        if doc != want:
            sys.exit(f"#second-heading resolves to {u}, not {want}")
        print(u)
        sys.exit(0)
sys.exit("no link to #second-heading in the page")
PY
)"; then
	ok "viewer: [the table](#second-heading) resolves to the document"
else bad "viewer anchor" "$r"; fi

css_code="$(curl -s -o katex.css -w '%{http_code}' -m 15 "http://$ADDR/_vellum/katex/katex.min.css")"
font="$(grep -oE 'fonts/KaTeX_Main-Regular\.woff2' katex.css 2>/dev/null | head -1)"
font_code="$(curl -s -o /dev/null -w '%{http_code}' -m 15 "http://$ADDR/_vellum/katex/${font:-fonts/KaTeX_Main-Regular.woff2}")"
if [[ "$css_code" == 200 && -n "$font" && "$font_code" == 200 ]] && grep -qF ".katex" katex.css; then
	ok "viewer: /_vellum/katex/ serves katex.min.css and $font"
else bad "viewer katex" "css HTTP $css_code, font '$font' HTTP $font_code"; fi

# --- 6. clipboard --------------------------------------------------------
skp "clipboard" "not exercised: the release service and any ssh session may have no GUI/pasteboard session, and writing the user's clipboard during a release is intrusive; covered by cv gate's pasteboard tests and manual smoke"

# --- summary -------------------------------------------------------------
echo
echo "verify-release summary (vellum ${EXPECTED}): ${pass} passed, ${fail} failed, ${skip} skipped"
printf '  %s\n' "${results[@]}"
if [[ $fail -ne 0 ]]; then
	echo "verify-release: FAIL — broken release"
	exit 1
fi
echo "verify-release: PASS"
