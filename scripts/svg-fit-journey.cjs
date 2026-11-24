// SVG fit journey: in Chrome and Firefox, at several window widths, an SVG
// in the viewer (a Mermaid diagram or an SVG image alone in a paragraph)
// is drawn as wide as the room allows and no larger than its text
// warrants.
//
// The rule under test: an SVG is drawn at its natural width, or wider
// until its small print reaches 0.75em (12px) or its typical text reaches
// 1em (16px), whichever comes first, and never wider than the panel less
// the article's gutters. Wider than the reading column, it is centred on
// the column.
//
// What decides a pass is measured in the page, not read from the server:
// for the image fixtures the expected widths are worked out by hand from
// the font sizes written below; for Mermaid diagrams the script reads
// every text node's computed font size and drawn scale in the browser and
// applies the rule to those.
//
// Playwright comes from editor/node_modules (`npm ci` there), with Chrome
// and Playwright's Firefox (`npx playwright install firefox`), and mmdc.
const path = require("node:path");
const fs = require("node:fs");
const os = require("node:os");
const { spawn } = require("node:child_process");
const pw = require(path.join(__dirname, "..", "editor", "node_modules", "playwright"));

const port = 19861;
const widths = [1886, 1400, 1100, 700];
const gutter = 32;
const tolerance = 1.5;
const smallPrintPx = 12;
const typicalPx = 16;
const hydrateTimeoutMs = 120000;

// A token in every diagram, so each run renders its own and the first
// load never finds them in the fragment cache.
const run = Date.now().toString(36);

const words = (n, size) =>
  `<text x="10" y="40" font-size="${size}">${"x".repeat(n)}</text>`;
const svg = (attrs, body) => `<svg xmlns="http://www.w3.org/2000/svg" ${attrs}><rect width="100%" height="100%" fill="#ddeeff"/>${body}</svg>\n`;

// name → file body, natural width, and how far the rule lets it grow.
const images = {
  wide: { body: svg('width="1400" height="120" viewBox="0 0 1400 120"', words(60, 16)), natural: 1400, grow: 1 },
  small: { body: svg('width="300" height="100" viewBox="0 0 300 100"', words(40, 8)), natural: 300, grow: smallPrintPx / 8 },
  big: { body: svg('width="300" height="100" viewBox="0 0 300 100"', words(20, 16)), natural: 300, grow: 1 },
  plain: { body: svg('width="300" height="100" viewBox="0 0 300 100"', ""), natural: 300, grow: 1 },
  // Tiny notes under large labels: growing would inflate the labels.
  notes: { body: svg('width="300" height="100" viewBox="0 0 300 100"', words(80, 16) + words(20, 6)), natural: 300, grow: 1 },
  // A stray 4px watermark does not count as the drawing's small print.
  watermark: { body: svg('width="300" height="100" viewBox="0 0 300 100"', words(95, 16) + words(5, 4)), natural: 300, grow: 1 },
  // Typical text reaches 16px before the small print reaches 12px.
  mixed: { body: svg('width="300" height="100" viewBox="0 0 300 100"', words(60, 14) + words(30, 10) + words(10, 20)), natural: 300, grow: typicalPx / 14 },
  // Point units, as Graphviz writes: 150pt is 200px and 6-unit text is 8px.
  points: { body: svg('width="150pt" height="75pt" viewBox="0 0 150 75"', words(20, 6)), natural: 200, grow: smallPrintPx / 8 },
  // No viewBox: the drawing would not scale with its box, so it is left.
  noviewbox: { body: svg('width="300" height="100"', words(40, 8)), natural: 300, grow: 1 },
};

const chain = Array.from({ length: 14 }, (_, i) => `S${i}[Stage ${i} of ${run}]`).join(" --> ");
const diagrams = {
  wideflow: ["flowchart LR", "  " + chain],
  tiny: ["flowchart TD", `  A[${run}] --> B`],
  gantt: [
    "gantt",
    `  title Plan ${run}`,
    "  dateFormat YYYY-MM-DD",
    "  section Build",
    "  Design :a1, 2026-01-01, 10d",
    "  Code :after a1, 20d",
  ],
};
const fence = (lines) => ["```mermaid", ...lines, "```", ""];

