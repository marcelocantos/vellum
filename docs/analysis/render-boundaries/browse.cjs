// node browse.cjs <url> <out.json> : load a viewer page in headless Chrome,
// wait for fragments, click the JS/data links and the task box, record
// every request, console message and dialog, and probe the chrome lookups.
const puppeteer = require("/opt/homebrew/Cellar/mermaid-cli/12.2.0/libexec/lib/node_modules/@mermaid-js/mermaid-cli/node_modules/puppeteer");
const fs = require("node:fs");
(async () => {
  const [url, outFile] = process.argv.slice(2);
  const browser = await puppeteer.launch({ headless: true, args: ["--no-first-run"] });
  const page = await browser.newPage();
  const log = { requests: [], console: [], dialogs: [], csp: [] };
  page.on("request", (r) => log.requests.push(r.method() + " " + r.url()));
  page.on("console", (m) => log.console.push(m.type() + ": " + m.text()));
  page.on("dialog", async (d) => { log.dialogs.push(d.message()); await d.dismiss(); });
  await page.goto(url, { waitUntil: "networkidle0", timeout: 60000 });
  await new Promise((r) => setTimeout(r, 4000));
  for (const sel of ["#jslink", "#datalink"]) {
    try { await page.click(sel); await new Promise((r) => setTimeout(r, 500)); } catch (e) { log.console.push("click " + sel + ": " + e.message); }
  }
  log.afterClicksURL = page.url();
  if (log.afterClicksURL !== url) { await page.goto(url, { waitUntil: "networkidle0" }); await new Promise((r) => setTimeout(r, 3000)); }
  log.probe = await page.evaluate(() => {
    const c = document.getElementById("vellum-consent");
    const lb = document.getElementById("vellum-lightbox-content");
    return {
      consentIsChrome: !!(c && !c.closest(".vellum-article")),
      consentText: c ? c.textContent.slice(0, 60) : null,
      lightboxIsChrome: !!(lb && !lb.closest(".vellum-article")),
      realConsentHidden: [...document.querySelectorAll("[id=vellum-consent]")].map((e) => e.hidden),
      mermaidSVG: document.querySelectorAll(".vellum-article svg").length,
      katex: document.querySelectorAll(".katex").length,
      pwned: window.__pwned || null,
    };
  });
  // Click the task box: the real consent dialog should appear.
  try {
    await page.click(".vellum-article input[type=checkbox]");
    await new Promise((r) => setTimeout(r, 800));
    log.afterTaskClick = await page.evaluate(() =>
      [...document.querySelectorAll("[id=vellum-consent]")].map((e) => ({ inArticle: !!e.closest(".vellum-article"), hidden: e.hidden, visible: e.getBoundingClientRect().height > 0 })));
  } catch (e) { log.afterTaskClick = "error " + e.message; }
  fs.writeFileSync(outFile, JSON.stringify(log, null, 2));
  await browser.close();
})().catch((e) => { console.error(e); process.exit(1); });
