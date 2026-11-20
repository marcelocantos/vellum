// Confine mmdc's Chromium. mermaid-cli loads its page from a file: URL and
// only then turns on request interception, which does not see a file: image
// loaded after that. This hook moves interception to before the navigation
// and answers every other request with an empty image, except that page,
// mermaid-cli's own asset origin, and data: or blob: URLs.
import { writeSync } from "node:fs";
import * as nodeModule from "node:module";

// vellum passes a per-run token and takes mmdc's output only when the
// token comes back on stderr. It is written at exit, and only when a
// browser was launched through both rewrites below. An mmdc this hook
// does not recognise (another install layout, a release that moves or
// bundles these files) launches unconfined and never reports the token,
// so vellum refuses its output instead of trusting it.
const confinedToken = process.env.VELLUM_MMDC_CONFINED_TOKEN;
delete process.env.VELLUM_MMDC_CONFINED_TOKEN;
process.on("exit", () => {
  if (confinedToken && globalThis.__vellumMermaidLaunchedConfined) {
    writeSync(2, `\n${confinedToken}\n`);
  }
});

const continueLine =
  "request.continue(undefined, DEFAULT_INTERCEPT_RESOLUTION_PRIORITY);";
// A denied request is answered with a 1×1 transparent PNG instead of
// abort(). Mermaid's image shapes await img.decode(), and an abort rejects
// that and drops the whole diagram. Answering with no pixels lets the
// diagram render, which is what an unconfined file: image already did,
// and Chromium never opens the named file or the network.
const confinedContinue = `const vellumURL = request.url();
      if (
        vellumURL.startsWith("data:") ||
        vellumURL.startsWith("blob:") ||
        vellumURL === globalThis.__vellumMermaidPage
      ) {
        return request.continue(undefined, DEFAULT_INTERCEPT_RESOLUTION_PRIORITY);
      }
      return request.respond(
        {
          status: 200,
          contentType: "image/png",
          body: Buffer.from(
            "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=",
            "base64",
          ),
        },
        DEFAULT_INTERCEPT_RESOLUTION_PRIORITY,
      );`;

const beforeGoto = `    const mermaidHTMLPath = path.join(__dirname, "..", "dist", "index.html");
    await page.goto(url.pathToFileURL(mermaidHTMLPath).href);
    await page.$eval(
      "body",
      (body, backgroundColor) => {
        body.style.background = backgroundColor;
      },
      backgroundColor,
    );

    const interceptor = new Interceptor();`;

const afterGoto = `    const mermaidHTMLPath = path.join(__dirname, "..", "dist", "index.html");
    globalThis.__vellumMermaidPage = url.pathToFileURL(mermaidHTMLPath).href;

    const interceptor = new Interceptor();`;

const beforeListen = `    page.on("request", interceptor.interceptRequestHandler);
    await page.setRequestInterception(true);

    const cssImports = [`;

const afterListen = `    page.on("request", interceptor.interceptRequestHandler);
    await page.setRequestInterception(true);

    await page.goto(url.pathToFileURL(mermaidHTMLPath).href);
    await page.$eval(
      "body",
      (body, backgroundColor) => {
        body.style.background = backgroundColor;
      },
      backgroundColor,
    );

    const cssImports = [`;

const launchLine = "puppeteer.launch(puppeteerConfig)";
const confinedLaunch =
  '(globalThis.__vellumMermaidConfined ? ((globalThis.__vellumMermaidLaunchedConfined = true), puppeteer.launch(puppeteerConfig)) : Promise.reject(new Error("vellum: mermaid renderer is not confined")))';

function sourceText(source) {
  if (typeof source === "string") return source;
  if (source instanceof Uint8Array) return Buffer.from(source).toString("utf8");
  return null;
}

function load(url, context, nextLoad) {
  const loaded = nextLoad(url, context);
  const finish = (result) => {
    const text = sourceText(result.source);
    if (text == null) return result;
    let source = text;
    if (url.includes("/@mermaid-js/mermaid-cli/src/puppeteerIntercept.js")) {
      const n = source.split(continueLine).length - 1;
      if (n !== 1) {
        throw new Error(`vellum: mmdc interceptor continue() count is ${n}, want 1`);
      }
      source =
        "globalThis.__vellumMermaidConfined = true;\n" +
        source.replace(continueLine, confinedContinue);
    }
    if (url.includes("/@mermaid-js/mermaid-cli/src/index.js")) {
      if (!source.includes(beforeGoto) || !source.includes(beforeListen)) {
        throw new Error("vellum: mmdc render page is not the shape this confine hook knows");
      }
      if (!source.includes(launchLine)) {
        throw new Error("vellum: mmdc has no puppeteer.launch to confine");
      }
      source = source.replace(beforeGoto, afterGoto).replace(beforeListen, afterListen);
      source = source.split(launchLine).join(confinedLaunch);
    }
    return { ...result, source };
  };
  if (loaded && typeof loaded.then === "function") return loaded.then(finish);
  return finish(loaded);
}

if (typeof nodeModule.registerHooks !== "function") {
  throw new Error("vellum: node module.registerHooks is required to confine mmdc");
}
nodeModule.registerHooks({ load });
