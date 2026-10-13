// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package clipboard

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/marcelocantos/vellum/internal/pandoc"
)

// The Windows backend is pure Go over user32/kernel32. One write puts
// CF_HTML, RTF and CF_UNICODETEXT on the clipboard together inside a
// single Open/Empty/Close transaction, the analogue of the macOS
// single-commit write. Windows has no CSS-aware HTML→RTF importer like
// AppKit's, so RTF always comes from pandoc ([RoutePandoc]); that is the
// platform's preferred route, not a fallback, so Fallback stays empty.

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procRegisterClipboardFormatW   = user32.NewProc("RegisterClipboardFormatW")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalSize   = kernel32.NewProc("GlobalSize")
	procMoveMemory   = kernel32.NewProc("RtlMoveMemory")
)

const (
	cfUnicodeText = 13
	cfHDrop       = 15
	gmemMoveable  = 0x0002
)

// Registered format names. "Portable Document Format" is Acrobat's
// name; "PDF" is what Office and most other producers register.
var (
	fmtHTML = registerFormat("HTML Format")
	fmtRTF  = registerFormat("Rich Text Format")
	fmtPDF  = registerFormat("PDF")
	fmtPDF2 = registerFormat("Portable Document Format")
)

func registerFormat(name string) uintptr {
	p, _ := windows.UTF16PtrFromString(name)
	r, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(p)))
	return r
}

// openClipboard opens the clipboard on a locked OS thread, retrying
// briefly because another process may hold it. The caller must call
// the returned close func.
func openClipboard() (func(), error) {
	runtime.LockOSThread()
	var lastErr error
	for i := 0; i < 50; i++ {
		r, _, err := procOpenClipboard.Call(0)
		if r != 0 {
			return func() {
				procCloseClipboard.Call()
				runtime.UnlockOSThread()
			}, nil
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	runtime.UnlockOSThread()
	return nil, fmt.Errorf("clipboard: OpenClipboard: %v", lastErr)
}

func sequence() uintptr {
	r, _, _ := procGetClipboardSequenceNumber.Call()
	return r
}

// setData copies data into a movable global block and hands it to the
// clipboard, which then owns it.
func setData(format uintptr, data []byte) error {
	if format == 0 {
		return errors.New("clipboard: format not registered")
	}
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return fmt.Errorf("clipboard: GlobalAlloc: %v", err)
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("clipboard: GlobalLock: %v", err)
	}
	if len(data) > 0 {
		procMoveMemory.Call(p, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	}
	procGlobalUnlock.Call(h)
	if r, _, err := procSetClipboardData.Call(format, h); r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("clipboard: SetClipboardData(%d): %v", format, err)
	}
	return nil
}

// getData returns a copy of the clipboard's data for format, or nil.
// The clipboard must be open.
func getData(format uintptr) []byte {
	if format == 0 {
		return nil
	}
	if r, _, _ := procIsClipboardFormatAvailable.Call(format); r == 0 {
		return nil
	}
	h, _, _ := procGetClipboardData.Call(format)
	if h == 0 {
		return nil
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return nil
	}
	defer procGlobalUnlock.Call(h)
	n, _, _ := procGlobalSize.Call(h)
	out := make([]byte, n)
	if n > 0 {
		procMoveMemory.Call(uintptr(unsafe.Pointer(&out[0])), p, n)
	}
	return out
}

func utf16NulBytes(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 0, 2*len(u)+2)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return append(b, 0, 0)
}

func decodeUTF16Nul(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		c := uint16(b[i]) | uint16(b[i+1])<<8
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

// trimNul drops the NUL terminator(s) Windows pads byte formats with.
func trimNul(b []byte) []byte {
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return b
}

// writeFormats replaces the clipboard with the given formats in one
// transaction and confirms the sequence number advanced.
func writeFormats(items []clipItem) error {
	before := sequence()
	closeFn, err := openClipboard()
	if err != nil {
		return err
	}
	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		closeFn()
		return fmt.Errorf("clipboard: EmptyClipboard: %v", err)
	}
	for _, it := range items {
		if err := setData(it.format, it.data); err != nil {
			closeFn()
			return err
		}
	}
	closeFn()
	if sequence() == before {
		return errors.New("clipboard: write produced no clipboard sequence advance")
	}
	return nil
}

