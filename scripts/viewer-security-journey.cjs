// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Viewer security journey: the shipped binary's view server, a real
// Chrome, real documents on disk. A document the owner did not write must
// not run script in the viewer, by its markup or by the browser's own
// reading of it; the viewer's own scripts (theme, TOC, math, diagrams,
// editor) must keep working under the Content-Security-Policy.
//
//   VELLUM_BINARY=/path/to/vellum node scripts/viewer-security-journey.cjs
//
// Needs Playwright (editor/node_modules, or NODE_PATH) and Google Chrome;
// node with katex and mmdc on PATH for the math and diagram checks. The
// server runs on a free loopback port with HOME in a temporary directory.

const { chromium } = require("playwright");
const fs = require("node:fs");
const http = require("node:http");
const net = require("node:net");
const os = require("node:os");
const path = require("node:path");
const { spawn } = require("node:child_process");

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.on("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const port = srv.address().port;
      srv.close(() => resolve(port));
    });
  });
}

async function waitFor(fn, what, ms) {
  const deadline = Date.now() + (ms || 10000);
  for (;;) {
    if (await fn()) return;
    if (Date.now() > deadline) throw new Error("timed out waiting for " + what);
    await new Promise((r) => setTimeout(r, 100));
  }
}

const failures = [];
function check(ok, msg) {
  if (!ok) failures.push(msg);
}

// Each payload, if it ever ran, would set window.__pwned (or the parent's),
// call alert, or load the beacon URL.
function payloadDoc(beacon) {
  const hit = (name) => "new Image().src='" + beacon + "/" + name + "';window.__pwned='" + name + "';alert('" + name + "')";
  const lines = [
    "# Payloads",
    "",
    "<script>" + hit("script") + "</script>",
    "",
    "text <script>" + hit("script-inline") + "</script> text",
    "",
    '<img src="missing.png" onerror="' + hit("onerror") + '">',
    "",
    '<body onload="' + hit("body-onload") + '">',
    "",
    '<details open ontoggle="' + hit("ontoggle") + '"><summary>s</summary>x</details>',
    "",
    '<div id="hover" onmouseover="' + hit("onmouseover") + '">hover me</div>',
    "",
    "[js link](javascript:" + hit("js-link") + ")",
    "",
    '<a id="rawjs" href="javascript:' + hit("raw-js-link") + '">raw js link</a>',
    "",
    "[data link](data:text/html;base64,PHNjcmlwdD5wYXJlbnQuX19wd25lZD0xPC9zY3JpcHQ+)",
    "",
    "<svg><script>" + hit("svg-script") + "</script></svg>",
    "",
    '<svg onload="' + hit("svg-onload") + '"></svg>',
    "",
    '<iframe srcdoc="<script>parent.__pwned=1</script>"></iframe>',
    "",
    '<iframe src="javascript:parent.__pwned=1"></iframe>',
    "",
    '<object data="javascript:parent.__pwned=1"></object>',
    "",
    '<embed src="javascript:parent.__pwned=1">',
    "",
    '<meta http-equiv="refresh" content="0;url=javascript:window.__pwned=1">',
    "",
    '<base href="https://evil.example/">',
    "",
    '<form action="https://evil.example/"><button id="formbtn">go</button></form>',
    "",
    '<style>.vellum-toolbar{display:none !important}</style>',
    "",
    '<div style="position:fixed;inset:0;z-index:99999;background:red">overlay</div>',
    "",
    '[title](https://example.com "\\"><script>' + hit("title") + '</script>")',
    "",
    '!["><img src=x onerror=' + hit("alt") + ">](pic.png)",
    "",
    '```"><script>' + hit("fence") + "</script>",
    "x",
    "```",
    "",
    "Footnote[^1].",
    "",
    '[^1]: <img src=x onerror="' + hit("footnote") + '">',
    "",
    "<div>",
    '<img src=x onerror="' + hit("html-block") + '">',
    "</div>",
    "",
    "| cell |",
    "|---|",
    '| <img src=x onerror="' + hit("table") + '"> |',
    "",
    "</body></html><script>" + hit("after-body") + "</script>",
    "",
    "Tail paragraph.",
    "",
  ];
  return lines.join("\n");
}

function featureDoc() {
  return [
    "# Features",
    "",
    "Inline math $e^{i\\pi} + 1 = 0$ and more.",
    "",
    "```mermaid",
    "graph TD",
    "  A --> B",
    "```",
    "",
    "## Tasks",
    "",
    "- [ ] first task",
    "- [x] second task",
    "",
    "## Picture",
    "",
    "![pic](pic.png)",
    "",
    "![outside pic](../outside.png)",
    "",
    "[linked page](page.html)",
    "",
    "[outside doc](../outside.md)",
    "",
  ].join("\n");
}

