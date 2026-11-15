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

function fixture() {
  const filler = Array.from({ length: 40 }, (_, i) => "Paragraph " + i + " of filler prose.\n").join("\n");
  return (
    "# Editor journey\n\nIntro.\n\n## Section A\n\n" + filler +
    "\n## Section B\n\n- first item\n- second item\n\n```go\nfunc main() {\n\tprintln(\"hi\")\n}\n```\n\n" +
    filler + "\n## Section C\n\nEnd.\n"
  );
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

    // Scroll the view to Section B; the editor should open there.
    await page.evaluate(() => {
      const pane = document.querySelector(".vellum-scroll");
      const h = [...document.querySelectorAll(".vellum-article h2")].find((e) => e.textContent === "Section B");
      pane.scrollTop += h.getBoundingClientRect().top - pane.getBoundingClientRect().top;
    });
    await page.keyboard.press("e");
    await page.waitForSelector(".vellum-editor:not([hidden]) .cm-editor");
    await waitFor(() => page.evaluate(() => document.activeElement && document.activeElement.classList.contains("cm-content")), "editor focus");
    const active = await page.locator(".cm-activeLine").innerText();
    check(active === "## Section B", "editor opened at " + JSON.stringify(active) + ", want the view's section");

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

    await page.locator('[data-vellum="ed-close"]').click();
    await page.waitForFunction(() => !window.__journeyMarker, null, { timeout: 5000 }).catch(() => {});
    await page.waitForSelector(".vellum-article h2");
    check(await page.evaluate(() => !window.__journeyMarker), "closing after a save did not refresh the view");
    check((await page.locator(".vellum-article").innerText()).includes("third item"), "view does not show the saved text");

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
    console.log("PASS: open at section, highlighting, list continuation, save, follow disk, conflict overwrite, refresh on close, dark, layout, discard, phone, offline");
  } finally {
    await browser.close();
    server.kill();
    fs.rmSync(dir, { recursive: true, force: true });
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