type clipItem struct {
	format uintptr
	data   []byte
}

func writePayload(p Payload) (WriteReport, error) {
	rtf, plain, err := pandocRichText(p.HTML)
	if err != nil {
		return WriteReport{}, fmt.Errorf("clipboard: %s HTML→RTF failed: %w", pandoc.Binary, err)
	}
	frag := htmlBodyFragment(p.HTML)
	if frag == "" {
		frag = p.HTML
	}
	err = writeFormats([]clipItem{
		{fmtHTML, encodeCFHTML(frag)},
		{fmtRTF, append(append([]byte{}, rtf...), 0)},
		{cfUnicodeText, utf16NulBytes(string(plain))},
	})
	if err != nil {
		return WriteReport{}, err
	}
	return WriteReport{Route: RoutePandoc}, nil
}

func readClipboard(format string) ([]byte, error) {
	closeFn, err := openClipboard()
	if err != nil {
		return nil, err
	}
	defer closeFn()
	switch format {
	case FormatRTF:
		return nilIfEmpty(trimNul(getData(fmtRTF))), nil
	case FormatHTML:
		b := trimNul(getData(fmtHTML))
		if len(b) == 0 {
			return nil, nil
		}
		return decodeCFHTML(b), nil
	case FormatPDF:
		if b := getData(fmtPDF); len(b) > 0 {
			return b, nil
		}
		return nilIfEmpty(getData(fmtPDF2)), nil
	case formatText:
		b := getData(cfUnicodeText)
		if len(b) == 0 {
			return nil, nil
		}
		return []byte(decodeUTF16Nul(b)), nil
	default:
		return nil, fmt.Errorf("clipboard: unknown format %q", format)
	}
}

// formatText is the internal plain-text read used by tests.
const formatText = "text"

func nilIfEmpty(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return b
}

// DROPFILES header: pFiles, pt.x, pt.y, fNC, fWide (20 bytes).
const dropFilesSize = 20

func writeFileRefs(paths []string) error {
	var list []byte
	n := 0
	for _, p := range paths {
		a, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("clipboard: resolving path %q: %w", p, err)
		}
		list = append(list, utf16NulBytes(a)...)
		n++
	}
	if n == 0 {
		return errors.New("clipboard: no valid paths for file reference write")
	}
	list = append(list, 0, 0) // list terminator
	data := make([]byte, dropFilesSize, dropFilesSize+len(list))
	data[0] = dropFilesSize // pFiles (little-endian DWORD)
	data[16] = 1            // fWide
	data = append(data, list...)
	return writeFormats([]clipItem{{cfHDrop, data}})
}

func readFileRefs() ([]string, error) {
	closeFn, err := openClipboard()
	if err != nil {
		return nil, err
	}
	b := getData(cfHDrop)
	closeFn()
	return parseDropFiles(b), nil
}

// parseDropFiles decodes a CF_HDROP DROPFILES block.
func parseDropFiles(b []byte) []string {
	if len(b) < dropFilesSize {
		return nil
	}
	off := int(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
	wide := b[16] != 0 || b[17] != 0
	if off < dropFilesSize || off > len(b) {
		return nil
	}
	var paths []string
	rest := b[off:]
	for len(rest) > 0 {
		var s string
		var used int
		if wide {
			s = decodeUTF16Nul(rest)
			used = 2*len(utf16.Encode([]rune(s))) + 2
		} else {
			i := 0
			for i < len(rest) && rest[i] != 0 {
				i++
			}
			s, used = string(rest[:i]), i+1
		}
		if s == "" {
			break
		}
		paths = append(paths, s)
		if used > len(rest) {
			break
		}
		rest = rest[used:]
	}
	return paths
}
