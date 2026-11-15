// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Editor journey: the shipped binary's viewer, a real Chrome, a real
// Markdown file on disk. Opens the editor, saves, survives outside edits,
// resolves a conflict, closes back into a refreshed view, and checks
// highlighting, theme and layout from the render.
//
//   VELLUM_BINARY=/path/to/vellum node editor/journey.cjs
//
// Needs `npm ci` in editor/ and Google Chrome. Screenshots land in
// $JOURNEY_SHOTS when set.

const { chromium } = require("playwright");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawn } = require("node:child_process");

const ADDR = "127.0.0.1:19851";
const ORIGIN = "http://" + ADDR;

// Every scroll probe lands on a numbered token, so a position in the page
// and one in the editor compare by number: filler paragraphs (A0…, B0…),
// lines of an untagged fenced block, and words of one long wrapped line.
function fixture() {
  const filler = (tag) =>
    Array.from({ length: 40 }, (_, i) => "Paragraph " + tag + i + " of filler prose.\n").join("\n");
  const code = Array.from({ length: 60 }, (_, i) => "code line " + i).join("\n");
  const words = Array.from({ length: 400 }, (_, i) => "w" + i).join(" ");
  return (
    "# Editor journey\n\nIntro.\n\n## Section A\n\n" +
    "Mixed **bold** and [a link](https://example.com/x \"t\") with `code`, an &amp; entity, " +
    "\u201cquotes\u201d and <em>html</em> end.\n\n" +
    "the cat and the dog and the bird\n\n- [x] done task\n- [ ] open task\n\n" +
    filler("A") +
    "\n```text\n" + code + "\n```\n\n" + words + "\n\n" +
    "## Section B\n\n- first item\n- second item\n\n```go\nfunc main() {\n\tprintln(\"hi\")\n}\n```\n\n" +
    filler("B") + "\n## Section C\n\nEnd.\n"
  );
}

// Slack in source lines (words, for the long line) between what the two
// sides show at the top.
const TOLERANCE = { Paragraph: 1, code: 3, w: 8 };

// The first and last numbered tokens on the top visual line of what the
// reader sees: in the page (below the pinned title) or in the editor. A
// wrapped line breaks at different words on each side, so positions
// compare as ranges.
function probe(page, where) {
  return page.evaluate((where) => {
    function tokenAt(x, y) {
      const r = document.caretRangeFromPoint(x, y);
      if (!r || r.startContainer.nodeType !== Node.TEXT_NODE) return "";
      const text = r.startContainer.textContent;
      const off = r.startOffset;
      let best = "";
      let bestDist = Infinity;
      for (const m of text.matchAll(/Paragraph [AB]\d+|code line \d+|\bw\d+/g)) {
        const end = m.index + m[0].length;
        const d = off < m.index ? m.index - off : off > end ? off - end : 0;
        if (d < bestDist) {
          best = m[0];
          bestDist = d;
        }
      }
      return best;
    }
    let top, left, right;
    if (where === "view") {
      const pane = document.querySelector(".vellum-scroll").getBoundingClientRect();
      const title = document.querySelector(".vellum-pin-title");
      const p = document.querySelector(".vellum-article p").getBoundingClientRect();
      top = title ? Math.max(pane.top, title.getBoundingClientRect().bottom) : pane.top;
      left = p.left + 24;
      right = p.right - 4;
    } else {
      const content = document.querySelector(".cm-content").getBoundingClientRect();
      top = document.querySelector(".cm-scroller").getBoundingClientRect().top;
      left = content.left + 24;
      right = content.right - 24;
    }
    // Step past blank lines and margins to the first line with a token.
    for (let dy = 2; dy < 120; dy += 4) {
      const first = tokenAt(left, top + dy);
      if (first) return { first: first, last: tokenAt(right, top + dy) || first };
    }
    return { first: "", last: "" };
  }, where);
}

function parseToken(tok) {
  const m = String(tok).match(/^(Paragraph|code|w)\D*?([AB]?)(\d+)$/);
  return m ? { kind: m[1], group: m[2], n: Number(m[3]) } : null;
}

