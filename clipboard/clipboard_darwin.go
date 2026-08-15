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

// vellum_rich_text carries the two representations AppKit derives from
// HTML. On failure both buffers are NULL and err holds the reason as a
// malloc'd C string. All three are owned by the caller.
//
// The two halves of the old single entry point — convert, then write —
// are separate now because they fail for unrelated reasons and only the
// conversion has an alternative route (🎯T23).
typedef struct {
    void *rtf;
    int rtfLen;
    void *plain;
    int plainLen;
    char *err;
} vellum_rich_text;

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

// vellum_copy_data copies an NSData into a malloc'd buffer the Go side
// owns. cgo cannot hold on to autoreleased memory past the pool.
static void vellum_copy_data(NSData *data, void **outBuf, int *outLen) {
    int n = (int)[data length];
    void *buf = malloc((size_t)n);
    memcpy(buf, [data bytes], n);
    *outBuf = buf;
    *outLen = n;
}

// vellum_html_to_rich_text renders a full HTML document (with its
// <head><style>) to RTF and plain text through AppKit, so the RTF
// inherits the CSS styling.
//
// On macOS 26 the HTML importer behind NSAttributedString is not
// in-process: it brokers to a per-user launchd agent
// (com.apple.textkit.nsattributedstringagent). Where that agent cannot
// be looked up — a sandboxed process, or any context outside the user's
// GUI session — the initialiser returns nil for every input. That is a
// missing rendering route, not a broken clipboard: the pasteboard below
// is unaffected, so the caller falls back rather than failing (🎯T23).
static vellum_rich_text vellum_html_to_rich_text(const void *htmlBytes, int htmlLen) {
    vellum_rich_text r = {0};
    @autoreleasepool {
        NSData *htmlData = [NSData dataWithBytes:htmlBytes length:htmlLen];

        NSDictionary *parseOpts = @{
            NSDocumentTypeDocumentAttribute: NSHTMLTextDocumentType,
            NSCharacterEncodingDocumentAttribute: @(NSUTF8StringEncoding)
        };
        NSError *err = nil;
        NSAttributedString *attr = [[NSAttributedString alloc]
            initWithData:htmlData
                 options:parseOpts
      documentAttributes:NULL
                   error:&err];
        if (!attr) {
            NSString *why = err ? [err localizedDescription]
                                : @"NSAttributedString HTML importer unavailable";
            r.err = strdup([why UTF8String]);
            return r;
        }

        NSDictionary *rtfOpts = @{
            NSDocumentTypeDocumentAttribute: NSRTFTextDocumentType
        };
        NSData *rtfData = [attr dataFromRange:NSMakeRange(0, [attr length])
                           documentAttributes:rtfOpts
                                        error:&err];
        if (!rtfData) {
            NSString *why = err ? [err localizedDescription]
                                : @"RTF serialisation failed";
            r.err = strdup([why UTF8String]);
            return r;
        }

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

        vellum_copy_data(rtfData, &r.rtf, &r.rtfLen);
        vellum_copy_data([plain dataUsingEncoding:NSUTF8StringEncoding],
                         &r.plain, &r.plainLen);
        return r;
    }
}

// vellum_write_pasteboard declares all three representations and sets
// them in one transaction, so no paste target can observe a half-written
// clipboard. clipHTML is a body fragment, not a document: Slack and
// similar rich-paste targets reject a full <head><style> document but
// accept a fragment. Returns the new changeCount, or a negative code:
//   -1  setData failed for one of the representations
static long vellum_write_pasteboard(
    const void *rtfBytes, int rtfLen,
    const void *clipHTMLBytes, int clipHTMLLen,
    const void *plainBytes, int plainLen) {
    @autoreleasepool {
        NSData *rtfData      = [NSData dataWithBytes:rtfBytes      length:rtfLen];
        NSData *clipHTMLData = [NSData dataWithBytes:clipHTMLBytes length:clipHTMLLen];
        NSData *plainData    = [NSData dataWithBytes:plainBytes    length:plainLen];

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
        if (!ok) { return -1; }

        return newCount > 0 ? newCount : 1;
    }
}

