// Viewer chrome journey: in Chrome and Firefox, against a real serve-view,
// the top bar names the file and then the document's title, the contents
// panel marks the section being read, the panel toggles (and on a narrow
// window opens over the page and closes again), the theme button shows one
// icon per mode, the mark opens the homepage, a document without headings
// has no panel, nothing overflows sideways down to phone width, and in the
// dark theme Mermaid diagrams are recoloured, each under its own SVG id.
//
// Playwright comes from editor/node_modules (`npm ci` there), with Chrome
// and Playwright's Firefox (`npx playwright install firefox`). The diagram
// checks need mmdc.
const path = require("node:path");
const fs = require("node:fs");
const os = require("node:os");
const { spawn } = require("node:child_process");
const pw = require(path.join(__dirname, "..", "editor", "node_modules", "playwright"));

const port = 19845;
const origin = "http://127.0.0.1:" + port;
const title = "Chrome journey";
const filler = "Filler prose so each section is taller than a line or two. ".repeat(12);
const sections = ["Alpha", "Beta", "Gamma", "Delta", "Epsilon"];
const markdown = [
  "# " + title,
  "",
  filler,
  "",
  ...sections.flatMap((s) => [
    "## " + s,
    "",
    filler,
    "",
    "### " + s + " one",
    "",
    filler,
    "",
    "### " + s + " two",
    "",
    filler,
    "",
  ]),
  "## Diagrams",
  "",
  "```mermaid",
  "graph TD",
  "  A --> B",
  "```",
  "",
  "```mermaid",
  "pie title Share",
  '  "A" : 3',
  '  "B" : 1',
  "```",
  "",
  "## Last",
  "",
  "Short.",
  "",
].join("\n");

const failures = [];
function check(ok, message) {
  if (!ok) failures.push(message);
}