// a and b are probe results or bare tokens; they are near when their
// ranges overlap within the kind's tolerance.
function near(a, b) {
  const range = (x) => (typeof x === "string" ? { first: x, last: x } : x);
  const ra = range(a);
  const rb = range(b);
  const a0 = parseToken(ra.first);
  const a1 = parseToken(ra.last) || a0;
  const b0 = parseToken(rb.first);
  const b1 = parseToken(rb.last) || b0;
  if (!a0 || !b0 || a0.kind !== b0.kind || a0.group !== b0.group) return false;
  const tol = TOLERANCE[a0.kind];
  return a0.n - tol <= b1.n && b0.n - tol <= a1.n;
}

function show(p) {
  return typeof p === "string" ? p : p.first === p.last ? p.first : p.first + "…" + p.last;
}

// Select in the page from the nth occurrence of startText (counted
// within the first text node containing hint) through the next endText,
// scrolled into view as a reader would have it.
function selectInView(page, hint, startText, nth, endText) {
  return page.evaluate(([hint, startText, nth, endText]) => {
    const walker = document.createTreeWalker(document.querySelector(".vellum-article"), NodeFilter.SHOW_TEXT);
    const nodes = [];
    let n;
    while ((n = walker.nextNode())) nodes.push(n);
    const at = nodes.findIndex((node) => node.textContent.includes(hint));
    if (at < 0) return false;
    let i = -1;
    for (let k = 0; k <= nth; k++) {
      i = nodes[at].textContent.indexOf(startText, i + 1);
      if (i < 0) return false;
    }
    let end = null;
    for (let k = at; k < nodes.length && !end; k++) {
      const j = nodes[k].textContent.indexOf(endText, k === at ? i : 0);
      if (j >= 0) end = { node: nodes[k], offset: j + endText.length };
    }
    if (!end) return false;
    const r = document.createRange();
    r.setStart(nodes[at], i);
    r.setEnd(end.node, end.offset);
    const pane = document.querySelector(".vellum-scroll");
    pane.scrollTop += r.getBoundingClientRect().top - pane.getBoundingClientRect().top - 200;
    getSelection().removeAllRanges();
    getSelection().addRange(r);
    return true;
  }, [hint, startText, nth, endText]);
}

// The editor's selection: its text and the text before it on its line.
function editorSelection(page) {
  return page.evaluate(() => {
    const s = getSelection();
    if (!s.rangeCount) return null;
    const r = s.getRangeAt(0);
    const line = (r.startContainer.nodeType === Node.TEXT_NODE ? r.startContainer.parentElement : r.startContainer).closest(".cm-line");
    if (!line) return null;
    const before = document.createRange();
    before.setStart(line, 0);
    before.setEnd(r.startContainer, r.startOffset);
    return { text: s.toString(), before: before.toString(), line: line.textContent };
  });
}

const squash = (x) => String(x).replace(/\s+/g, "");

// Scroll the page so token sits at the top of what the reader sees.
function scrollViewTo(page, token) {
  return page.evaluate((token) => {
    const pane = document.querySelector(".vellum-scroll");
    const walker = document.createTreeWalker(document.querySelector(".vellum-article"), NodeFilter.SHOW_TEXT);
    const re = new RegExp("(^|\\W)" + token + "(?!\\d)");
    let node;
    while ((node = walker.nextNode())) {
      const m = node.textContent.match(re);
      if (!m) continue;
      const range = document.createRange();
      const at = m.index + m[1].length;
      range.setStart(node, at);
      range.setEnd(node, at + token.length);
      for (let pass = 0; pass < 2; pass++) {
        const title = document.querySelector(".vellum-pin-title");
        const paneTop = pane.getBoundingClientRect().top;
        const top = title ? Math.max(paneTop, title.getBoundingClientRect().bottom) : paneTop;
        pane.scrollTop += range.getBoundingClientRect().top - top - 1;
      }
      return true;
    }
    return false;
  }, token);
}