// vellum_set_file_refs writes Finder-compatible file references.
// paths is a C string array of absolute paths; n is the count.
//
// Uses NSFilenamesPboardType (string name to avoid deprecation warnings)
// with declareTypes/setPropertyList so the pasteboard server owns the
// data. writeObjects: of NSURLs looks fine in-process but the file-URL
// items evaporate when the writing process exits — unusable for a CLI.
// Returns the new pasteboard changeCount on success, or a negative code:
//   -1  no paths / invalid
//   -2  setPropertyList failed
static long vellum_set_file_refs(const char **paths, int n) {
    @autoreleasepool {
        if (n <= 0 || paths == NULL) { return -1; }

        NSMutableArray<NSString *> *filenames = [NSMutableArray arrayWithCapacity:(NSUInteger)n];
        for (int i = 0; i < n; i++) {
            if (paths[i] == NULL) { continue; }
            NSString *p = [NSString stringWithUTF8String:paths[i]];
            if (p.length == 0) { continue; }
            [filenames addObject:p];
        }
        if (filenames.count == 0) { return -1; }

        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        // String type name: the NSFilenamesPboardType symbol is deprecated
        // but the pasteboard type string remains what Finder and most apps
        // read for file-copy. declareTypes copies data into pboardd.
        NSString *filenamesType = @"NSFilenamesPboardType";
        long newCount = [pb declareTypes:@[filenamesType] owner:nil];
        if (![pb setPropertyList:filenames forType:filenamesType]) {
            return -2;
        }
        return newCount > 0 ? newCount : 1;
    }
}

// vellum_read_file_refs fills *outPaths with a newly allocated C string
// array of absolute paths (caller frees each string and the array with
// free()). *outCount is set to the length. Returns 0 on success (including
// empty), -1 on allocation failure.
static int vellum_read_file_refs(char ***outPaths, int *outCount) {
    @autoreleasepool {
        *outPaths = NULL;
        *outCount = 0;
        NSPasteboard *pb = [NSPasteboard generalPasteboard];

        NSArray<Class> *classes = @[[NSURL class]];
        NSDictionary *options = @{ NSPasteboardURLReadingFileURLsOnlyKey: @YES };
        NSArray *urls = [pb readObjectsForClasses:classes options:options];
        if (urls.count == 0) {
            // Legacy Finder copies may only expose NSFilenamesPboardType.
            // Use the type string to avoid deprecated-symbol warnings.
            NSArray *filenames = [pb propertyListForType:@"NSFilenamesPboardType"];
            if (![filenames isKindOfClass:[NSArray class]] || filenames.count == 0) {
                return 0;
            }
            int n = (int)filenames.count;
            char **arr = (char **)calloc((size_t)n, sizeof(char *));
            if (!arr) { return -1; }
            int wrote = 0;
            for (id obj in filenames) {
                if (![obj isKindOfClass:[NSString class]]) { continue; }
                const char *utf8 = [(NSString *)obj fileSystemRepresentation];
                if (!utf8) { continue; }
                arr[wrote] = strdup(utf8);
                if (!arr[wrote]) {
                    for (int i = 0; i < wrote; i++) free(arr[i]);
                    free(arr);
                    return -1;
                }
                wrote++;
            }
            *outPaths = arr;
            *outCount = wrote;
            return 0;
        }
        int n = (int)urls.count;
        char **arr = (char **)calloc((size_t)n, sizeof(char *));
        if (!arr) { return -1; }
        int wrote = 0;
        for (NSURL *url in urls) {
            if (!url.isFileURL) { continue; }
            NSString *path = url.path;
            if (path.length == 0) { continue; }
            const char *utf8 = [path fileSystemRepresentation];
            if (!utf8) { continue; }
            arr[wrote] = strdup(utf8);
            if (!arr[wrote]) {
                for (int i = 0; i < wrote; i++) free(arr[i]);
                free(arr);
                return -1;
            }
            wrote++;
        }
        *outPaths = arr;
        *outCount = wrote;
        return 0;
    }
}
*/
import "C"

import (
	"errors"
	"fmt"
	"path/filepath"
	"unsafe"

	"github.com/marcelocantos/vellum/internal/pandoc"
)

