# Confluence rendered-page corpus

This corpus tests what readers see after Markdown becomes ADF and is published
as a Confluence page. ADF JSON checks are useful diagnostics; they do not pass
a visual scenario. Keep the source documents fixed while changing the
converter. Add a new scenario when an exploratory pass finds a new failure,
and retain the old one as a regression case.

The existing `adf` package is conversion-only. It does not upload local media
or publish pages. Until that path exists, a scenario that needs it is
**blocked**, not passed. Publish into disposable pages in a test space; never
overwrite an owner's page to run this corpus.

| ID | Document | Main challenge | What the final page must show |
| --- | --- | --- | --- |
| C01 | [table-shapes.md](table-shapes.md) | Unequal, dense, and aligned columns | A wide table gets room beyond prose width; short identifiers stay readable; column widths respond to content rather than becoming uniform. |
| C02 | [table-content.md](table-content.md) | Wrapping and mixed cell content | Prose wraps without crushing short columns; links, code, and emphasis remain inside their cells; no text is clipped. |
| C03 | [raster-media.md](raster-media.md) | Photo and line-art sizing | The photo uses the available reading width without distortion; the labeled line art remains legible. |
| C04 | [vector-diagrams.md](vector-diagrams.md) | Wide, tall, and small SVGs plus Mermaid | The wide diagram is readable, the tall diagram fits without clipping, the badge stays small, and Mermaid is visible through the chosen macro or an explicit fallback. |
| C05 | [media-in-tables.md](media-in-tables.md) | Image in a table cell | A cell image stays associated with its row and does not force every other column into a sliver. |
| C06 | [rich-structure.md](rich-structure.md) | Non-layout Markdown fidelity | Lists, tasks, code, quotes, links, math, and footnotes keep their content and order; declared fallbacks are visible. |
| C07 | [integrated-report.md](integrated-report.md) | Realistic mixed page | Tables, media, code, and prose coexist without a layout fix for one block damaging the others. |
| C08 | [missing-media.md](missing-media.md) | Failure path | Publishing fails with the missing asset named; no page silently contains a broken or tiny placeholder. |

The media files are deliberately relative to these documents. C03–C05 and C07
require the eventual publisher to resolve and upload local assets. C01, C02,
and C06 can be exercised by the present pure ADF converter. Today, C03–C05,
C07, and C08 stop at `ImagePolicyExternal requires http(s) URL`; that is a
known blocker, not a visual result. C08 must ultimately report the missing
file rather than merely rejecting relative media. The existing assets are
`../desk-photo.jpg` (900×600), `../line-art.png` (520×360), and
`../pipeline.svg` (520×160); C04 adds local wide, tall, and badge SVGs.

## Playwright review loop

1. Freeze the corpus at a commit. For each document, convert and publish to
   its own disposable Confluence page. Record the source commit, converter
   commit, ADF hash, page URL, page version, and media upload results. An
   upload or publishing failure is **blocked** or **fail**, never visual pass.
2. Use Playwright on the **published Confluence URL**, with a signed-in test
   account, at desktop and narrow browser widths. Wait for the document
   marker, scroll through the page to trigger lazy media, wait for fonts and
   images, then capture full-page screenshots and DOM geometry. The capture
   helper is [`scripts/confluence-layout-capture.py`](../../scripts/confluence-layout-capture.py).
   Write screenshots and measurements to a scratch directory outside the
   repository, not to `test/output` or another synced directory.
   For example, after an agent has saved a test account's Playwright storage
   state outside the repo:

   ```text
   python scripts/confluence-layout-capture.py \
     --url https://YOUR-SITE.atlassian.net/wiki/spaces/TEST/pages/PAGE-ID \
     --marker 'C01 — Table shapes' \
     --storage-state /private/tmp/confluence-auth.json \
     --out /private/tmp/vellum-confluence-C01-pass1
   ```

   The helper refuses in-repository captures. Its `--self-test` mode checks
   the capture machinery on a local file; those images never count as
   Confluence evidence. If Confluence places page content outside `main`, use
   `--content-selector` for the rendered article and record that selector.
   A missing marker, incomplete image, or `render_stable: false` needs
   investigation before a visual verdict.
3. Have a validation agent inspect each screenshot and the measured table,
   image, and SVG boxes. Compare against the source document and the outcome
   column above. For every issue, name the fixture, viewport, page URL,
   screenshot, observed geometry, and likely mechanism. Distinguish source
   loss, ADF shape, media transport, Confluence rendering, and sizing.
4. Fix one named mechanism, then republish **all** scenarios and repeat the
   Playwright pass. A fix cannot pass only the example it was tuned against.
   Preserve earlier screenshots for before/after comparison. Add a fixture
   when a new failure class appears; do not silently loosen an existing one.

For C01–C07, the measurable checks are: all expected headings and blocks are
present in order; no cell text or media is clipped; table columns do not
overlap; images load and keep their intrinsic aspect ratio; the page has no
unintended horizontal overflow. A dense table may scroll horizontally on a
narrow viewport if its content remains readable. The validation agent also
judges whether the available space is being used sensibly; that judgement
must cite the captured page, not a predicted ADF layout. Record **pass**,
**fail**, or **blocked** separately for desktop and narrow views.

Treat output quality as settled only after two complete passes on the same
frozen corpus produce no new high-impact findings, all required scenarios
pass in both viewports, and the owner accepts the remaining visual choices
once from the final screenshots. A green ADF unit suite alone is not this
gate. If the Confluence site or test account is unavailable, report the live
pass as blocked and keep the structural checks separate.
