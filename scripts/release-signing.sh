#!/usr/bin/env bash
# The macOS binary's signature: Canticode's Developer ID. Sourced by
# release-common.sh (release-package.sh signs with it), by
# verify-release.sh (the installed binary must carry it), and by
# launchd/run-view.sh, which signs the checkout's dev build as
# $SIGN_IDENTIFIER.dev so macOS privacy grants survive a rebuild (🎯T86).
SIGN_IDENTITY="Developer ID Application: Canticode Pty Ltd (N29CRQ669R)"
SIGN_TEAM="N29CRQ669R"
SIGN_IDENTIFIER="com.marcelocantos.vellum"
