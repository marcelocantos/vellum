// T3 Code journey: in Chrome and Firefox, a Markdown document is read,
// followed, reloaded, ticked and edited inside a sandboxed frame that
// reaches a real serve-view only through a gateway applying the rules of
// T3 Code's plugin gateway (apps/server/src/plugins/PluginGatewayHttp.ts
// in the T3 Code plugin build, at 64d6716):
//
//   - a capability prefix, /api/plugin-panels/<token>/, over the plugin's
//     whole origin;
//   - a path is refused (400) if it has a raw space, backslash, ? or #, or
//     decodes to something with a space, a percent sign or a dot segment;
//   - only Accept and Content-Type reach the plugin, only Content-Type
//     comes back, with the gateway's own Content-Security-Policy and CORS
//     headers (allowed request headers: content-type, accept);
//   - requests are limited to 1 MiB and responses to 8 MiB; an upstream
//     redirect is a 502;
//   - a WebSocket is relayed with no Origin.
//
// The gateway here is a stand-in written from that file, not T3 itself:
// what T3's own clients do with the card (dialog, sheet, hover) is not
// covered. The plugin socket is spoken to directly, as T3's server does.
//
// Playwright comes from editor/node_modules (`npm ci` there), with Chrome
// and Playwright's Firefox. The diagram and math checks need mmdc and
// katex.
const path = require("node:path");
const fs = require("node:fs");
const os = require("node:os");
const http = require("node:http");
const net = require("node:net");
const zlib = require("node:zlib");
const { spawn } = require("node:child_process");
const pw = require(path.join(__dirname, "..", "editor", "node_modules", "playwright"));

const viewPort = 19855;
const t3Port = 19856;
const gatewayPort = 19857;
const upstream = "http://127.0.0.1:" + t3Port;
const gateway = "http://127.0.0.1:" + gatewayPort;
const PREFIX = "/api/plugin-panels/";
const token = "0f8fad5b-d9cb-469f-a165-70867728950e";
const root = gateway + PREFIX + token + "/";
const REQUEST_LIMIT = 1024 * 1024;
const RESPONSE_LIMIT = 8 * 1024 * 1024;
const SURFACE_ID = "link:markdown";

const failures = [];
function check(ok, message) {
  if (!ok) failures.push(message);
}