// Scroll the editor so token's line is at its top.
function scrollEditorTo(page, token) {
  return page.evaluate(async (token) => {
    const sc = document.querySelector(".cm-scroller");
    sc.scrollTop = 0;
    await new Promise((r) => requestAnimationFrame(r));
    for (let i = 0; i < 200; i++) {
      const line = [...document.querySelectorAll(".cm-line")].find((l) => new RegExp("(^|\\W)" + token + "(?!\\d)").test(l.textContent));
      if (line) {
        // Heights of lines not yet drawn are estimates; settle in passes.
        for (let pass = 0; pass < 5; pass++) {
          const l = [...document.querySelectorAll(".cm-line")].find((e) => new RegExp("(^|\\W)" + token + "(?!\\d)").test(e.textContent));
          if (!l) break;
          const delta = l.getBoundingClientRect().top - sc.getBoundingClientRect().top;
          if (Math.abs(delta) < 1) return true;
          sc.scrollTop += delta;
          await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
        }
        return true;
      }
      // CodeMirror renders only near the viewport; walk down to find it.
      sc.scrollTop += sc.clientHeight / 2;
      await new Promise((r) => requestAnimationFrame(r));
    }
    return false;
  }, token);
}

function check(cond, msg) {
  if (!cond) throw new Error(msg);
}

async function waitFor(fn, msg, ms = 5000) {
  const end = Date.now() + ms;
  for (;;) {
    if (await fn()) return;
    if (Date.now() > end) throw new Error("timed out: " + msg);
    await new Promise((r) => setTimeout(r, 50));
  }
}

