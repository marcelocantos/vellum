// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package viewer

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// BundleID is the CFBundleIdentifier for Vellum Viewer.app.
	BundleID = "com.marcelocantos.vellum-viewer"
	// AppName is the display / directory name of the app bundle.
	AppName = "Vellum Viewer.app"
)

//go:embed applet_darwin.m.src
var appletSource string

// InstallOptions configures InstallViewer.
type InstallOptions struct {
	// ApplicationsDir defaults to ~/Applications.
	ApplicationsDir string
	// VellumPath is the absolute path to the vellum binary injected into
	// the app via LSEnvironment VELLUM_BIN. Empty resolves via PATH
	// (preferring the Homebrew shell wrapper) or os.Executable.
	VellumPath string
	// SkipDefaultHandler, when true, installs the bundle but does not
	// claim the default .md handler via duti.
	SkipDefaultHandler bool
}

// InstallViewer generates Vellum Viewer.app, registers it with Launch
// Services, and (unless SkipDefaultHandler) sets it as the default
// handler for Markdown UTIs/extensions via duti.
//
// The app's CFBundleExecutable is a small Cocoa binary (compiled with
// clang at install time). Launch Services delivers open-document paths
// via Apple Events, not argv — a shell script launcher cannot receive
// them, which is why the original double-click path appeared dead.
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
	// Prefer the `vellum` on PATH — for Homebrew that is the shell wrapper
	// which prepends brew/tool dirs. Raw vellum-bin under launchd has a
	// minimal PATH and may fail to find node/mmdc.
	if p, err := exec.LookPath("vellum"); err == nil {
		return filepath.Abs(p)
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			return resolved, nil
		}
		return exe, nil
	}
	return "", fmt.Errorf("cannot find vellum binary on PATH")
}

func writeAppBundle(appPath, vellumPath string) error {
	if err := os.RemoveAll(appPath); err != nil {
		return fmt.Errorf("clearing old bundle: %w", err)
	}
	macosDir := filepath.Join(appPath, "Contents", "MacOS")
	if err := os.MkdirAll(macosDir, 0o755); err != nil {
		return err
	}

	plist := strings.ReplaceAll(infoPlist, "{{VELLUM_BIN}}", xmlEscape(vellumPath))
	plistPath := filepath.Join(appPath, "Contents", "Info.plist")
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("writing Info.plist: %w", err)
	}

	launcherPath := filepath.Join(macosDir, "VellumViewer")
	if err := compileApplet(launcherPath); err != nil {
		return err
	}
	return nil
}

// compileApplet builds the Cocoa document handler into outPath using
// the system clang. Requires Xcode Command Line Tools (or full Xcode).
func compileApplet(outPath string) error {
	clang, err := exec.LookPath("clang")
	if err != nil {
		return fmt.Errorf("clang not found — install Xcode Command Line Tools (`xcode-select --install`) to build Vellum Viewer.app: %w", err)
	}
	srcDir, err := os.MkdirTemp("", "vellum-viewer-src-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(srcDir)
	srcPath := filepath.Join(srcDir, "applet.m")
	if err := os.WriteFile(srcPath, []byte(appletSource), 0o644); err != nil {
		return err
	}
	cmd := exec.Command(clang,
		"-fobjc-arc",
		"-framework", "AppKit",
		"-framework", "Foundation",
		"-o", outPath,
		srcPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compiling VellumViewer applet: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	if err := os.Chmod(outPath, 0o755); err != nil {
		return err
	}
	return nil
}

func xmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}

func lsregister(appPath string) error {
	ls := "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
	if _, err := os.Stat(ls); err != nil {
		ls = "/System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/LaunchServices.framework/Versions/A/Support/lsregister"
	}
	cmd := exec.Command(ls, "-f", appPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func setDefaultHandler() error {
	if _, err := exec.LookPath("duti"); err != nil {
		return fmt.Errorf("duti not found on PATH (install with `brew install duti`)")
	}
	targets := []string{
		"net.daringfireball.markdown",
		"public.markdown",
		"org.vim.markdown-file",
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
	return nil
}

func clearDefaultHandler() error {
	if _, err := exec.LookPath("duti"); err != nil {
		return err
	}
	return nil
}

// infoPlist declares Markdown document types and injects VELLUM_BIN into
// the app's environment so the Cocoa applet can find the CLI under the
// minimal PATH launchd provides.
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
	<string>1.1</string>
	<key>CFBundleVersion</key>
	<string>2</string>
	<key>LSMinimumSystemVersion</key>
	<string>12.0</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>LSEnvironment</key>
	<dict>
		<key>VELLUM_BIN</key>
		<string>{{VELLUM_BIN}}</string>
	</dict>
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