(async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "vellum-chrome-journey-"));
  const doc = path.join(dir, "journey.md");
  const plain = path.join(dir, "plain.md");
  fs.writeFileSync(doc, markdown);
  fs.writeFileSync(plain, "Just a paragraph.\n\nAnd another.\n");
  const server = spawn(process.env.VELLUM_BINARY || "./vellum", ["serve-view", "--addr", "127.0.0.1:" + port], { stdio: "inherit" });
  try {
    for (const [name, opts] of [["chromium", { channel: "chrome" }], ["firefox", {}]]) {
      const browser = await pw[name].launch({ headless: true, ...opts });
      try {
        const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
        page.on("pageerror", (e) => failures.push(`${name}: page error ${e.message}`));
        for (let attempt = 0; ; attempt++) {
          try {
            await page.goto(origin + doc);
            break;
          } catch (e) {
            if (attempt >= 50) throw e;
            await new Promise((r) => setTimeout(r, 100));
          }
        }
        await page.waitForSelector(".vellum-toc-nav a");
        // The contents panel fills itself a level at a time.
        await page.waitForTimeout(800);
        const pane = page.locator("#vellum-scroll");
        const titled = () => page.locator(".vellum-doc").evaluate((e) => e.classList.contains("is-titled"));
        const scrollTo = (heading) =>
          page.evaluate((text) => {
            const h = [...document.querySelectorAll(".vellum-article :is(h2, h3)")].find((e) => e.textContent.trim() === text);
            const pane = document.getElementById("vellum-scroll");
            pane.scrollTop += h.getBoundingClientRect().top - pane.getBoundingClientRect().top - 4;
          }, heading);

        // The bar: file name, then the title once the h1 is gone; a click
        // on it returns to the top.
        check((await page.locator(".vellum-docname").innerText()) === "journey.md", `${name}: bar does not name the file`);
        check(!(await titled()), `${name}: bar shows the title before any scroll`);
        await scrollTo("Beta");
        await page.waitForTimeout(200);
        check(await titled(), `${name}: bar does not show the title after scrolling`);
        check((await page.locator(".vellum-doctitle").innerText()) === title, `${name}: bar title text is wrong`);
        await page.click(".vellum-doc");
        await page.waitForTimeout(200);
        check((await pane.evaluate((e) => e.scrollTop)) === 0, `${name}: a click on the bar label does not return to the top`);
        check(!(await titled()), `${name}: bar still shows the title at the top`);

        // The contents panel marks the section at the top of the pane and
        // keeps its link in view.
        for (const heading of ["Alpha one", "Gamma", "Delta two", "Epsilon"]) {
          await scrollTo(heading);
          await page.waitForTimeout(200);
          const active = await page.evaluate(() => {
            const links = [...document.querySelectorAll(".vellum-toc-active")];
            const toc = document.getElementById("vellum-toc").getBoundingClientRect();
            return links.map((a) => {
              const r = a.getBoundingClientRect();
              return { text: a.textContent, shown: r.height > 0 && r.top >= toc.top && r.bottom <= toc.bottom };
            });
          });
          check(active.length === 1, `${name}: at "${heading}" ${active.length} links are marked`);
          // A link inside a collapsed section is stood in for by its section's.
          const want = [heading, heading.split(" ")[0]];
          check(active[0] && want.includes(active[0].text), `${name}: at "${heading}" the panel marks ${JSON.stringify(active)}`);
          check(active[0] && active[0].shown, `${name}: the marked link for "${heading}" is out of view`);
        }
        // A click marks its section and brings the heading just under the bar.
        await page.click('.vellum-toc-nav a:text-is("Beta")');
        await page.waitForTimeout(900);
        check((await page.locator(".vellum-toc-active").innerText()) === "Beta", `${name}: a clicked link is not the marked one`);
        const under = await page.evaluate(() => {
          const h = [...document.querySelectorAll(".vellum-article h2")].find((e) => e.textContent.trim() === "Beta");
          return h.getBoundingClientRect().top - document.getElementById("vellum-scroll").getBoundingClientRect().top;
        });
        check(under >= 0 && under < 40, `${name}: the jumped-to heading is ${under}px below the bar`);
        // The end of the document marks its last section, which is too
        // short to reach the top of the pane.
        await pane.evaluate((e) => (e.scrollTop = e.scrollHeight));
        await page.waitForTimeout(900);
        check((await page.locator(".vellum-toc-active").innerText()) === "Last", `${name}: the end of the document does not mark the last section`);

        // The panel's toggle.
        const toggle = page.locator('[data-vellum="toc-toggle"]');
        await toggle.click();
        check(!(await page.locator("#vellum-toc").isVisible()), `${name}: the toggle does not hide the panel`);
        check((await toggle.getAttribute("title")) === "Show contents", `${name}: the toggle's label did not change`);
        await toggle.click();
        check(await page.locator("#vellum-toc").isVisible(), `${name}: the toggle does not show the panel`);

        // The theme button shows exactly one icon in each of its modes.
        const modes = new Set();
        for (let i = 0; i < 3; i++) {
          await page.click('[data-vellum="theme"]');
          const state = await page.evaluate(() => ({
            mode: document.documentElement.dataset.vellumTheme,
            shown: [...document.querySelectorAll(".vellum-theme-btn svg")].filter((s) => getComputedStyle(s).display !== "none").length,
          }));
          modes.add(state.mode);
          check(state.shown === 1, `${name}: theme ${state.mode} shows ${state.shown} icons`);
        }
        check(modes.size === 3, `${name}: the theme button visits ${[...modes]}`);

        // Dark theme: diagrams lose their white ground and are recoloured;
        // each has its own SVG id, so one's styles do not reach another.
        await page.evaluate(() => {
          document.documentElement.dataset.vellumTheme = "dark";
          [...document.querySelectorAll(".vellum-article h2")].find((e) => e.textContent.trim() === "Diagrams").scrollIntoView();
        });
        await page.waitForFunction(() => document.querySelectorAll(".vellum-article .mermaid-svg svg").length === 2, null, { timeout: 60000 });
        const diagrams = (theme) =>
          page.evaluate((theme) => {
            document.documentElement.dataset.vellumTheme = theme;
            return [...document.querySelectorAll(".vellum-article .mermaid-svg > svg")].map((s) => {
              const cs = getComputedStyle(s);
              return { id: s.id, filter: cs.filter, ground: cs.backgroundColor };
            });
          }, theme);
        const dark = await diagrams("dark");
        check(new Set(dark.map((d) => d.id)).size === 2 && dark.every((d) => /^mmd-[0-9a-f]{12}$/.test(d.id)), `${name}: diagram ids are ${dark.map((d) => d.id)}`);
        check(dark.every((d) => d.filter.includes("invert")), `${name}: dark diagrams are not recoloured: ${dark.map((d) => d.filter)}`);
        check(dark.every((d) => d.ground === "rgba(0, 0, 0, 0)"), `${name}: dark diagrams keep a ground: ${dark.map((d) => d.ground)}`);
        const light = await diagrams("light");
        check(light.every((d) => d.filter === "none"), `${name}: light diagrams are filtered: ${light.map((d) => d.filter)}`);

        // The message after an action goes away when clicked.
        await page.evaluate(() => {
          const s = document.getElementById("vellum-status");
          s.textContent = "Something failed";
          s.hidden = false;
        });
        await page.click("#vellum-status");
        check(!(await page.locator("#vellum-status").isVisible()), `${name}: the status message does not dismiss on click`);

        // The mark opens the homepage, which lists the document.
        await page.click(".vellum-home");
        await page.waitForSelector("#recent");
        check(new URL(page.url()).pathname === "/", `${name}: the mark opened ${page.url()}`);
        await page.click(`#recent a:has-text("${title}")`);
        await page.waitForSelector(".vellum-article h1");
        check(decodeURI(new URL(page.url()).pathname).endsWith("journey.md"), `${name}: the homepage row opened ${page.url()}`);

        // A narrow window: the panel opens over the page, and a click
        // outside, Escape, or a link closes it. Nothing overflows sideways.
        const overflow = () =>
          page.evaluate(() => {
            const bar = document.querySelector(".vellum-toolbar");
            const pane = document.getElementById("vellum-scroll");
            return { bar: bar.scrollWidth - bar.clientWidth, page: document.documentElement.scrollWidth - innerWidth, pane: pane.scrollWidth - pane.clientWidth };
          });
        await page.setViewportSize({ width: 600, height: 800 });
        await page.waitForTimeout(200);
        const tocShown = () => page.locator("#vellum-toc").isVisible();
        check(!(await tocShown()), `${name}: the panel is open on a narrow window`);
        await toggle.click();
        check(await tocShown(), `${name}: narrow toggle does not open the panel`);
        await page.mouse.click(560, 400);
        check(!(await tocShown()), `${name}: a click outside does not close the panel`);
        await toggle.click();
        await page.keyboard.press("Escape");
        check(!(await tocShown()), `${name}: Escape does not close the panel`);
        await toggle.click();
        await page.click('.vellum-toc-nav a:text-is("Gamma")');
        await page.waitForTimeout(300);
        check(!(await tocShown()), `${name}: a link does not close the panel`);
        for (const width of [600, 360]) {
          await page.setViewportSize({ width, height: 800 });
          await page.waitForTimeout(200);
          const o = await overflow();
          check(o.bar <= 0 && o.page <= 0 && o.pane <= 0, `${name}: sideways overflow at ${width}px: ${JSON.stringify(o)}`);
        }

        // A document without headings has no panel and no toggle.
        await page.setViewportSize({ width: 1440, height: 900 });
        await page.goto(origin + plain);
        await page.waitForSelector(".vellum-article p");
        await page.waitForTimeout(300);
        check(!(await page.locator("#vellum-toc").isVisible()), `${name}: a document without headings shows the panel`);
        check(!(await page.locator('[data-vellum="toc-toggle"]').isVisible()), `${name}: a document without headings shows the toggle`);
        check(!(await page.locator(".vellum-doc").evaluate((e) => e.classList.contains("is-titled"))), `${name}: a document without a title shows one`);
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
    console.log("PASS: chrome and firefox: bar, contents panel, themes, dark diagrams, homepage, narrow window");
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
