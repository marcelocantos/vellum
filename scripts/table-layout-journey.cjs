// Table layout journey: in Chrome and Firefox, at several window widths,
// the viewer's page never scrolls sideways, narrow tables line up with the
// prose, wide tables are centred on the column inside the panel's
// 32px gutters, a long code line wraps inside its block, and a block
// narrower than the column (a diagram) is centred, not pushed right. Firefox counts a transformed element's untransformed box
// as scrollable overflow, so it is the browser that catches a table laid
// out past the panel's right edge.
//
// Playwright comes from editor/node_modules (`npm ci` there), with Chrome
// and Playwright's Firefox (`npx playwright install firefox`).
const path = require("node:path");
const fs = require("node:fs");
const os = require("node:os");
const { spawn } = require("node:child_process");
const pw = require(path.join(__dirname, "..", "editor", "node_modules", "playwright"));

const port = 19844;
const widths = [1886, 1660, 1400, 1100, 700];
const gutter = 32;
const tolerance = 1.5;

const longCell = "A rule long enough to wrap, ".repeat(12).trim();
const markdown = [
  "# Table layout journey",
  "",
  "Prose sets the reading column. ".repeat(20),
  "",
  "| Narrow | Table |",
  "|---|---|",
  "| a | b |",
  "",
  "| Form | Rule |",
  "|---|---|",
  ...Array.from({ length: 8 }, (_, i) => `| Constant form ${i} with several words | ${longCell} ${i} |`),
  "",
  "More prose after the tables.",
  "",
  "```mermaid",
  "graph TD",
  "  A --> B",
  "```",
  "",
  "```js",
  "const row = { " + Array.from({ length: 30 }, (_, i) => `field${i}: value${i}`).join(", ") + " };",
  "```",
  "",
].join("\n");

(async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "vellum-table-layout-"));
  const file = path.join(dir, "tables.md");
  fs.writeFileSync(file, markdown);
  const server = spawn(process.env.VELLUM_BINARY || "./vellum", ["serve-view", "--addr", "127.0.0.1:" + port], { stdio: "inherit" });
  const failures = [];
  try {
    for (const [name, opts] of [["chromium", { channel: "chrome" }], ["firefox", {}]]) {
      const browser = await pw[name].launch({ headless: true, ...opts });
      try {
        for (const width of widths) {
          const page = await browser.newPage({ viewport: { width, height: 900 } });
          page.on("pageerror", (e) => failures.push(`${name} ${width}: page error ${e.message}`));
          for (let attempt = 0; ; attempt++) {
            try {
              await page.goto(`http://127.0.0.1:${port}${file}`);
              break;
            } catch (e) {
              if (attempt >= 50) throw e;
              await new Promise((r) => setTimeout(r, 100));
            }
          }
          await page.waitForSelector(".vellum-article > table");
          const r = await page.evaluate(({ gutter, tolerance }) => {
            const scroll = document.querySelector(".vellum-scroll");
            const panel = scroll.getBoundingClientRect();
            const prose = document.querySelector(".vellum-article > p").getBoundingClientRect();
            const tables = [...document.querySelectorAll(".vellum-article > table")].map((t) => {
              const box = t.getBoundingClientRect();
              const row = t.querySelector("tr").getBoundingClientRect();
              const narrow = row.width <= prose.width + tolerance;
              const placed = narrow
                ? Math.abs(row.left - prose.left) < tolerance
                : Math.abs((box.left + box.right) / 2 - (prose.left + prose.right) / 2) < tolerance;
              const inside = box.left >= panel.left + gutter - tolerance && box.right <= panel.left + scroll.clientWidth - gutter + tolerance;
              return { narrow, placed, inside, box: [Math.round(box.left), Math.round(box.right)] };
            });
            // Blocks other than tables: inside the column, and a narrow one
            // either starts at the column's left edge or is centred on it.
            const blocks = [...document.querySelectorAll(".vellum-article > :not(table)")].map((e) => {
              const box = e.getBoundingClientRect();
              const inside = box.left >= prose.left - tolerance && box.right <= prose.right + tolerance;
              const centred = Math.abs((box.left + box.right) / 2 - (prose.left + prose.right) / 2) < tolerance;
              return { tag: e.tagName + (e.className ? "." + e.className : ""), ok: inside && (centred || Math.abs(box.left - prose.left) < tolerance), box: [Math.round(box.left), Math.round(box.right)] };
            });
            const diagram = document.querySelector(".vellum-article > .mermaid-svg");
            const code = document.querySelector(".vellum-article > pre");
            return {
              overflow: scroll.scrollWidth - scroll.clientWidth,
              tables,
              blocks,
              diagram: Boolean(diagram),
              codeOverflow: code ? code.scrollWidth - code.clientWidth : -1,
            };
          }, { gutter, tolerance });
          if (r.overflow > 0) failures.push(`${name} ${width}: page scrolls sideways by ${r.overflow}px`);
          if (r.tables.length !== 2) failures.push(`${name} ${width}: expected 2 tables, got ${r.tables.length}`);
          r.tables.forEach((t, i) => {
            if (!t.placed) failures.push(`${name} ${width}: table ${i} (${t.narrow ? "narrow" : "wide"}) misplaced at ${t.box}`);
            if (!t.inside) failures.push(`${name} ${width}: table ${i} outside the panel gutters at ${t.box}`);
          });
          if (width >= 1400 && r.tables[1] && r.tables[1].narrow) failures.push(`${name} ${width}: wide fixture table did not grow past the column`);
          r.blocks.filter((b) => !b.ok).forEach((b) => failures.push(`${name} ${width}: ${b.tag} misplaced at ${b.box}`));
          if (!r.diagram) failures.push(`${name} ${width}: fixture diagram block missing`);
          if (r.codeOverflow !== 0) failures.push(`${name} ${width}: long code line overflows its block by ${r.codeOverflow}px`);
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
  if (failures.length) {
    failures.forEach((f) => console.error("FAIL: " + f));
    process.exitCode = 1;
  } else {
    console.log(`PASS: chrome and firefox at ${widths.join(", ")}px: no sideways page scroll, tables and blocks placed, code wraps`);
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
