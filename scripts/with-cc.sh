#!/bin/sh
# Run a command with CC pinned to Apple's clang on macOS, unless CC is
# already set. Homebrew llvm's clang is often first on PATH and can
# target a newer SDK than the Command Line Tools ship, which breaks cgo
# and the Cocoa viewer applet ("'Cocoa/Cocoa.h' file not found").
# Other hosts (Windows) run the command unchanged.
if [ "$(uname -s)" = Darwin ] && [ -z "${CC:-}" ]; then
	CC=/usr/bin/clang
	export CC
fi
exec "$@"
