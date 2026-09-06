# Conversion performance baseline

Recorded 2026-09-06 on an Apple M4 Max (16 cores), macOS 26, Go 1.26,
WeasyPrint and Poppler from Homebrew. Fixtures come from
`internal/perffixture` and are generated into a tempdir; nothing large
is checked in.

Every wall-clock number below was taken on an **idle machine**. An
earlier draft of this document carried figures measured while a dozen
agents were building and testing in parallel; those were 1.5–2× slower
across the board and showed a spurious WeasyPrint regression. They have
been replaced, not adjusted.

Two kinds of number live here:

- **Allocation metrics** (allocs and bytes per operation) for the
  pure-Go paths. These are deterministic for a fixed input and are
  locked in both directions by `docs/perf/baseline.yaml`, checked by
  the `TestRatchet_*` tests in `convert` and `importer` on every
  `cv gate`. A change that moves them by more than the band fails
  until the baseline is re-recorded in the same commit.
- **Wall-clock numbers** from `cv bench` and from pairwise CLI runs.
  These are reported, not gated. Anything that spawns WeasyPrint,
  node, or Poppler carries their process cost, which varies far more
  than the Go code around it.

## Before and after

Command, identical on both sides:

```
go test -run '^$' -bench . -benchmem -benchtime=5x ./convert ./importer
```

Before is master `e090840` with the benchmark files from `0699813`
checked out over it, so both sides run the same harness on the same
fixtures. After is `262e1af`, the head of `burst/perf`.

| Benchmark | Before | After | Mechanism |
|---|---|---|---|
| Render/prose (1.1 MiB, no math) | 66.2 ms, 273 MB | 15.5 ms, 23.0 MB | Inline-code placeholders were restored with one `strings.Replace` each, copying the whole document per span. Now one pass. The math and Mermaid regexes also skip documents with no `$` and no Mermaid fence. |
| Render/code (chroma-heavy) | 216 ms, 348 MB | 174 ms, 125 MB | Same placeholder fix. The remaining cost is chroma tokenising, 2.34M allocs on both sides, unchanged. |
| Render/math (2,400 expressions, node) | 5.78 s, 6.57 GB | 0.285 s, 76.5 MB | Math placeholders substituted in one pass instead of one whole-document copy per expression. |
| RenderMermaid (200 stubbed diagrams) | 21.0 ms, 164 MB | 3.2 ms, 15.0 MB | Same fix for Mermaid placeholders. |
| ConvertPDF/prose (WeasyPrint) | 2.15–4.20 s, 5.93 MB | 2.17–2.49 s, 2.03 MB | Wall time unchanged: it is WeasyPrint, and the spread within either binary exceeds the gap between them. Only vellum's own allocations moved. |
| ConvertPDF/images (WeasyPrint, 200 PNGs) | 3.18–3.51 s, 3.75 MB | 3.18–3.61 s, 1.07 MB | As above. |
| ImportPDF (40 pages, Poppler) | 0.981 s | 0.218 s | pdftotext first, which is cheap and yields the page count, then pdftoppm fans out over page ranges bounded by CPU count. |
| BundleDir (200 cached bundles, 4,000 files) | 41.6 ms | 4.1 ms | The prune walk runs at most once per ten minutes, recorded by a stamp file. The first call in an interval still walks, which is what the residual 4 ms is. |
| AbsolutizeMedia (2,000 refs) | 33.6 ms | 28.1 ms | Unchanged code; the gap is run-to-run noise. See "Not changed" below. |

The ConvertPDF rows are quoted as ranges because they were sampled four
times each, alternating binaries, precisely because a single pair
suggested a regression that repeated sampling did not support.

## Pairwise CLI runs, master vs branch

The shipped binary on the shipped code path, not the benchmark harness.
Binaries alternated, three rounds each, idle machine.

| Conversion | master | branch |
|---|---|---|
| Markdown → HTML, math fixture (4.9 MB HTML out) | 5.72 / 5.53 / 5.47 s | 0.32 / 0.32 / 0.32 s |
| PDF import, 40 pages, cold cache | 1.12 / 1.05 / 1.06 s | 0.18 / 0.18 / 0.26 s |

## Output equivalence

The changes are meant to be pure performance. Checked by running each
fixture through both binaries and comparing bytes:

| Fixture | Result |
|---|---|
| prose.md → HTML | identical (1,402,263 bytes) |
| math.md → HTML (live node) | identical (4,899,375 bytes) |
| fixture.pdf → Markdown (40 pages) | identical after normalising the cache directory name |
| fixture.pdf → page PNGs | all 40 byte-identical between binaries |

The PDF page images matter more than the Markdown: rendering pages
concurrently across several pdftoppm processes could in principle
produce different rasters, and does not.

Mermaid with a live `mmdc` was not compared because its SVG output
carries generated ids; the stubbed ratchet covers the substitution.

## Re-recording

When a change legitimately moves an allocation metric, re-record it in
the same commit, once per build mode, and update the table above:

```
VELLUM_PERF_RECORD=1 go test -p 1 -race -run Ratchet ./convert ./importer
VELLUM_PERF_RECORD=1 go test -p 1 -run Ratchet ./convert ./importer
cv bench
```

The ratchet has been checked in both directions: with a baseline
halved it reports "regressed", with a baseline doubled it reports
"improved … lock it in by re-recording", and neither passes.

## Not changed, worth knowing

- `Render/code` spends its time inside chroma; the only lever is
  switching highlighter or class output, which changes the HTML.
- `AbsolutizeMedia` runs each image regex twice per match and stats up
  to three candidate paths per reference. At 14 µs per reference it is
  far below the pandoc process that precedes it.
- The MCP `convert` tool does not cache converted output; only the
  viewer does, keyed by source path, size and mtime. A cache keyed the
  same way would not notice edits to referenced images, so this was
  left as a product decision rather than a perf fix.