const markdown = [
  "# SVG fit journey",
  "",
  "Prose sets the reading column. ".repeat(20),
  "",
  ...Object.keys(images).flatMap((name) => [`![${name}](${name}.svg)`, ""]),
  "A sentence with ![inline](small.svg) an image inside it.",
  "",
  '<p><img src="small.svg" alt="sized" width="100"></p>',
  "",
  "- A list item.",
  "",
  "  ![nested](wide.svg)",
  "",
  ...fence(diagrams.wideflow),
  ...fence(diagrams.tiny),
  ...fence(diagrams.gantt),
  "<!-- vellum:scale 0.5 -->",
  ...fence(["flowchart LR", `  H0[Hinted ${run}] --> H1[A second stage] --> H2[A third stage] --> H3[A fourth stage] --> H4[A fifth stage]`]),
  ...Array.from({ length: 80 }, (_, i) => [`Filler paragraph ${i}. `.repeat(12), ""]).flat(),
  ...fence(diagrams.wideflow),
].join("\n");

const topDiagrams = 4;

// Runs in the page. Returns the layout facts the checks below read.
function measurePage({ gutter }) {
  const scroll = document.querySelector(".vellum-scroll");
  const panel = scroll.getBoundingClientRect();
  const prose = document.querySelector(".vellum-article > p").getBoundingClientRect();
  const room = { left: panel.left + gutter, right: panel.left + scroll.clientWidth - gutter };
  const rect = (el) => {
    const b = el.getBoundingClientRect();
    return { left: b.left, right: b.right, width: b.width, height: b.height };
  };

  // Every drawn character's size on screen: its element's computed
  // font-size times the scale its nearest SVG ancestor is drawn at.
  function textSizes(svg) {
    const out = [];
    const walker = document.createTreeWalker(svg, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const parent = node.parentElement;
      if (!parent || parent.closest("style, script, title, desc, metadata, defs")) continue;
      if (!parent.closest("text, foreignObject")) continue;
      const chars = node.data.replace(/\s/g, "").length;
      if (!chars) continue;
      let graphic = parent;
      while (graphic && typeof graphic.getScreenCTM !== "function") graphic = graphic.parentElement;
      const m = graphic.getScreenCTM();
      const scale = Math.sqrt(Math.abs(m.a * m.d - m.b * m.c));
      out.push({ size: parseFloat(getComputedStyle(parent).fontSize) * scale, chars });
    }
    out.sort((a, b) => a.size - b.size);
    const total = out.reduce((n, s) => n + s.chars, 0);
    const at = (share) => {
      let seen = 0;
      for (const s of out) {
        seen += s.chars;
        if (seen > share * total) return s.size;
      }
      return out.length ? out[out.length - 1].size : 0;
    };
    return { small: at(0.1), typical: at(0.5) };
  }

  const images = {};
  document.querySelectorAll(".vellum-article img").forEach((img) => {
    images[img.alt] = { ...rect(img), loaded: img.complete && img.naturalWidth > 0, container: rect(img.parentElement) };
  });
  const diagrams = [...document.querySelectorAll(".vellum-article > .mermaid-svg")].map((wrap) => {
    const svg = wrap.querySelector(":scope > svg");
    if (!svg) return { pending: true, wrap: rect(wrap), hinted: wrap.hasAttribute("style") };
    return {
      wrap: rect(wrap),
      hinted: wrap.hasAttribute("style"),
      box: rect(svg),
      natural: parseFloat(svg.style.maxWidth),
      text: textSizes(svg),
    };
  });
  return {
    overflow: scroll.scrollWidth - scroll.clientWidth,
    prose: { left: prose.left, right: prose.right, width: prose.width },
    room,
    images,
    diagrams,
  };
}

