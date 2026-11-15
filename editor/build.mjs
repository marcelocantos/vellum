// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Bundle the viewer's CodeMirror editor into viewer/editor/, which the Go
// binary embeds and serves under /_vellum/editor/. Run `npm ci && npm run
// build` in this directory after changing src/ or package.json, then
// commit viewer/editor/ alongside the change.
//
// ESM with code splitting: editor.js holds CodeMirror and Markdown;
// each fenced-code language from @codemirror/language-data is its own
// chunk, fetched the first time a document uses it. Chunk names carry a
// content hash, so the server can mark them immutable.

import * as esbuild from "esbuild";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const outdir = path.join(here, "..", "viewer", "editor");

fs.rmSync(outdir, { recursive: true, force: true });

const result = await esbuild.build({
  entryPoints: { editor: path.join(here, "src", "main.js") },
  bundle: true,
  format: "esm",
  splitting: true,
  minify: true,
  target: "es2020",
  outdir: outdir,
  chunkNames: "chunks/c-[hash]",
  legalComments: "none",
  metafile: true,
  logLevel: "warning",
});

// Every bundled npm package's licence, so the embedded tree carries the
// notices its MIT licences require.
const pkgDirs = new Set();
for (const input of Object.keys(result.metafile.inputs)) {
  const m = input.match(/^(.*node_modules\/(?:@[^/]+\/)?[^/]+)\//);
  if (m) pkgDirs.add(path.join(here, m[1]));
}
const notices = [];
for (const dir of [...pkgDirs].sort()) {
  const pkg = JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8"));
  const licenceFile = fs.readdirSync(dir).find((f) => /^licen[cs]e/i.test(f));
  const text = licenceFile ? fs.readFileSync(path.join(dir, licenceFile), "utf8").trim() : "License: " + pkg.license;
  notices.push(pkg.name + " " + pkg.version + "\n\n" + text);
}
fs.writeFileSync(
  path.join(outdir, "THIRD_PARTY_LICENSES.txt"),
  notices.join("\n\n" + "-".repeat(72) + "\n\n") + "\n"
);

let bytes = 0;
let chunks = 0;
for (const [file, info] of Object.entries(result.metafile.outputs)) {
  bytes += info.bytes;
  if (file.includes("/chunks/")) chunks++;
}
console.log(
  "built viewer/editor: " + (bytes / 1024).toFixed(0) + " KiB in " + (chunks + 1) +
  " files, " + pkgDirs.size + " packages"
);