// A 1x1 PNG.
const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64"
);

(async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "vellum-security-journey-"));
  const root = fs.realpathSync(dir);
  const docs = path.join(root, "docs");
  fs.mkdirSync(docs);
  const beaconHits = [];
  const beacon = http.createServer((req, res) => {
    beaconHits.push(req.url);
    res.end();
  });
  await new Promise((r) => beacon.listen(0, "127.0.0.1", r));
  const beaconURL = "http://127.0.0.1:" + beacon.address().port + "/hit";

  const payloads = path.join(docs, "payloads.md");
  const features = path.join(docs, "features.md");
  fs.writeFileSync(payloads, payloadDoc(beaconURL));
  fs.writeFileSync(features, featureDoc());
  fs.writeFileSync(path.join(docs, "pic.png"), PNG);
  const tasks = path.join(docs, "tasks.md");
  const tasksSrc = "# Tasks\n\n```\n- [ ] example in code\n```\n\n> - [ ] quoted task\n\n- [ ] real task\n\n- [ ] loose one\n\n- [ ] loose two\n";
  fs.writeFileSync(tasks, tasksSrc);
  fs.writeFileSync(path.join(root, "outside.png"), PNG);
  fs.writeFileSync(path.join(root, "outside.md"), "# Outside\n\nSECRET-OUTSIDE\n");
  fs.writeFileSync(path.join(docs, "page.html"), "<!doctype html><title>linked</title><p id=linked>linked page</p><script>window.__ran=1;try{parent.__pwned='linked-html'}catch(e){}</script>");

  const port = await freePort();
  const origin = "http://127.0.0.1:" + port;
  const home = path.join(root, "home");
  fs.mkdirSync(home);
  const server = spawn(process.env.VELLUM_BINARY || "./vellum", ["serve-view", "--addr", "127.0.0.1:" + port], {
    stdio: "inherit",
    env: Object.assign({}, process.env, {
      HOME: home,
      XDG_CONFIG_HOME: path.join(home, ".config"),
      VELLUM_VIEW_ADDR: "",
      // mmdc finds its Chrome under the real home, as the LaunchAgent does.
      PUPPETEER_CACHE_DIR: process.env.PUPPETEER_CACHE_DIR || path.join(os.homedir(), ".cache", "puppeteer"),
    }),
  });
  const browser = await chromium.launch({ headless: true, channel: "chrome" });
  try {
    await waitFor(async () => {
      try {
        return (await fetch(origin + "/healthz")).ok;
      } catch (e) {
        return false;
      }
    }, "server health");

    const context = await browser.newContext({ viewport: { width: 1200, height: 900 } });
    const page = await context.newPage();
    page.setDefaultTimeout(15000);
    const dialogs = [];
    const errors = [];
    const cspViolations = [];
    page.on("dialog", (d) => {
      dialogs.push(d.message());
      d.dismiss().catch(() => {});
    });
    page.on("pageerror", (e) => errors.push(String(e)));
    page.on("console", (m) => {
      if (/Content Security Policy|Refused to/i.test(m.text())) cspViolations.push(m.text());
    });

    // 1. A document full of payloads: nothing runs, the viewer still does.
    await page.goto(origin + payloads);
    await page.waitForSelector(".vellum-article h1");
    await page.waitForFunction(() => document.querySelectorAll("#vellum-toc-nav a").length > 0);
    // Give image errors, toggles and refreshes time to fire, then provoke
    // the handlers a reader would trigger.
    await page.evaluate(() => {
      const art = document.querySelector(".vellum-article");
      for (const el of art.querySelectorAll("*")) {
        // Not click: a click on a figure opens the viewer's lightbox.
        for (const type of ["mouseover", "mouseenter", "focus", "toggle"]) {
          try {
            el.dispatchEvent(new Event(type, { bubbles: true }));
          } catch (e) {}
        }
      }
    });
    await page.waitForTimeout(800);
    const state = await page.evaluate(() => ({
      pwned: window.__pwned || null,
      jsLinks: Array.from(document.querySelectorAll("a[href]")).filter((a) => /^\s*(javascript|data|vbscript):/i.test(a.getAttribute("href"))).length,
      toolbarVisible: getComputedStyle(document.querySelector(".vellum-toolbar")).display !== "none",
      overlay: Array.from(document.querySelectorAll(".vellum-article div")).some((d) => getComputedStyle(d).position === "fixed"),
      iframes: document.querySelectorAll(".vellum-article iframe, .vellum-article object, .vellum-article embed").length,
      base: document.baseURI,
      tail: document.body.textContent.includes("Tail paragraph."),
      chromeScriptRan: Boolean(document.documentElement.dataset.vellumTheme),
      url: location.href,
    }));
    check(state.pwned === null, "a payload ran: " + state.pwned);
    check(state.jsLinks === 0, state.jsLinks + " script-URL links survived");
    check(state.toolbarVisible, "a document style hid the viewer toolbar");
    check(!state.overlay, "a document element is position:fixed over the viewer");
    check(state.iframes === 0, "embedded frames survived");
    check(state.base.startsWith(origin + "/"), "document changed the base URL to " + state.base);
    check(state.tail, "content after the payloads was lost");
    check(state.chromeScriptRan, "the viewer's theme script did not run");
    check(state.url === origin + payloads, "page navigated away to " + state.url);
    // Clicking what is left of the script links does nothing.
    await page.evaluate(() => document.getElementById("rawjs").click());
    await page.waitForTimeout(200);
    check((await page.evaluate(() => window.__pwned || null)) === null, "raw javascript: link ran on click");

    // 2. The second layer: the policy alone refuses injected script.
    const before = cspViolations.length;
    await page.evaluate(() => {
      const s = document.createElement("script");
      s.textContent = "window.__cspBypass = 'inline'";
      document.body.appendChild(s);
      const d = document.createElement("div");
      d.innerHTML = '<img src="nope.png" onerror="window.__cspBypass = \'handler\'">';
      document.body.appendChild(d);
      const ext = document.createElement("script");
      ext.src = "pic.png.js";
      document.body.appendChild(ext);
      const a = document.createElement("a");
      a.href = "javascript:window.__cspBypass='url'";
      document.body.appendChild(a);
      a.click();
    });
    await page.waitForTimeout(500);
    check((await page.evaluate(() => window.__cspBypass || null)) === null, "CSP let injected script run");
    check(cspViolations.length > before, "CSP reported no refusal for injected script (is the policy sent?)");

    // 3. The viewer's own features under the policy.
    const ownViolations = cspViolations.length;
    await page.goto(origin + features);
    await page.waitForSelector(".vellum-article h1");
    await page.waitForFunction(() => document.querySelector(".vellum-article .katex"), null, { timeout: 30000 });
    await page.waitForFunction(() => document.querySelector(".vellum-article .mermaid-svg svg"), null, { timeout: 60000 });
    const theme0 = await page.evaluate(() => document.documentElement.dataset.vellumTheme);
    await page.click('[data-vellum="theme"]');
    const theme1 = await page.evaluate(() => document.documentElement.dataset.vellumTheme);
    check(theme0 !== theme1, "theme button did nothing");
    await page.waitForFunction(() => {
      const img = document.querySelector('.vellum-article img[alt="pic"]');
      return img && img.complete && img.naturalWidth === 1;
    });
    // Editor: opens, edits, saves to disk.
    await page.click('[data-vellum="edit"]');
    await page.waitForSelector(".cm-content");
    await page.click(".cm-content");
    await page.keyboard.press(process.platform === "darwin" ? "Meta+End" : "Control+End");
    await page.keyboard.type("\nEdited in the journey.\n");
    await page.keyboard.press(process.platform === "darwin" ? "Meta+s" : "Control+s");
    await waitFor(() => fs.readFileSync(features, "utf8").includes("Edited in the journey."), "editor save");
    await page.click('[data-vellum="ed-close"]');
    check(cspViolations.length === ownViolations, "the viewer's own features hit the CSP: " + cspViolations.slice(ownViolations).join(" | "));

    // 4. Only docs/ is served: the browser's own requests for a file
    // outside it (an image, a followed link) are refused; the user can
    // still open that file directly, which serves its folder.
    await page.goto(origin + features);
    await page.waitForSelector(".vellum-article h1");
    await page.waitForTimeout(300);
    const outsidePic = await page.evaluate(() => {
      const img = document.querySelector('.vellum-article img[alt="outside pic"]');
      return img ? { complete: img.complete, w: img.naturalWidth } : null;
    });
    check(outsidePic && outsidePic.w === 0, "an image outside the served folder loaded: " + JSON.stringify(outsidePic));
    const [outResp] = await Promise.all([page.waitForNavigation(), page.click('a[href$="outside.md"]')]);
    check(outResp && outResp.status() === 403, "a link out of the served folder was served: " + (outResp && outResp.status()));
    check(!(await page.content()).includes("SECRET-OUTSIDE"), "outside document content shown after a link");
    const direct = await page.goto(origin + path.join(root, "outside.md"));
    check(direct.status() === 200, "opening the outside document directly: " + direct.status());

    // 5. A linked HTML page opens sandboxed: no script, opaque origin.
    await page.goto(origin + features);
    await page.waitForSelector(".vellum-article h1");
    await Promise.all([page.waitForNavigation(), page.click('a[href$="page.html"]')]);
    await page.waitForSelector("#linked");
    const linked = await page.evaluate(() => ({ ran: window.__ran || null, origin: window.origin }));
    check(linked.ran === null, "a linked HTML file ran its script");
    check(linked.origin === "null", "a linked HTML file runs in the viewer's origin: " + linked.origin);

    // 6. Task checkboxes edit exactly the task clicked, and a click on a
    // page whose file has changed underneath it is refused. The watch
    // socket is held off so the page cannot reload before the click.
    const tasksPage = await context.newPage();
    tasksPage.setDefaultTimeout(15000);
    tasksPage.on("pageerror", (e) => errors.push(String(e)));
    await tasksPage.routeWebSocket(/\/_vellum\/watch/, () => {});
    await tasksPage.goto(origin + tasks);
    await tasksPage.waitForSelector(".vellum-article li[data-task-index]");
    const boxes = await tasksPage.evaluate(() =>
      Array.from(document.querySelectorAll(".vellum-article input[type=checkbox]")).map((b) => ({
        text: b.closest("li").textContent.trim(),
        enabled: !b.disabled,
        line: b.closest("li").getAttribute("data-task-line"),
      }))
    );
    check(
      JSON.stringify(boxes.map((b) => b.text)) === JSON.stringify(["quoted task", "real task", "loose one", "loose two"]),
      "page checkboxes: " + JSON.stringify(boxes)
    );
    check(boxes.every((b) => b.enabled), "a task checkbox is disabled: " + JSON.stringify(boxes));
    const box = (text) => tasksPage.locator(".vellum-article li", { hasText: text }).locator("input[type=checkbox]");
    await box("quoted task").click();
    await tasksPage.click('[data-vellum="consent-session"]');
    await waitFor(() => fs.readFileSync(tasks, "utf8").includes("> - [x] quoted task"), "quoted task ticked on disk");
    let onDisk = fs.readFileSync(tasks, "utf8");
    check(onDisk === tasksSrc.replace("> - [ ] quoted", "> - [x] quoted"), "the toggle changed more than the quoted task:\n" + onDisk);
    // Two quick clicks: both land, one after the other.
    await box("loose one").click();
    await box("loose two").click();
    await waitFor(() => /- \[x\] loose one[\s\S]*- \[x\] loose two/.test(fs.readFileSync(tasks, "utf8")), "both loose tasks ticked");
    // Someone edits the file; this page still shows the old version.
    const edited = "- [ ] inserted above\n\n" + fs.readFileSync(tasks, "utf8");
    fs.writeFileSync(tasks, edited);
    await box("real task").click();
    await tasksPage.waitForTimeout(800);
    check(fs.readFileSync(tasks, "utf8") === edited, "a click on a stale page wrote the file:\n" + fs.readFileSync(tasks, "utf8"));
    check(!(await box("real task").isChecked()), "the refused checkbox stayed ticked");
    const status = await tasksPage.evaluate(() => document.getElementById("vellum-status").textContent);
    check(/changed on disk/.test(status), "no message for the refused toggle: " + JSON.stringify(status));
    await tasksPage.close();

    check(dialogs.length === 0, "dialogs opened: " + dialogs.join(", "));
    check(beaconHits.length === 0, "beacon hit: " + beaconHits.join(", "));
    check(errors.length === 0, "page errors: " + errors.join(" | "));
  } finally {
    await browser.close();
    server.kill();
    beacon.close();
    fs.rmSync(dir, { recursive: true, force: true });
  }
  if (failures.length) {
    for (const f of failures) console.error("FAIL: " + f);
    process.exitCode = 1;
    return;
  }
  console.log("PASS: payloads inert, CSP refuses injected script, viewer features work, only opened folders served, linked HTML sandboxed, task toggles exact and refused when stale");
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
