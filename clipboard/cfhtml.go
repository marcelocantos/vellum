// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package clipboard

import (
	"fmt"
	"regexp"
	"strconv"
)

// CF_HTML ("HTML Format") is the Windows clipboard's HTML flavour: a
// UTF-8 description header with byte offsets, then an HTML document
// whose fragment is bracketed by StartFragment/EndFragment comments.
// It lives outside the windows build so the encoder is tested on every
// platform.

const cfHTMLHeader = "Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n"

// encodeCFHTML wraps an HTML body fragment in a CF_HTML envelope.
func encodeCFHTML(fragment string) []byte {
	const pre = "<html><body>\r\n<!--StartFragment-->"
	const post = "<!--EndFragment-->\r\n</body></html>"
	headerLen := len(fmt.Sprintf(cfHTMLHeader, 0, 0, 0, 0))
	startHTML := headerLen
	startFrag := startHTML + len(pre)
	endFrag := startFrag + len(fragment)
	endHTML := endFrag + len(post)
	return []byte(fmt.Sprintf(cfHTMLHeader, startHTML, endHTML, startFrag, endFrag) + pre + fragment + post)
}

var cfHTMLOffsetRe = regexp.MustCompile(`(?m)^(StartHTML|EndHTML|StartFragment|EndFragment):(-?\d+)\r?$`)

// decodeCFHTML returns the HTML document carried by a CF_HTML payload
// (StartHTML..EndHTML). It falls back to the fragment offsets, then to
// the raw data when the header is missing or inconsistent, since some
// producers write StartHTML:-1.
func decodeCFHTML(data []byte) []byte {
	off := map[string]int{}
	for _, m := range cfHTMLOffsetRe.FindAllSubmatch(data, -1) {
		n, err := strconv.Atoi(string(m[2]))
		if err == nil {
			off[string(m[1])] = n
		}
	}
	valid := func(a, b string) bool {
		s, ok1 := off[a]
		e, ok2 := off[b]
		return ok1 && ok2 && s >= 0 && e >= s && e <= len(data)
	}
	switch {
	case valid("StartHTML", "EndHTML"):
		return data[off["StartHTML"]:off["EndHTML"]]
	case valid("StartFragment", "EndFragment"):
		return data[off["StartFragment"]:off["EndFragment"]]
	}
	return data
}