func writePayload(p Payload) (WriteReport, error) {
	report := WriteReport{Route: RouteAppKit}
	rtf, plain, err := appKitRichText(p.HTML)
	if err != nil {
		prtf, pplain, perr := pandocRichText(p.HTML)
		if perr != nil {
			return WriteReport{}, fmt.Errorf(
				"clipboard: AppKit rich text unavailable (%v) and the %s fallback failed: %w",
				err, pandoc.Binary, perr)
		}
		rtf, plain = prtf, pplain
		report = WriteReport{Route: RoutePandoc, Fallback: err.Error()}
	}

	clipHTML := []byte(htmlBodyFragment(p.HTML))
	if len(clipHTML) == 0 {
		clipHTML = []byte(p.HTML)
	}
	code := C.vellum_write_pasteboard(
		unsafe.Pointer(&rtf[0]), C.int(len(rtf)),
		unsafe.Pointer(&clipHTML[0]), C.int(len(clipHTML)),
		unsafe.Pointer(&plain[0]), C.int(len(plain)),
	)
	switch {
	case code > 0:
		return report, nil
	case code == -1:
		return WriteReport{}, fmt.Errorf("clipboard: NSPasteboard setData failed")
	default:
		return WriteReport{}, fmt.Errorf("clipboard: NSPasteboard write produced no changeCount advance")
	}
}

// appKitRichText renders HTML to RTF and plain text through
// NSAttributedString. An error here means the route is unavailable in
// this process, not that the payload is bad — see the C comment on
// vellum_html_to_rich_text.
func appKitRichText(html string) (rtf, plain []byte, err error) {
	src := []byte(html)
	r := C.vellum_html_to_rich_text(unsafe.Pointer(&src[0]), C.int(len(src)))
	if r.err != nil {
		defer C.free(unsafe.Pointer(r.err))
		return nil, nil, errors.New(C.GoString(r.err))
	}
	defer C.free(r.rtf)
	defer C.free(r.plain)
	if r.rtfLen == 0 || r.plainLen == 0 {
		return nil, nil, errors.New("AppKit produced an empty rich-text rendering")
	}
	return C.GoBytes(r.rtf, r.rtfLen), C.GoBytes(r.plain, r.plainLen), nil
}

// readPasteboardData returns the raw bytes for the given UTI on the
// general pasteboard, or nil if the type is not present.
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

// readClipboard returns the clipboard data for the named [Format], or nil
// if absent. macOS-specific UTI mapping happens here.
func readClipboard(format string) ([]byte, error) {
	switch format {
	case FormatRTF:
		return readPasteboardData("public.rtf"), nil
	case FormatHTML:
		return readPasteboardData("public.html"), nil
	case FormatPDF:
		// PowerPoint slide copies often use com.adobe.pdf; macOS also
		// uses public.pdf. Prefer public.pdf then Adobe flavour.
		if b := readPasteboardData("public.pdf"); len(b) > 0 {
			return b, nil
		}
		if b := readPasteboardData("com.adobe.pdf"); len(b) > 0 {
			return b, nil
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("clipboard: unknown format %q", format)
	}
}

func writeFileRefs(paths []string) error {
	abs := make([]string, 0, len(paths))
	for _, p := range paths {
		a, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("clipboard: resolving path %q: %w", p, err)
		}
		abs = append(abs, a)
	}

	cPaths := make([]*C.char, len(abs))
	for i, p := range abs {
		cPaths[i] = C.CString(p)
		defer C.free(unsafe.Pointer(cPaths[i]))
	}
	var ptr **C.char
	if len(cPaths) > 0 {
		ptr = (**C.char)(unsafe.Pointer(&cPaths[0]))
	}
	code := C.vellum_set_file_refs(ptr, C.int(len(cPaths)))
	switch {
	case code > 0:
		return nil
	case code == -1:
		return fmt.Errorf("clipboard: no valid paths for file reference write")
	case code == -2:
		return fmt.Errorf("clipboard: NSFilenamesPboardType write failed")
	default:
		return fmt.Errorf("clipboard: file reference write produced no changeCount advance")
	}
}

func readFileRefs() ([]string, error) {
	var outPaths **C.char
	var outCount C.int
	if rc := C.vellum_read_file_refs(&outPaths, &outCount); rc != 0 {
		return nil, fmt.Errorf("clipboard: reading file references failed")
	}
	if outCount == 0 || outPaths == nil {
		return nil, nil
	}
	defer func() {
		// Free each C string and the array.
		slice := unsafe.Slice(outPaths, int(outCount))
		for i := 0; i < int(outCount); i++ {
			if slice[i] != nil {
				C.free(unsafe.Pointer(slice[i]))
			}
		}
		C.free(unsafe.Pointer(outPaths))
	}()

	slice := unsafe.Slice(outPaths, int(outCount))
	paths := make([]string, 0, int(outCount))
	for i := 0; i < int(outCount); i++ {
		if slice[i] == nil {
			continue
		}
		p := C.GoString(slice[i])
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			paths = append(paths, p)
			continue
		}
		paths = append(paths, abs)
	}
	return paths, nil
}
