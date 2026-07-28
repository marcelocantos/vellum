// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package viewer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

const (
	// BundleID is the CFBundleIdentifier for Vellum Viewer.app.
	BundleID = "com.marcelocantos.vellum-viewer"
	// AppName is the display / directory name of the app bundle.
	AppName = "Vellum Viewer.app"
)

// InstallOptions configures InstallViewer.
type InstallOptions struct {
	// ApplicationsDir defaults to ~/Applications.
	ApplicationsDir string
	// VellumPath is the absolute path to the vellum binary baked into the
	// app launcher. Empty resolves via os.Executable / LookPath.
	VellumPath string
	// SkipDefaultHandler, when true, installs the bundle but does not
	// claim the default .md handler via duti.
	SkipDefaultHandler bool
}

// InstallViewer generates Vellum Viewer.app, registers it with Launch
// Services, and (unless SkipDefaultHandler) sets it as the default
// handler for Markdown UTIs/extensions via duti.
func InstallViewer(opts *InstallOptions) (appPath string, err error) {
	if opts == nil {
		opts = &InstallOptions{}
	}
	appDir, err := resolveApplicationsDir(opts.ApplicationsDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", fmt.Errorf("creating Applications dir: %w", err)
	}

	vellumPath, err := resolveVellumPath(opts.VellumPath)
	if err != nil {
		return "", err
	}

	appPath = filepath.Join(appDir, AppName)
	if err := writeAppBundle(appPath, vellumPath); err != nil {
		return "", err
	}

	// Refresh Launch Services so the new bundle is known.
	if err := lsregister(appPath); err != nil {
		return appPath, fmt.Errorf("registered bundle at %s but lsregister failed: %w", appPath, err)
	}

	if !opts.SkipDefaultHandler {
		if err := setDefaultHandler(); err != nil {
			return appPath, fmt.Errorf("installed %s but failed to set default handler: %w\n"+
				"  The app is registered; set it as default via Finder Get Info, or install duti (`brew install duti`) and re-run install-viewer",
				appPath, err)
		}
	}
	return appPath, nil
}

// UninstallViewer removes the app bundle and attempts to clear default
// handler claims. It does not restore the previous default handler —
// macOS has no reliable API for that.
func UninstallViewer(opts *InstallOptions) error {
	if opts == nil {
		opts = &InstallOptions{}
	}
	appDir, err := resolveApplicationsDir(opts.ApplicationsDir)
	if err != nil {
		return err
	}
	appPath := filepath.Join(appDir, AppName)
	if err := os.RemoveAll(appPath); err != nil {
		return fmt.Errorf("removing %s: %w", appPath, err)
	}
	// Best-effort: clear duti claims if duti is available. Failure is
	// non-fatal because the bundle is already gone.
	_ = clearDefaultHandler()
	return nil
}

// AppBundlePath returns the path where the viewer app would be installed.
func AppBundlePath(applicationsDir string) (string, error) {
	appDir, err := resolveApplicationsDir(applicationsDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, AppName), nil
}

func resolveApplicationsDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir: %w", err)
	}
	return filepath.Join(home, "Applications"), nil
}

func resolveVellumPath(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	// Prefer the running binary so brew upgrades still work only after
	// re-running install-viewer (the path is baked in at install time).
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			return resolved, nil
		}
		return exe, nil
	}
	p, err := exec.LookPath("vellum")
	if err != nil {
		return "", fmt.Errorf("cannot find vellum binary: %w", err)
	}
	return filepath.Abs(p)
}

func writeAppBundle(appPath, vellumPath string) error {
	// Rebuild cleanly so upgrades replace the launcher script and plist.
	if err := os.RemoveAll(appPath); err != nil {
		return fmt.Errorf("clearing old bundle: %w", err)
	}
	macosDir := filepath.Join(appPath, "Contents", "MacOS")
	if err := os.MkdirAll(macosDir, 0o755); err != nil {
		return err
	}

	plistPath := filepath.Join(appPath, "Contents", "Info.plist")
	if err := os.WriteFile(plistPath, []byte(infoPlist), 0o644); err != nil {
		return fmt.Errorf("writing Info.plist: %w", err)
	}

	launcherPath := filepath.Join(macosDir, "VellumViewer")
	var buf strings.Builder
	if err := launcherTmpl.Execute(&buf, map[string]string{
		"VellumPath": vellumPath,
	}); err != nil {
		return err
	}
	if err := os.WriteFile(launcherPath, []byte(buf.String()), 0o755); err != nil {
		return fmt.Errorf("writing launcher: %w", err)
	}
	return nil
}