(async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "vellum-editor-journey-"));
  const file = path.join(dir, "doc.md");
  fs.writeFileSync(file, fixture());
  fs.chmodSync(file, 0o640);
  const server = spawn(process.env.VELLUM_BINARY || "./vellum", ["serve-view", "--addr", ADDR], {
    stdio: "inherit",
    env: Object.assign({}, process.env, { HOME: dir }),
  });
  const browser = await chromium.launch({ headless: true, channel: "chrome" });
  const shots = process.env.JOURNEY_SHOTS || "";
  try {
    await waitFor(async () => {
      try {
        return (await fetch(ORIGIN + "/healthz")).ok;
      } catch (e) {
        return false;
      }
    }, "server health");

    const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
    const page = await context.newPage();
    // Fail a stuck step quickly rather than after Playwright's 30s.
    page.setDefaultTimeout(10000);
    const errors = [];
    const foreign = [];
    page.on("pageerror", (e) => errors.push(String(e)));
    page.on("request", (r) => {
      if (!r.url().startsWith(ORIGIN) && !r.url().startsWith("data:")) foreign.push(r.url());
    });
    const mod = process.platform === "darwin" ? "Meta" : "Control";

    await page.goto(ORIGIN + file);
    await page.waitForSelector(".vellum-article h2");
    await page.evaluate(() => (window.__journeyMarker = 1));

    // Scroll position carries both ways: the editor opens showing what
    // the page showed, and closing (clean, no reload) puts the page where
    // the editor was. Cases: filler paragraphs, inside an untagged fenced
    // block, partway through one long wrapped line, the top.
    const openEditor = async () => {
      await page.keyboard.press("e");
      await page.waitForSelector(".vellum-editor:not([hidden]) .cm-editor");
      await waitFor(() => page.evaluate(() => document.activeElement && document.activeElement.classList.contains("cm-content")), "editor focus");
      await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
    };
    // The first Escape clears an editor selection (CodeMirror's own
    // binding); a bare Escape closes.
    const closeEditor = async () => {
      for (let i = 0; i < 2; i++) {
        await page.keyboard.press("Escape");
        if (await page.locator(".vellum-editor").isHidden()) return;
      }
      await page.waitForSelector(".vellum-editor", { state: "hidden", timeout: 2000 });
    };
    for (const token of ["Paragraph A12", "code line 30", "w200", "Paragraph B25", "Paragraph A37"]) {
      check(await scrollViewTo(page, token), "fixture lacks " + token);
      const seen = await probe(page, "view");
      check(near(seen, token), "page probe " + show(seen) + " after scrolling to " + token);
      await openEditor();
      const got = await probe(page, "editor");
      check(near(got, seen), "editor opened at " + show(got) + ", page showed " + show(seen));
      await closeEditor();
      const back = await probe(page, "view");
      check(near(back, seen), "page came back at " + show(back) + ", was " + show(seen));
    }
    await page.evaluate(() => (document.querySelector(".vellum-scroll").scrollTop = 0));
    await openEditor();
    check(
      await page.evaluate(() => document.querySelector(".cm-scroller").scrollTop === 0),
      "editor not at the top for a page at the top"
    );
    // Editor → page: move in the editor, close, and the page follows.
    for (const token of ["Paragraph B10", "code line 45"]) {
      check(await scrollEditorTo(page, token), "editor lacks " + token);
      const seen = await probe(page, "editor");
      check(near(seen, token), "editor probe " + show(seen) + " after scrolling to " + token);
      await closeEditor();
      const got = await probe(page, "view");
      check(near(got, seen), "page landed at " + show(got) + ", editor showed " + show(seen));
      await openEditor();
    }
    await closeEditor();
    check(await page.evaluate(() => window.__journeyMarker === 1), "a clean close reloaded the page");

    // A page selection becomes the same text selected in the source,
    // through Markdown syntax, link URLs, entities, tags, task and list
    // markers, a repeated word, a fenced block and a long multi-block run.
    const selectionCases = [
      {
        sel: ["bold", "bold", 0, "end"],
        want: 'bold** and [a link](https://example.com/x "t") with `code`, an &amp; entity, \u201cquotes\u201d and <em>html</em> end',
      },
      { sel: ["the cat", "the", 1, "the"], want: "the", before: "the cat and " },
      { sel: ["the cat", "dog and the", 0, "bird"], want: "dog and the bird" },
      { sel: ["code line 30", "line 30", 0, "line 30"], want: "line 30", line: "code line 30" },
      { sel: ["done task", "done", 0, "open"], want: "done task\n- [ ] open" },
      {
        sel: ["Paragraph A3 ", "filler", 0, "A6"],
        want: "filler prose.\n\nParagraph A4 of filler prose.\n\nParagraph A5 of filler prose.\n\nParagraph A6",
      },
    ];
    for (const [i, c] of selectionCases.entries()) {
      check(await selectInView(page, ...c.sel), "could not select " + c.sel.join(" / "));
      // Alternate the two ways in: the toolbar button and the e key.
      if (i % 2 === 0) await page.click('[data-vellum="edit"]');
      else await page.keyboard.press("e");
      await page.waitForSelector(".vellum-editor:not([hidden]) .cm-editor");
      await waitFor(async () => {
        const got = await editorSelection(page);
        return got && got.text.length > 0;
      }, "editor selection for " + c.want);
      const got = await editorSelection(page);
      check(squash(got.text) === squash(c.want), "editor selected " + JSON.stringify(got.text) + ", want " + JSON.stringify(c.want));
      if (c.before !== undefined) check(got.before === c.before, "selected the wrong occurrence: before " + JSON.stringify(got.before));
      if (c.line !== undefined) check(got.line === c.line, "selected on line " + JSON.stringify(got.line));
      await closeEditor();
    }
    // No page selection: the editor opens with a bare cursor.
    await page.evaluate(() => getSelection().removeAllRanges());
    await openEditor();
    check((await editorSelection(page)).text === "", "editor opened with a selection nobody made");
    await closeEditor();

    // The rest works around Section B.
    await page.evaluate(() => {
      const pane = document.querySelector(".vellum-scroll");
      const h = [...document.querySelectorAll(".vellum-article h2")].find((e) => e.textContent === "Section B");
      pane.scrollTop += h.getBoundingClientRect().top - pane.getBoundingClientRect().top;
    });
    await openEditor();

    // Fenced Go is highlighted by its lazily loaded language chunk.
    await waitFor(
      () =>
        page.evaluate(() => {
          const line = [...document.querySelectorAll(".cm-line")].find((l) => l.textContent.startsWith("func main"));
          if (!line) return false;
          const span = [...line.querySelectorAll("span")].find((s) => s.textContent === "func");
          return Boolean(span) && getComputedStyle(span).color !== getComputedStyle(line).color;
        }),
      "Go keyword highlighted"
    );
    const heading = await page.evaluate(() => {
      const line = [...document.querySelectorAll(".cm-line")].find((l) => l.textContent === "## Section B");
      const span = line && line.querySelector("span");
      return span ? getComputedStyle(span).fontWeight : "";
    });
    check(Number(heading) >= 700, "heading not bold in the editor: " + heading);

    // Markdown list continuation.
    await page.locator(".cm-line", { hasText: "second item" }).click();
    await page.keyboard.press("End");
    await page.keyboard.press("Enter");
    await page.keyboard.type("third item");
    check(await page.locator(".vellum-editor-state").innerText() === "Unsaved changes", "dirty state not shown");
    check(await page.locator('[data-vellum="ed-save"]').isEnabled(), "Save disabled while dirty");

    if (shots) await page.screenshot({ path: path.join(shots, "editor-light.png") });

    await page.keyboard.press(mod + "+s");
    await waitFor(async () => (await page.locator(".vellum-editor-state").innerText()) === "Saved", "saved state");
    check(fs.readFileSync(file, "utf8").includes("- second item\n- third item\n"), "list continuation not on disk");
    check((fs.statSync(file).mode & 0o777) === 0o640, "save changed the file mode");
    check(await page.evaluate(() => window.__journeyMarker === 1), "page reloaded while the editor was open");

    // An outside edit while clean: the editor follows it.
    fs.writeFileSync(file, fs.readFileSync(file, "utf8").replace("End.", "End, edited outside."));
    // CodeMirror renders only the visible lines, so check the status and
    // then the line itself once scrolled to.
    await waitFor(async () => (await page.locator(".vellum-editor-state").innerText()) === "Reloaded from disk", "editor follows disk");
    await page.keyboard.press(mod + "+End");
    await waitFor(() => page.locator(".cm-content").innerText().then((t) => t.includes("End, edited outside.")), "followed text in buffer");
    check(await page.evaluate(() => window.__journeyMarker === 1), "outside edit reloaded the page under the editor");

    // An outside edit while dirty: conflict, then overwrite.
    await page.keyboard.press(mod + "+Home");
    await page.locator(".cm-line", { hasText: "Intro." }).click();
    await page.keyboard.press("End");
    await page.keyboard.type(" Mine.");
    fs.writeFileSync(file, fs.readFileSync(file, "utf8").replace("Intro.", "Intro. Theirs."));
    await page.waitForSelector(".vellum-editor-banner:not([hidden])");
    check((await page.locator(".vellum-editor-banner").innerText()).includes("changed on disk"), "no conflict banner");
    await page.getByRole("button", { name: "Overwrite" }).click();
    await waitFor(async () => (await page.locator(".vellum-editor-state").innerText()) === "Saved", "overwrite saved");
    const disk = fs.readFileSync(file, "utf8");
    check(disk.includes("Intro. Mine.") && !disk.includes("Theirs"), "overwrite did not win: " + disk.slice(0, 60));

    // Closing after a save reloads the page at the editor's position.
    check(await scrollEditorTo(page, "Paragraph B12"), "editor lacks Paragraph B18");
    const lastSeen = await probe(page, "editor");
    await page.locator('[data-vellum="ed-close"]').click();
    await page.waitForFunction(() => !window.__journeyMarker, null, { timeout: 5000 }).catch(() => {});
    await page.waitForSelector(".vellum-article h2");
    check(await page.evaluate(() => !window.__journeyMarker), "closing after a save did not refresh the view");
    check((await page.locator(".vellum-article").innerText()).includes("third item"), "view does not show the saved text");
    await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
    const reloadedAt = await probe(page, "view");
    check(near(reloadedAt, lastSeen), "reloaded page at " + show(reloadedAt) + ", editor showed " + show(lastSeen));

    await page.keyboard.press("e");
    await page.waitForSelector(".vellum-editor:not([hidden]) .cm-editor");
    const editorBg = () => page.evaluate(() => getComputedStyle(document.querySelector(".cm-editor")).backgroundColor);
    check((await editorBg()) === "rgb(255, 255, 255)", "light editor background " + (await editorBg()));
    // The toolbar's theme button (under the modal) recolours the open
    // editor live; cycle it to dark through its own click handler.
    await page.evaluate(() => {
      const btn = document.querySelector('[data-vellum="theme"]');
      for (let i = 0; i < 3 && document.documentElement.dataset.vellumTheme !== "dark"; i++) btn.click();
    });
    check((await editorBg()) === "rgb(13, 17, 23)", "dark editor background " + (await editorBg()));
    if (shots) await page.screenshot({ path: path.join(shots, "editor-dark.png") });

    // Layout from the render: the panel sits inside the viewport, the
    // editor scrolls internally, the page does not scroll sideways.
    const box = await page.evaluate(() => {
      const p = document.querySelector(".vellum-editor-panel").getBoundingClientRect();
      const s = document.querySelector(".cm-scroller");
      return {
        l: p.left, t: p.top, r: p.right, b: p.bottom, w: innerWidth, h: innerHeight,
        scrolls: s.scrollHeight > s.clientHeight,
        hover: document.documentElement.scrollWidth > innerWidth,
      };
    });
    check(box.l >= 0 && box.t >= 0 && box.r <= box.w && box.b <= box.h, "panel outside viewport " + JSON.stringify(box));
    check(box.scrolls && !box.hover, "editor scroll/overflow wrong " + JSON.stringify(box));

    // Dirty close asks; Discard leaves the file alone.
    const before = fs.readFileSync(file, "utf8");
    await page.keyboard.press(mod + "+End");
    await page.locator(".cm-line", { hasText: "End, edited outside." }).click();
    await page.keyboard.type(" unsaved");
    await page.keyboard.press("Escape");
    await page.waitForSelector(".vellum-editor-banner:not([hidden])");
    check((await page.locator(".vellum-editor-banner").innerText()).includes("unsaved changes"), "no unsaved prompt");
    await page.getByRole("button", { name: "Discard" }).click();
    await page.waitForSelector(".vellum-editor", { state: "hidden" });
    check(fs.readFileSync(file, "utf8") === before, "discard wrote the file");

    // Phone width: the panel fills the screen.
    await page.setViewportSize({ width: 390, height: 780 });
    await page.keyboard.press("e");
    await page.waitForSelector(".vellum-editor:not([hidden]) .cm-editor");
    const phone = await page.evaluate(() => {
      const p = document.querySelector(".vellum-editor-panel").getBoundingClientRect();
      return { w: p.width, vw: innerWidth, over: document.documentElement.scrollWidth > innerWidth };
    });
    check(Math.abs(phone.w - phone.vw) < 1 && !phone.over, "phone layout " + JSON.stringify(phone));
    if (shots) await page.screenshot({ path: path.join(shots, "editor-phone.png") });

    check(errors.length === 0, "page errors: " + errors.join("; "));
    check(foreign.length === 0, "network requests off the view server: " + foreign.join(", "));
    console.log("PASS: page selection carried into the editor, scroll position both ways (paragraphs, fenced block, wrapped line, top, after reload), highlighting, list continuation, save, follow disk, conflict overwrite, refresh on close, dark, layout, discard, phone, offline");
  } finally {
    await browser.close();
    server.kill();
    fs.rmSync(dir, { recursive: true, force: true });
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
