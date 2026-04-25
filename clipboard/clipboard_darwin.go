// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package clipboard

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>

// vellum_clip_result holds the new pasteboard changeCount on success
// (positive) or an error code on failure (negative). 0 means "no change",
// which we treat as a write failure.
//
// Codes:
//   -1  HTML→NSAttributedString parse failed
//   -2  NSAttributedString→RTF serialisation failed
//   -3  NSPasteboard setData failed for one of the registered types
typedef struct {
    long changeCount;
} vellum_clip_result;

// vellum_read_pasteboard_data reads the raw bytes for the named UTI from
// the general pasteboard. The caller must free the returned buffer with
// free(). On miss, returns NULL with *outLen=0. Used only by tests.
static const void *vellum_read_pasteboard_data(const char *uti, int *outLen) {
    @autoreleasepool {
        NSString *type = [NSString stringWithUTF8String:uti];
        NSData *data = [[NSPasteboard generalPasteboard] dataForType:type];
        if (!data) { *outLen = 0; return NULL; }
        int n = (int)[data length];
        void *buf = malloc(n);
        memcpy(buf, [data bytes], n);
        *outLen = n;
        return buf;
    }
}

// vellum_set_clipboard_html drives the full transaction. Parameters:
//   rtfSrcBytes / rtfSrcLen   — full HTML document (with <head><style>);
//                               passed to NSAttributedString so the
//                               resulting RTF inherits CSS styling.
//   clipHTMLBytes / clipHTMLLen — body fragment placed on the
//                                 pasteboard under public.html. Slack
//                                 and similar rich-paste targets reject
//                                 full documents but accept fragments.
static vellum_clip_result vellum_set_clipboard_html(
    const void *rtfSrcBytes, int rtfSrcLen,
    const void *clipHTMLBytes, int clipHTMLLen) {
    vellum_clip_result r = {0};
    @autoreleasepool {
        NSData *rtfSrcData  = [NSData dataWithBytes:rtfSrcBytes  length:rtfSrcLen];
        NSData *clipHTMLData = [NSData dataWithBytes:clipHTMLBytes length:clipHTMLLen];

        NSDictionary *parseOpts = @{
            NSDocumentTypeDocumentAttribute: NSHTMLTextDocumentType,
            NSCharacterEncodingDocumentAttribute: @(NSUTF8StringEncoding)
        };
        NSError *err = nil;
        NSAttributedString *attr = [[NSAttributedString alloc]
            initWithData:rtfSrcData
                 options:parseOpts
      documentAttributes:NULL
                   error:&err];
        if (!attr) { r.changeCount = -1; return r; }

        NSDictionary *rtfOpts = @{
            NSDocumentTypeDocumentAttribute: NSRTFTextDocumentType
        };
        NSData *rtfData = [attr dataFromRange:NSMakeRange(0, [attr length])
                           documentAttributes:rtfOpts
                                        error:&err];
        if (!rtfData) { r.changeCount = -2; return r; }

        // NSAttributedString uses U+2028 (LINE SEPARATOR) and U+2029
        // (PARAGRAPH SEPARATOR) in its plain-text projection. These are
        // technically valid Unicode line terminators but trip editor
        // heuristics (VS Code flags them as "unusual line terminators").
        // Normalise to U+000A so plain-text consumers see ordinary
        // newlines.
        NSMutableString *plain = [[attr string] mutableCopy];
        [plain replaceOccurrencesOfString:@" " withString:@"\n"
                                  options:0 range:NSMakeRange(0, [plain length])];
        [plain replaceOccurrencesOfString:@" " withString:@"\n"
                                  options:0 range:NSMakeRange(0, [plain length])];
        NSData *plainData = [plain dataUsingEncoding:NSUTF8StringEncoding];

        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSArray *types = @[
            NSPasteboardTypeRTF,
            NSPasteboardTypeHTML,
            NSPasteboardTypeString,
        ];
        long newCount = [pb declareTypes:types owner:nil];

        BOOL ok = YES;
        ok = ok && [pb setData:rtfData      forType:NSPasteboardTypeRTF];
        ok = ok && [pb setData:clipHTMLData forType:NSPasteboardTypeHTML];
        ok = ok && [pb setData:plainData    forType:NSPasteboardTypeString];
        if (!ok) { r.changeCount = -3; return r; }

        r.changeCount = newCount;
        return r;
    }
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func writePayload(p Payload) error {
	rtfSrc := []byte(p.HTML)
	clipHTML := []byte(htmlBodyFragment(p.HTML))
	if len(clipHTML) == 0 {
		clipHTML = rtfSrc
	}
	res := C.vellum_set_clipboard_html(
		unsafe.Pointer(&rtfSrc[0]), C.int(len(rtfSrc)),
		unsafe.Pointer(&clipHTML[0]), C.int(len(clipHTML)),
	)
	switch {
	case res.changeCount > 0:
		return nil
	case res.changeCount == -1:
		return fmt.Errorf("clipboard: failed to parse HTML into NSAttributedString")
	case res.changeCount == -2:
		return fmt.Errorf("clipboard: failed to serialise RTF from HTML")
	case res.changeCount == -3:
		return fmt.Errorf("clipboard: NSPasteboard setData failed")
	default:
		return fmt.Errorf("clipboard: NSPasteboard write produced no changeCount advance")
	}
}

// readPasteboardData returns the raw bytes for the given UTI on the
// general pasteboard, or nil if the type is not present. It exists for
// the round-trip test — production code does not need to read back what
// it just wrote.
func readPasteboardData(uti string) []byte {
	cUTI := C.CString(uti)
	defer C.free(unsafe.Pointer(cUTI))
	var n C.int
	ptr := C.vellum_read_pasteboard_data(cUTI, &n)
	if ptr == nil || n == 0 {
		return nil
	}
	defer C.free(unsafe.Pointer(ptr))
	return C.GoBytes(unsafe.Pointer(ptr), n)
}
