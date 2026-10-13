// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package viewer

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// InstallService registers a per-user Scheduled Task that starts
// `vellum serve-view` at logon, then starts it now. It is the Windows
// counterpart of `brew services start vellum`:
//
//   - per-user, least privilege, interactive token: no admin, and the
//     server runs in the user's desktop session so the clipboard works
//     (a Windows service would run in session 0, where it cannot);
//   - no execution time limit (schtasks defaults to 72 hours);
//   - restarts on failure; ignores a second instance;
//   - launched through conhost --headless so no console window shows.
//
// Re-running replaces the task (e.g. after moving vellum.exe). It
// returns the registered command line.
func InstallService(opts *ServiceOptions) (string, error) {
	if opts == nil {
		opts = &ServiceOptions{}
	}
	exe := opts.VellumPath
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return "", fmt.Errorf("install-service: locating vellum.exe: %w", err)
		}
	}
	exe, err := filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(exe); err != nil {
		return "", fmt.Errorf("install-service: %w", err)
	}
	args := `"` + exe + `" serve-view`
	if opts.Addr != "" {
		args += " --addr " + opts.Addr
	}
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("install-service: current user: %w", err)
	}
	conhost := filepath.Join(os.Getenv("SystemRoot"), "System32", "conhost.exe")
	doc, err := serviceTaskXML(u.Username, conhost, "--headless "+args, filepath.Dir(exe))
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "vellum-task-*.xml")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(doc); err != nil {
		f.Close()
		return "", err
	}
	f.Close()
	if out, err := schtasks("/Create", "/F", "/TN", ServiceTaskName, "/XML", f.Name()); err != nil {
		return "", fmt.Errorf("install-service: schtasks /Create: %v: %s", err, out)
	}
	if !opts.NoStart {
		// A server already listening (e.g. an on-demand spawn) makes the
		// new instance exit on bind failure; stop that first so the task
		// owns the port.
		stopStrayServers(exe)
		if out, err := schtasks("/Run", "/TN", ServiceTaskName); err != nil {
			return "", fmt.Errorf("install-service: schtasks /Run: %v: %s", err, out)
		}
	}
	return conhost + " --headless " + args, nil
}

// UninstallService ends and deletes the Scheduled Task.
func UninstallService() error {
	_, _ = schtasks("/End", "/TN", ServiceTaskName)
	if out, err := schtasks("/Delete", "/F", "/TN", ServiceTaskName); err != nil {
		return fmt.Errorf("uninstall-service: schtasks /Delete: %v: %s", err, out)
	}
	return nil
}

// ServiceStatus returns schtasks' verbose listing for the task.
func ServiceStatus() (string, error) {
	out, err := schtasks("/Query", "/TN", ServiceTaskName, "/V", "/FO", "LIST")
	if err != nil {
		return "", fmt.Errorf("service not installed (%v): %s", err, out)
	}
	return out, nil
}

func schtasks(args ...string) (string, error) {
	out, err := exec.Command("schtasks", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// stopStrayServers ends serve-view processes of exe that are not the
// task's, best-effort.
func stopStrayServers(exe string) {
	if err := ProbeViewServer(DefaultViewOrigin()); err != nil {
		return
	}
	ps := `Get-CimInstance Win32_Process -Filter "Name='` + filepath.Base(exe) + `'" | ` +
		`Where-Object { $_.CommandLine -like '*serve-view*' -and $_.ProcessId -ne $PID } | ` +
		`ForEach-Object { Stop-Process -Id $_.ProcessId -Force }`
	_ = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Run()
}

type taskXML struct {
	XMLName xml.Name `xml:"Task"`
	Version string   `xml:"version,attr"`
	Xmlns   string   `xml:"xmlns,attr"`
	Info    struct {
		Description string `xml:"Description"`
	} `xml:"RegistrationInfo"`
	Triggers struct {
		Logon struct {
			Enabled bool   `xml:"Enabled"`
			UserID  string `xml:"UserId"`
		} `xml:"LogonTrigger"`
	} `xml:"Triggers"`
	Principals struct {
		Principal struct {
			ID        string `xml:"id,attr"`
			UserID    string `xml:"UserId"`
			LogonType string `xml:"LogonType"`
			RunLevel  string `xml:"RunLevel"`
		} `xml:"Principal"`
	} `xml:"Principals"`
	Settings struct {
		MultipleInstancesPolicy    string `xml:"MultipleInstancesPolicy"`
		DisallowStartIfOnBatteries bool   `xml:"DisallowStartIfOnBatteries"`
		StopIfGoingOnBatteries     bool   `xml:"StopIfGoingOnBatteries"`
		AllowHardTerminate         bool   `xml:"AllowHardTerminate"`
		StartWhenAvailable         bool   `xml:"StartWhenAvailable"`
		AllowStartOnDemand         bool   `xml:"AllowStartOnDemand"`
		Enabled                    bool   `xml:"Enabled"`
		Hidden                     bool   `xml:"Hidden"`
		ExecutionTimeLimit         string `xml:"ExecutionTimeLimit"`
		RestartOnFailure           struct {
			Interval string `xml:"Interval"`
			Count    int    `xml:"Count"`
		} `xml:"RestartOnFailure"`
	} `xml:"Settings"`
	Actions struct {
		Context string `xml:"Context,attr"`
		Exec    struct {
			Command          string `xml:"Command"`
			Arguments        string `xml:"Arguments"`
			WorkingDirectory string `xml:"WorkingDirectory"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

// serviceTaskXML renders the Task Scheduler definition as the UTF-16LE
// (with BOM) document schtasks /XML expects.
func serviceTaskXML(userID, command, arguments, workdir string) ([]byte, error) {
	var t taskXML
	t.Version = "1.2"
	t.Xmlns = "http://schemas.microsoft.com/windows/2004/02/mit/task"
	t.Info.Description = "Runs the vellum Markdown view server and HTTP MCP endpoint (vellum serve-view) for this user. Managed by vellum install-service."
	t.Triggers.Logon.Enabled = true
	t.Triggers.Logon.UserID = userID
	p := &t.Principals.Principal
	p.ID, p.UserID, p.LogonType, p.RunLevel = "Author", userID, "InteractiveToken", "LeastPrivilege"
	s := &t.Settings
	s.MultipleInstancesPolicy = "IgnoreNew"
	s.AllowHardTerminate, s.StartWhenAvailable, s.AllowStartOnDemand, s.Enabled = true, true, true, true
	s.ExecutionTimeLimit = "PT0S"
	s.RestartOnFailure.Interval, s.RestartOnFailure.Count = "PT1M", 3
	t.Actions.Context = "Author"
	t.Actions.Exec.Command, t.Actions.Exec.Arguments, t.Actions.Exec.WorkingDirectory = command, arguments, workdir
	body, err := xml.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, err
	}
	doc := `<?xml version="1.0" encoding="UTF-16"?>` + "\r\n" + string(body)
	var buf bytes.Buffer
	buf.Write([]byte{0xFF, 0xFE})
	for _, c := range utf16.Encode([]rune(doc)) {
		buf.WriteByte(byte(c))
		buf.WriteByte(byte(c >> 8))
	}
	return buf.Bytes(), nil
}
