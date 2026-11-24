# cv gate (cvfile) is the only definition of green. The bullseye target
# exists solely so tools that call `make bullseye` (the bullseye/cv
# convergence hook) get the same oracle; never add a second gate recipe
# here (ENT-001).
#
# verify-release is not a gate: it checks the INSTALLED release on Colossus
# (Homebrew vellum + LaunchAgent vellum-view-release) after `brew upgrade
# vellum`. Override the expected version with VERSION=x.y.z (default: the
# latest git tag).
.PHONY: bullseye verify-release

# macOS: cgo and the viewer applet use Apple's clang unless CC is set
# (Homebrew llvm's clang can target an SDK the Command Line Tools lack).
ifeq ($(shell uname -s),Darwin)
ifeq ($(origin CC),default)
CC = /usr/bin/clang
endif
export CC
endif

bullseye:
	cv bullseye

verify-release:
	./scripts/verify-release.sh $(VERSION)

# Requires Playwright and Chrome; exercises the live viewer on an isolated port.
.PHONY: test-table-headers test-table-layout test-chrome test-editor
test-table-headers:
	go build -o /tmp/vellum-table-journey ./cmd/vellum
	NODE_PATH=editor/node_modules VELLUM_BINARY=/tmp/vellum-table-journey node scripts/table-header-journey.cjs

# Requires `npm ci` in editor/, Chrome, and Playwright's Firefox
# (`cd editor && npx playwright install firefox`). Firefox is the browser
# that shows a table pushing the page into sideways scroll.
test-table-layout:
	go build -o /tmp/vellum-table-layout-journey ./cmd/vellum
	VELLUM_BINARY=/tmp/vellum-table-layout-journey node scripts/table-layout-journey.cjs

# Requires `npm ci` in editor/, Chrome, Playwright's Firefox, and mmdc. The
# viewer's chrome in a real browser: the bar's name and title, the contents
# panel's section tracking and narrow-window drawer, themes, dark diagrams,
# the homepage link.
test-chrome:
	go build -o /tmp/vellum-chrome-journey ./cmd/vellum
	VELLUM_BINARY=/tmp/vellum-chrome-journey node scripts/viewer-chrome-journey.cjs

# Requires `npm ci` in editor/ (Playwright), Chrome, and node with katex and
# mmdc. Opens hostile documents in the viewer and checks nothing they carry
# runs, that the CSP refuses injected script on its own, and that the
# viewer's own features (theme, math, diagrams, editor) still work.
.PHONY: test-viewer-security
test-viewer-security:
	go build -o /tmp/vellum-security-journey ./cmd/vellum
	NODE_PATH=editor/node_modules VELLUM_BINARY=/tmp/vellum-security-journey node scripts/viewer-security-journey.cjs

# Requires `npm ci` in editor/ and Chrome; drives the source editor end to
# end (open, save, outside edits, conflict, close, theme, layout, offline).
# Rebuilds the embedded bundle first, so the journey never tests a stale one;
# commit viewer/editor/ when it changes.
test-editor:
	cd editor && npm run build
	go build -o /tmp/vellum-editor-journey ./cmd/vellum
	VELLUM_BINARY=/tmp/vellum-editor-journey node editor/journey.cjs