// pluginRelativePath, as in PluginGatewaySessions.ts.
function pluginRelativePath(p) {
  if (!p.startsWith("/") || p.startsWith("//") || /[\\\u0000- ?#]/.test(p)) return false;
  try {
    const decoded = decodeURIComponent(p);
    return !/[\\%\u0000- ]/.test(decoded) && !decoded.split("/").some((part) => part === "." || part === "..");
  } catch (e) {
    return false;
  }
}

const gatewayLog = [];
function gatewayHeaders() {
  const wsRoot = root.replace(/^http/, "ws");
  return {
    "cache-control": "no-store",
    "referrer-policy": "no-referrer",
    "x-content-type-options": "nosniff",
    "access-control-allow-origin": "null",
    "access-control-allow-methods": "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS",
    "access-control-allow-headers": "content-type, accept",
    "content-security-policy": `sandbox allow-scripts allow-forms; default-src 'none'; script-src 'unsafe-inline' 'wasm-unsafe-eval' ${root}; style-src 'unsafe-inline' ${root}; img-src data: blob: ${root}; font-src ${root}; connect-src ${root} ${wsRoot}; base-uri ${root}; form-action ${root}; frame-src 'none'; worker-src 'none'; object-src 'none'`,
  };
}

// The page T3 would be: it frames the card in the sandbox T3 uses and
// offers the bridge.
function hostPage(cardPath) {
  return `<!doctype html><meta charset="utf-8"><title>host</title>
<style>html,body{margin:0;height:100%}iframe{border:0;width:100%;height:100%;display:block}</style>
<iframe id="card" sandbox="allow-scripts allow-forms" src="${PREFIX}${token}${cardPath}"></iframe>
<script>
window.bridgeLog = [];
const frame = document.getElementById("card");
function init() {
  const channel = new MessageChannel();
  channel.port1.onmessage = (ev) => window.bridgeLog.push(ev.data);
  frame.contentWindow.postMessage(
    { type: "t3.bridge.init", version: 1, surfaceId: "${SURFACE_ID}", features: ["surface.maximize", "surface.resize"] },
    "*",
    [channel.port2]
  );
}
frame.addEventListener("load", init);
window.addEventListener("message", (ev) => {
  if (ev.source === frame.contentWindow && ev.data && ev.data.type === "t3.bridge.hello") init();
});
</script>`;
}

function startGateway() {
  const server = http.createServer((req, res) => {
    const rawPath = req.url.split("?", 1)[0];
    const search = req.url.slice(rawPath.length);
    if (rawPath === "/favicon.ico") {
      res.writeHead(204);
      res.end();
      return;
    }
    if (rawPath === "/host") {
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
      res.end(hostPage(new URL(req.url, gateway).searchParams.get("card")));
      return;
    }
    const entry = { method: req.method, path: rawPath, status: 0 };
    gatewayLog.push(entry);
    const finish = (status, headers, body) => {
      entry.status = status;
      res.writeHead(status, headers);
      res.end(body);
    };
    if (!pluginRelativePath(rawPath) || !rawPath.startsWith(PREFIX + token + "/")) return finish(400, {});
    const headers = gatewayHeaders();
    if (req.method === "OPTIONS") return finish(204, headers);
    const chunks = [];
    let size = 0;
    let tooBig = false;
    req.on("data", (c) => {
      size += c.length;
      if (size > REQUEST_LIMIT) tooBig = true;
      else chunks.push(c);
    });
    req.on("end", () => {
      if (tooBig) return finish(502, headers, "Plugin gateway unavailable");
      const forward = { accept: req.headers.accept || "*/*", "content-type": req.headers["content-type"] || "application/octet-stream" };
      const up = http.request(upstream + rawPath.slice((PREFIX + token).length) + search, { method: req.method, headers: forward }, (ur) => {
        if (ur.statusCode >= 300 && ur.statusCode < 400) {
          ur.resume();
          return finish(502, headers, "Plugin redirects are not supported");
        }
        const out = [];
        let n = 0;
        ur.on("data", (c) => {
          n += c.length;
          out.push(c);
        });
        ur.on("end", () => {
          if (n > RESPONSE_LIMIT) return finish(502, headers, "Plugin gateway unavailable");
          finish(ur.statusCode, { ...headers, "content-type": ur.headers["content-type"] || "application/octet-stream" }, Buffer.concat(out));
        });
      });
      up.on("error", () => finish(502, headers, "Plugin gateway unavailable"));
      if (req.method !== "GET" && req.method !== "HEAD") up.write(Buffer.concat(chunks));
      up.end();
    });
  });
  // A socket is relayed with the handshake a server-side client sends:
  // no Origin, no cookies.
  server.on("upgrade", (req, socket) => {
    const rawPath = req.url.split("?", 1)[0];
    const search = req.url.slice(rawPath.length);
    gatewayLog.push({ method: "WS", path: rawPath, status: 101 });
    if (!pluginRelativePath(rawPath) || !rawPath.startsWith(PREFIX + token + "/")) return socket.destroy();
    const up = net.connect(t3Port, "127.0.0.1", () => {
      up.write(
        `GET ${rawPath.slice((PREFIX + token).length)}${search} HTTP/1.1\r\n` +
          `Host: 127.0.0.1:${t3Port}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n` +
          `Sec-WebSocket-Key: ${req.headers["sec-websocket-key"]}\r\nSec-WebSocket-Version: 13\r\n\r\n`
      );
      up.pipe(socket);
      socket.pipe(up);
    });
    up.on("error", () => socket.destroy());
    socket.on("error", () => up.destroy());
  });
  return new Promise((resolve) => server.listen(gatewayPort, "127.0.0.1", () => resolve(server)));
}

// One request over the plugin socket, as T3's server sends it.
function pluginRequest(frame) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket("ws://127.0.0.1:" + t3Port + "/t3");
    const timer = setTimeout(() => reject(new Error("no answer from the plugin socket")), 10000);
    ws.onopen = () => ws.send(JSON.stringify(frame));
    ws.onmessage = (ev) => {
      clearTimeout(timer);
      ws.close();
      resolve(JSON.parse(String(ev.data)));
    };
    ws.onerror = () => reject(new Error("plugin socket failed"));
  });
}