func lsregister(appPath string) error {
	// Standard location on modern macOS.
	ls := "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
	if _, err := os.Stat(ls); err != nil {
		// Older layout fallback.
		ls = "/System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/LaunchServices.framework/Versions/A/Support/lsregister"
	}
	cmd := exec.Command(ls, "-f", appPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// setDefaultHandler uses duti to claim Markdown UTIs and extensions.
func setDefaultHandler() error {
	if _, err := exec.LookPath("duti"); err != nil {
		return fmt.Errorf("duti not found on PATH (install with `brew install duti`)")
	}
	// UTIs first, then extension fallbacks for files with no type metadata.
	targets := []string{
		"net.daringfireball.markdown",
		"public.markdown",
		"org.vim.markdown-file", // common alternate UTI
		".md",
		".markdown",
		".mdown",
	}
	var errs []string
	for _, t := range targets {
		cmd := exec.Command("duti", "-s", BundleID, t, "all")
		out, err := cmd.CombinedOutput()
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v (%s)", t, err, strings.TrimSpace(string(out))))
		}
	}
	if len(errs) == len(targets) {
		return fmt.Errorf("duti failed for all targets:\n  %s", strings.Join(errs, "\n  "))
	}
	// Partial success is fine (some UTIs may not exist on all systems).
	return nil
}

func clearDefaultHandler() error {
	if _, err := exec.LookPath("duti"); err != nil {
		return err
	}
	// duti has no explicit "unset"; re-claiming is the user's job.
	// No-op beyond documentation — kept for symmetry.
	return nil
}

var launcherTmpl = template.Must(template.New("launcher").Parse(`#!/bin/bash
# Generated by vellum install-viewer. Do not edit.
# Opens Markdown files via vellum's view mode (cached HTML by default).
set -euo pipefail
VELLUM={{printf "%q" .VellumPath}}
exec "$VELLUM" --open "$@"
`))

// infoPlist declares Markdown document types so Launch Services will
// offer Vellum Viewer as a handler for .md double-clicks.
const infoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleExecutable</key>
	<string>VellumViewer</string>
	<key>CFBundleIdentifier</key>
	<string>` + BundleID + `</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>Vellum Viewer</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>1.0</string>
	<key>CFBundleVersion</key>
	<string>1</string>
	<key>LSMinimumSystemVersion</key>
	<string>12.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>CFBundleDocumentTypes</key>
	<array>
		<dict>
			<key>CFBundleTypeName</key>
			<string>Markdown Document</string>
			<key>CFBundleTypeRole</key>
			<string>Viewer</string>
			<key>LSHandlerRank</key>
			<string>Owner</string>
			<key>LSItemContentTypes</key>
			<array>
				<string>net.daringfireball.markdown</string>
				<string>public.markdown</string>
				<string>org.vim.markdown-file</string>
				<string>net.ia.markdown</string>
			</array>
			<key>CFBundleTypeExtensions</key>
			<array>
				<string>md</string>
				<string>markdown</string>
				<string>mdown</string>
				<string>mkd</string>
			</array>
		</dict>
	</array>
	<key>UTImportedTypeDeclarations</key>
	<array>
		<dict>
			<key>UTTypeIdentifier</key>
			<string>net.daringfireball.markdown</string>
			<key>UTTypeDescription</key>
			<string>Markdown Document</string>
			<key>UTTypeConformsTo</key>
			<array>
				<string>public.plain-text</string>
			</array>
			<key>UTTypeTagSpecification</key>
			<dict>
				<key>public.filename-extension</key>
				<array>
					<string>md</string>
					<string>markdown</string>
					<string>mdown</string>
					<string>mkd</string>
				</array>
			</dict>
		</dict>
	</array>
</dict>
</plist>
`
