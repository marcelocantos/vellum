const puppeteer = require("/opt/homebrew/Cellar/mermaid-cli/12.2.0/libexec/lib/node_modules/@mermaid-js/mermaid-cli/node_modules/puppeteer");
(async () => {
  const browser = await puppeteer.launch({ headless: true });
  for (const url of process.argv.slice(2)) {
    const page = await browser.newPage();
    const errs = [];
    page.on("console", (m) => { if (m.type() === "error") errs.push(m.text()); });
    await page.goto(url, { waitUntil: "networkidle0", timeout: 60000 });
    await new Promise((r) => setTimeout(r, 4000));
    const r = await page.evaluate(() => ({
      visibleAfter: document.body.innerText.includes("VISIBLE-AFTER"),
      tocLinks: document.querySelectorAll("#vellum-toc-nav a").length,
      diagrams: document.querySelectorAll(".vellum-article svg").length,
      chromeScripts: document.querySelectorAll("script[nonce]").length,
      editorDialog: !!document.querySelector(".vellum-editor"),
    }));
    console.log(url.split("/").pop(), JSON.stringify(r), errs.slice(0, 2).join(" | "));
    await page.close();
  }
  await browser.close();
})();