const filler = "Filler prose so the page is taller than the window. ".repeat(14);
const markdown = [
  "# T3 journey",
  "",
  "See [the other note](other%20note.md#part), [a table](table.csv) and [a site](https://example.invalid/page).",
  "",
  "- [ ] first task",
  "- [ ] second task",
  "",
  "![blue](pic.png)",
  "",
  "<script>window.pwned = 1</script><img src=\"x\" onerror=\"window.pwned = 2\">",
  "",
  "Inline math $x^2 + y^2$.",
  "",
  "```mermaid",
  "graph TD",
  "  A --> B",
  "```",
  "",
  ...["Alpha", "Beta", "Gamma", "Delta"].flatMap((s) => ["## " + s, "", filler, "", filler, ""]),
  "## Last",
  "",
  "The end.",
  "",
].join("\n");

// A 2×2 blue PNG.
function pngChunk(type, data) {
  const body = Buffer.concat([Buffer.from(type), data]);
  const out = Buffer.alloc(body.length + 8);
  out.writeUInt32BE(data.length, 0);
  body.copy(out, 4);
  out.writeUInt32BE(zlib.crc32(body) >>> 0, body.length + 4);
  return out;
}
const pngHeader = Buffer.from([0, 0, 0, 2, 0, 0, 0, 2, 8, 2, 0, 0, 0]); // 2×2, 8-bit RGB
const pngRows = Buffer.from([0, 0, 0, 255, 0, 0, 255, 0, 0, 0, 255, 0, 0, 255]); // filter byte, then two blue pixels, twice
const png = Buffer.concat([
  Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
  pngChunk("IHDR", pngHeader),
  pngChunk("IDAT", zlib.deflateSync(pngRows)),
  pngChunk("IEND", Buffer.alloc(0)),
]);

