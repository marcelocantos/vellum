// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package viewer

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestServiceTaskXML(t *testing.T) {
	b, err := serviceTaskXML(`HOST\me`, `C:\Windows\System32\conhost.exe`, `--headless "C:\v\vellum.exe" serve-view`, `C:\v`)
	if err != nil {
		t.Fatal(err)
	}
	if b[0] != 0xFF || b[1] != 0xFE {
		t.Fatal("missing UTF-16LE BOM")
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 2; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	doc := string(utf16.Decode(u))
	for _, want := range []string{
		`<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>`,
		`<LogonType>InteractiveToken</LogonType>`,
		`<RunLevel>LeastPrivilege</RunLevel>`,
		`<UserId>HOST\me</UserId>`,
		`<Arguments>--headless &#34;C:\v\vellum.exe&#34; serve-view</Arguments>`,
		`<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("task XML missing %s:\n%s", want, doc)
		}
	}
}