(async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "vellum-svg-fit-"));
  const file = path.join(dir, "fit.md");
  fs.writeFileSync(file, markdown);
  for (const [name, image] of Object.entries(images)) fs.writeFileSync(path.join(dir, name + ".svg"), image.body);
  const server = spawn(process.env.VELLUM_BINARY || "./vellum", ["serve-view", "--addr", "127.0.0.1:" + port], { stdio: "inherit" });
  const failures = [];
  const seen = [];

  async function open(browser, width) {
    const page = await browser.newPage({ viewport: { width, height: 900 } });
    for (let attempt = 0; ; attempt++) {
      try {
        await page.goto(`http://127.0.0.1:${port}${file}`);
        break;
      } catch (e) {
        if (attempt >= 50) throw e;
        await new Promise((r) => setTimeout(r, 100));
      }
    }
    return page;
  }

  // The diagrams near the top arrive on their own; images load lazily,
  // so ask for them now.
  async function settle(page) {
    await page.waitForFunction((n) => document.querySelectorAll(".vellum-article > .mermaid-svg > svg").length >= n, topDiagrams, {
      timeout: hydrateTimeoutMs,
    });
    await page.evaluate(() => document.querySelectorAll(".vellum-article img").forEach((img) => (img.loading = "eager")));
    await page.waitForFunction(() => [...document.querySelectorAll(".vellum-article img")].every((img) => img.complete && img.naturalWidth > 0));
  }

  try {
    for (const [name, opts] of [["chromium", { channel: "chrome" }], ["firefox", {}]]) {
      const browser = await pw[name].launch({ headless: true, ...opts });
      try {
        for (const width of widths) {
          const at = `${name} ${width}`;
          const fail = (msg) => failures.push(`${at}: ${msg}`);
          const page = await open(browser, width);
          page.on("pageerror", (e) => fail(`page error ${e.message}`));
          await settle(page);
          const r = await page.evaluate(measurePage, { gutter });
          const roomWidth = r.room.right - r.room.left;
          const columnMid = (r.prose.left + r.prose.right) / 2;
          const near = (a, b) => Math.abs(a - b) <= tolerance;
          const insideRoom = (b) => b.left >= r.room.left - tolerance && b.right <= r.room.right + tolerance;

          if (r.overflow > 0) fail(`page scrolls sideways by ${r.overflow}px`);

          for (const [alt, image] of Object.entries(images)) {
            const got = r.images[alt];
            if (!got) {
              fail(`image ${alt} missing`);
              continue;
            }
            const want = Math.min(roomWidth, image.natural * image.grow);
            if (!near(got.width, want)) fail(`image ${alt} is ${got.width.toFixed(1)}px wide, want ${want.toFixed(1)}`);
            if (!insideRoom(got)) fail(`image ${alt} outside the panel gutters at ${got.left.toFixed(0)}..${got.right.toFixed(0)}`);
            if (got.width <= r.prose.width + tolerance) {
              if (!near(got.left, r.prose.left)) fail(`image ${alt} fits the column but starts at ${got.left.toFixed(1)}, column at ${r.prose.left.toFixed(1)}`);
            } else if (!near((got.left + got.right) / 2, columnMid)) {
              fail(`image ${alt} is wider than the column but not centred on it`);
            }
            if (!(got.height > 0)) fail(`image ${alt} has no height`);
          }
          // Left alone: an image in a sentence, one the author sized, and
          // one in a list (which gets its container's width, no more).
          const inline = r.images.inline;
          if (!inline || !near(inline.width, Math.min(images.small.natural, inline.container.width))) fail(`inline image changed size: ${inline && inline.width}`);
          const sized = r.images.sized;
          if (!sized || !near(sized.width, 100)) fail(`sized image is ${sized && sized.width}px wide, want the author's 100`);
          const nested = r.images.nested;
          if (!nested || nested.right > nested.container.right + tolerance || !near(nested.width, Math.min(images.wide.natural, nested.container.width))) {
            fail(`nested image is ${nested && nested.width.toFixed(1)}px in a ${nested && nested.container.width.toFixed(1)}px item`);
          }

          const plain = r.diagrams.filter((d) => !d.hinted && !d.pending);
          if (plain.length !== 3) fail(`expected 3 fitted diagrams, got ${plain.length}`);
          plain.forEach((d, i) => {
            // Text sizes at the natural size, from what is drawn now.
            const drawn = d.box.width / d.natural;
            const small = d.text.small / drawn;
            const typical = d.text.typical / drawn;
            const grow = Math.max(1, Math.min(smallPrintPx / small, typicalPx / typical));
            const want = Math.min(roomWidth, d.natural * grow);
            if (!near(d.box.width, want)) {
              fail(`diagram ${i} is ${d.box.width.toFixed(1)}px wide, want ${want.toFixed(1)} (natural ${d.natural}, text ${small.toFixed(1)}/${typical.toFixed(1)}px)`);
            }
            if (!insideRoom(d.box)) fail(`diagram ${i} outside the panel gutters at ${d.box.left.toFixed(0)}..${d.box.right.toFixed(0)}`);
            if (!near((d.box.left + d.box.right) / 2, columnMid)) fail(`diagram ${i} not centred on the column`);
            if (d.box.width > d.natural + tolerance && d.text.small > smallPrintPx + 0.3 && d.text.typical > typicalPx + 0.3) {
              fail(`diagram ${i} grew past reading size: text ${d.text.small.toFixed(1)}/${d.text.typical.toFixed(1)}px`);
            }
            seen.push(`${at} diagram ${i}: natural ${d.natural.toFixed(0)} drawn ${d.box.width.toFixed(0)} text ${d.text.small.toFixed(1)}/${d.text.typical.toFixed(1)}px`);
          });
          const [wideflow, tiny, gantt] = plain;
          if (wideflow && width >= 1400 && !(wideflow.box.width > r.prose.width + tolerance)) fail("wide diagram did not leave the column");
          if (tiny && !near(tiny.box.width, tiny.natural)) fail(`small diagram is ${tiny.box.width.toFixed(1)}px, natural ${tiny.natural}`);
          if (gantt && width >= 1400 && !(gantt.box.width > gantt.natural + tolerance)) fail("diagram with small text did not grow");

          const hinted = r.diagrams.find((d) => d.hinted);
          if (!hinted || hinted.pending) fail("hinted diagram missing");
          else {
            if (hinted.wrap.width > r.prose.width / 2 + tolerance) fail(`hinted diagram is ${hinted.wrap.width.toFixed(1)}px, over half the column`);
            if (hinted.box.width > hinted.wrap.width + tolerance) fail("hinted diagram overflows the width its author set");
          }

          // The diagram far below is the first one again, so it is cached
          // by now: on a reload its placeholder must hold the box the
          // diagram then takes.
          await page.reload();
          await settle(page);
          const reserved = await page.evaluate(() => {
            const el = [...document.querySelectorAll(".vellum-article > .mermaid-svg")].pop().querySelector(".vellum-deferred-mermaid");
            if (!el) return null;
            const b = el.getBoundingClientRect();
            el.scrollIntoView();
            return { width: b.width, height: b.height, fitted: el.getAttribute("style") || "" };
          });
          if (!reserved) fail("the far diagram was already drawn; nothing to compare its placeholder with");
          else {
            if (!reserved.fitted.includes("--vellum-svg-natural")) fail(`cached placeholder carries no sizing: ${reserved.fitted}`);
            await page.waitForFunction(() => [...document.querySelectorAll(".vellum-article > .mermaid-svg")].pop().querySelector(":scope > svg"), null, {
              timeout: hydrateTimeoutMs,
            });
            const drawn = await page.evaluate(() => {
              const b = [...document.querySelectorAll(".vellum-article > .mermaid-svg")].pop().querySelector(":scope > svg").getBoundingClientRect();
              return { width: b.width, height: b.height };
            });
            if (!near(reserved.width, drawn.width) || !near(reserved.height, drawn.height)) {
              fail(`placeholder reserved ${reserved.width.toFixed(1)}x${reserved.height.toFixed(1)}, diagram took ${drawn.width.toFixed(1)}x${drawn.height.toFixed(1)}`);
            }
          }
          await page.close();
        }
      } finally {
        await browser.close();
      }
    }
  } finally {
    server.kill();
    fs.rmSync(dir, { recursive: true, force: true });
  }
  if (process.env.VERBOSE) seen.forEach((s) => console.log(s));
  if (failures.length) {
    failures.forEach((f) => console.error("FAIL: " + f));
    process.exitCode = 1;
  } else {
    console.log(`PASS: chrome and firefox at ${widths.join(", ")}px: SVG images and diagrams fill the room up to reading size, placeholders hold their box`);
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