(async () => {
  const base = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "vellum-t3-journey-")));
  // A folder name with a space: the gateway refuses such a path, so the
  // page must never put one in a URL.
  const dir = path.join(base, "my notes");
  fs.mkdirSync(dir);
  const doc = path.join(dir, "journey.md");
  const other = path.join(dir, "other note.md");
  fs.writeFileSync(doc, markdown);
  fs.writeFileSync(other, "# Other note\n\n## Part\n\nThe other body.\n");
  fs.writeFileSync(path.join(dir, "table.csv"), "a,b\n");
  fs.writeFileSync(path.join(dir, "pic.png"), png);
  fs.writeFileSync(path.join(dir, "evil.svg"), '<svg xmlns="http://www.w3.org/2000/svg"><script>parent.postMessage("svg-ran","*")</script></svg>');

  // The viewer advertises itself in a folder of this run's, never in the
  // one a real T3 Code reads.
  const home = path.join(base, "home");
  fs.mkdirSync(home);
  const env = { ...process.env, XDG_DATA_HOME: path.join(home, "data") };
  const server = spawn(
    process.env.VELLUM_BINARY || "./vellum",
    ["serve-view", "--addr", "127.0.0.1:" + viewPort, "--t3-addr", "127.0.0.1:" + t3Port],
    { stdio: "inherit", env }
  );
  const gw = await startGateway();
  const manifest = path.join(home, "data", "t3code", "plugins", `github.com+marcelocantos+vellum+${t3Port}.json`);
  try {
    for (let attempt = 0; ; attempt++) {
      try {
        const probe = await (await fetch(upstream + "/t3", { headers: { accept: "application/json" } })).json();
        check(probe.id === "github.com/marcelocantos/vellum" && probe.scopes.join() === "links", "probe: " + JSON.stringify(probe));
        break;
      } catch (e) {
        if (attempt >= 50) throw e;
        await new Promise((r) => setTimeout(r, 100));
      }
    }
    check(fs.existsSync(manifest), "no discovery file while the viewer runs");
    if (fs.existsSync(manifest)) {
      const m = JSON.parse(fs.readFileSync(manifest, "utf8"));
      check(m.connection.baseUrl === upstream + "/t3", "discovery file names " + m.connection.baseUrl);
    }

    // Nothing is served before T3 relays the user's link.
    const docToken = Buffer.from(doc).toString("base64url");
    check((await fetch(root + "d/" + docToken)).status === 403, "the document is served before any link request");
    const answer = await pluginRequest({ type: "surface", requestId: "r1", scope: "link", linkId: "markdown", url: doc });
    check(answer.ok === true && answer.web && pluginRelativePath(answer.web.path), "link request: " + JSON.stringify(answer));
    const cardPath = answer.web.path;

    for (const [name, opts] of [["chromium", { channel: "chrome" }], ["firefox", {}]]) {
      fs.writeFileSync(doc, markdown);
      const browser = await pw[name].launch({ headless: true, ...opts });
      try {
        const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
        page.on("pageerror", (e) => failures.push(`${name}: page error ${e.message}`));
        const blocked = [];
        page.on("console", (m) => {
          if (/Content Security Policy|Content-Security-Policy/i.test(m.text())) blocked.push(m.text());
        });
        await page.goto(gateway + "/host?card=" + encodeURIComponent(cardPath));
        const frame = () => page.frames().find((f) => f.url().startsWith(root));
        const waitFrame = async (what) => {
          for (let i = 0; i < 100; i++) {
            const f = frame();
            if (f && (await f.evaluate(what).catch(() => false))) return f;
            await page.waitForTimeout(100);
          }
          throw new Error(`${name}: frame never satisfied ${what}`);
        };
        let f = await waitFrame(() => Boolean(document.querySelector(".vellum-toc-nav a")));

        // The page: its own styles and script ran, in embedded form.
        check((await f.locator(".vellum-article h1").innerText()) === "T3 journey", `${name}: the document is not shown`);
        check((await f.locator(".vellum-toolbar").evaluate((e) => getComputedStyle(e).display)) === "flex", `${name}: the page has no styles`);
        check((await f.locator(".vellum-docname").innerText()) === "journey.md", `${name}: the bar does not name the file`);
        for (const gone of [".vellum-home", '[data-vellum="pdf"]', '[data-vellum="clipboard"]', '[data-vellum="reveal"]']) {
          check((await f.locator(gone).count()) === 0, `${name}: embedded page offers ${gone}`);
        }
        check((await f.locator('[data-vellum="edit"]').count()) === 1, `${name}: embedded page has no edit button`);

        // What the document carries does not run. Nor would markup that
        // got past the sanitiser: the page's own policy refuses inline
        // script without its nonce, although the gateway's allows it.
        check((await f.evaluate(() => window.pwned)) === undefined, `${name}: the document's script ran`);
        const injected = await f.evaluate(async () => {
          const div = document.createElement("div");
          div.innerHTML = '<img src="data:image/png;base64,AA" onerror="window.injected = 1">';
          document.body.appendChild(div);
          await new Promise((r) => setTimeout(r, 300));
          return window.injected;
        });
        check(injected === undefined, `${name}: markup without the nonce ran script in the embedded page`);

        // Image, math and diagram arrive through the gateway.
        await f.locator(".vellum-article img[alt=blue]").scrollIntoViewIfNeeded();
        await waitFrame(() => {
          const img = document.querySelector(".vellum-article img[alt=blue]");
          return img && img.complete && img.naturalWidth === 2;
        }).catch(() => failures.push(`${name}: the image did not load`));
        await waitFrame(() => Boolean(document.querySelector(".vellum-article .katex"))).catch(() => failures.push(`${name}: math was not rendered`));
        await f.locator(".vellum-deferred-mermaid, .mermaid-svg").first().scrollIntoViewIfNeeded();
        await waitFrame(() => Boolean(document.querySelector(".vellum-article svg[id^=mmd-]"))).catch(() => failures.push(`${name}: the diagram was not rendered`));
        const katexFont = await f.evaluate(async () => {
          await document.fonts.ready;
          return [...document.fonts].some((face) => /KaTeX/.test(face.family) && face.status === "loaded");
        });
        check(katexFont, `${name}: no KaTeX font loaded through the gateway`);

        // The bridge: the card asks for the whole window.
        await page.waitForFunction(() => window.bridgeLog.some((m) => m.type === "surface.update" && m.maximized === true));
        const asked = await page.evaluate(() => window.bridgeLog);
        check(asked.every((m) => m.surfaceId === "link:markdown"), `${name}: bridge messages name another surface: ${JSON.stringify(asked)}`);

        // A link that leaves the document, and one to a file that is not
        // Markdown, keep the reader on the page.
        const before = f.url();
        await f.locator('.vellum-article a:has-text("a site")').click();
        await page.waitForTimeout(300);
        check(frame() && frame().url() === before, `${name}: a link to another site navigated the card`);
        check(/outside the document/.test(await f.locator("#vellum-status").innerText()), `${name}: no note for the outside link`);
        await f.locator('.vellum-article a:has-text("a table")').click();
        await page.waitForTimeout(300);
        check(frame() && frame().url() === before, `${name}: a link to a non-Markdown file navigated the card`);

        // A task: ticking it edits the file (stamp in the query).
        await f.locator('.vellum-article input[type=checkbox]').nth(1).click();
        await f.locator('[data-vellum="consent-session"]').click();
        for (let i = 0; i < 50 && !fs.readFileSync(doc, "utf8").includes("- [x] second task"); i++) await page.waitForTimeout(100);
        check(fs.readFileSync(doc, "utf8").includes("- [x] second task"), `${name}: ticking a task did not change the file`);
        check(fs.readFileSync(doc, "utf8").includes("- [ ] first task"), `${name}: ticking the second task changed the first`);

        // A change on disk reloads the page where the reader was.
        await page.waitForTimeout(1500);
        f = frame();
        await f.evaluate(() => {
          const h = [...document.querySelectorAll(".vellum-article h2")].find((e) => e.textContent.trim() === "Gamma");
          const pane = document.getElementById("vellum-scroll");
          pane.scrollTop += h.getBoundingClientRect().top - pane.getBoundingClientRect().top - 4;
        });
        await page.waitForTimeout(300);
        fs.writeFileSync(doc, fs.readFileSync(doc, "utf8").replace("The end.", "The end, rewritten."));
        f = await waitFrame(() => /rewritten/.test(document.querySelector(".vellum-article").textContent));
        await page.waitForTimeout(500);
        const gammaTop = await f.evaluate(() => {
          const h = [...document.querySelectorAll(".vellum-article h2")].find((e) => e.textContent.trim() === "Gamma");
          return Math.round(h.getBoundingClientRect().top - document.getElementById("vellum-scroll").getBoundingClientRect().top);
        });
        check(Math.abs(gammaTop) < 80, `${name}: after a reload the page is not where the reader was (Gamma at ${gammaTop}px)`);

        // The editor loads through the gateway and saves (1 MiB limit,
        // PUT, stamp in the query); a conflict is seen and overwritten.
        await f.locator('[data-vellum="edit"]').click();
        await f.locator("#vellum-editor .cm-content").waitFor();
        await f.locator("#vellum-editor .cm-content").click();
        await page.keyboard.press(process.platform === "darwin" ? "Meta+End" : "Control+End");
        await page.keyboard.type("\nTyped in T3.\n");
        await f.locator('[data-vellum="ed-save"]').click();
        for (let i = 0; i < 50 && !fs.readFileSync(doc, "utf8").includes("Typed in T3."); i++) await page.waitForTimeout(100);
        check(fs.readFileSync(doc, "utf8").includes("Typed in T3."), `${name}: the editor did not save`);
        await f.locator("#vellum-editor .cm-content").click();
        await page.keyboard.press(process.platform === "darwin" ? "Meta+End" : "Control+End");
        await page.keyboard.type("More.\n");
        fs.writeFileSync(doc, fs.readFileSync(doc, "utf8") + "\nWritten by someone else.\n");
        await f.locator(".vellum-editor-banner:not([hidden])").waitFor();
        check(/changed on disk/.test(await f.locator(".vellum-editor-banner").innerText()), `${name}: no conflict banner`);
        await f.locator('.vellum-editor-banner button:has-text("Overwrite")').click();
        for (let i = 0; i < 50 && !fs.readFileSync(doc, "utf8").includes("More."); i++) await page.waitForTimeout(100);
        const saved = fs.readFileSync(doc, "utf8");
        check(saved.includes("More.") && !saved.includes("someone else"), `${name}: overwrite after a conflict did not save the editor's text`);
        await f.locator('[data-vellum="ed-close"]').click();

        // A link to another document opens it in the card.
        f = await waitFrame(() => Boolean(document.querySelector('.vellum-article a[href*="/d/"], .vellum-article a[href^="../d/"]')));
        await f.evaluate(() => (document.getElementById("vellum-scroll").scrollTop = 0));
        await f.locator('.vellum-article a:has-text("the other note")').click();
        f = await waitFrame(() => document.querySelector(".vellum-article h1") && document.querySelector(".vellum-article h1").textContent === "Other note");
        check(f.url().startsWith(root + "d/") && f.url().endsWith("#part"), `${name}: the linked document's URL is ${f.url()}`);

        // The frame navigated to a file a browser would run gets nothing.
        const svgToken = Buffer.from(path.join(dir, "evil.svg")).toString("base64url");
        let svgRan = false;
        await page.exposeFunction("noteSvg", () => (svgRan = true));
        await page.evaluate(() => window.addEventListener("message", (ev) => ev.data === "svg-ran" && window.noteSvg()));
        await page.evaluate((src) => (document.getElementById("card").src = src), root + "f/" + svgToken);
        await page.waitForTimeout(600);
        check(!svgRan, `${name}: an SVG file's script ran in the card`);

        // Two refusals are expected: the inline handler injected above,
        // and the font Mermaid embeds in every diagram as a data: URL for
        // its hand-drawn look, which the gateway's font-src does not
        // admit. Diagrams drawn in the default look do not use it.
        const needed = blocked.filter((m) => !/inline|event handler|script-src-attr|data:font/i.test(m));
        check(needed.length === 0, `${name}: a policy blocked something the page needs: ${needed.join(" | ")}`);
      } finally {
        await browser.close();
      }
    }

    // Everything the pages asked for was a path the gateway relays, and
    // nothing came back as a gateway failure.
    const refused = gatewayLog.filter((e) => e.status === 400);
    check(refused.length === 0, "the page used paths T3's gateway refuses: " + refused.map((e) => e.path).join(", "));
    const failed = gatewayLog.filter((e) => e.status === 502);
    check(failed.length === 0, "gateway failures: " + failed.map((e) => e.method + " " + e.path).join(", "));
    check(gatewayLog.some((e) => e.method === "WS"), "the page opened no watch socket through the gateway");
    check(gatewayLog.some((e) => e.method === "PUT" && e.status === 200), "no save went through the gateway");

    // More than the gateway's request limit is a failed save, not a
    // truncated file.
    const big = "x".repeat(REQUEST_LIMIT + 1);
    const resp = await fetch(root + "_vellum/edit?path=" + encodeURIComponent(doc) + "&stamp=whatever", { method: "PUT", body: big });
    check(resp.status === 502, "a save over 1 MiB got status " + resp.status);
    check(!fs.readFileSync(doc, "utf8").includes("xxxxxxxxxx"), "a save over 1 MiB reached the file");
  } finally {
    server.kill("SIGTERM");
    await new Promise((r) => server.once("exit", r));
    gw.close();
  }
  check(!fs.existsSync(manifest), "the discovery file outlived the viewer");
  if (failures.length) {
    console.log("FAIL:\n  " + failures.join("\n  "));
    process.exit(1);
  }
  console.log("PASS: chrome and firefox through a T3-rules gateway: discovery, link request, page, image, math, diagram, bridge, links, task, reload in place, editor save and conflict, document links, no script from documents or SVG, no refused path");
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
