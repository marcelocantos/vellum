#!/usr/bin/env python3
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0
"""pdf-anchor-check.py PDF NAME — exit 0 when the PDF has an internal link
annotation for #NAME (a named /Dest that the PDF defines, as WeasyPrint
writes, or an explicit /Dest array, as Prince writes) and no external URI
for it. Stdlib only: Flate streams are inflated and scanned as text."""
import re
import sys
import zlib

pdf_path, name = sys.argv[1], sys.argv[2]
raw = open(pdf_path, "rb").read()
parts = [raw]
for m in re.finditer(rb"stream\r?\n(.*?)\r?\nendstream", raw, re.S):
    try:
        parts.append(zlib.decompress(m.group(1)))
    except zlib.error:
        pass
objs = b"\n".join(parts).decode("latin-1")

if re.search(r"/URI\s*\([^)]*#" + re.escape(name) + r"\)", objs):
    sys.exit(f"#{name} is an external URI link, not an internal jump")
links = [m.start() for m in re.finditer(r"/Subtype\s*/Link\b", objs)]
if not links:
    sys.exit("no link annotations in the PDF")
defined = re.search(r"\(" + re.escape(name) + r"\)\s*\[\s*\d+\s+\d+\s+R\s*/XYZ", objs)
if re.search(r"/Dest\s*\(" + re.escape(name) + r"\)", objs):
    if not defined:
        sys.exit(f"link to named destination {name!r}, which the PDF does not define")
    print(f"#{name}: named destination")
    sys.exit(0)
if re.search(r"/Dest\s*\[\s*\d+\s+\d+\s+R\s*/XYZ", objs):
    print(f"#{name}: explicit destination")
    sys.exit(0)
sys.exit(f"no internal destination for #{name}")
