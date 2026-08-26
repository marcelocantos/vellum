(function () {
  const chrome = document.querySelector(".vellum-chrome");
  if (!chrome) return;

  const source = chrome.getAttribute("data-source") || "";
  const article = chrome.querySelector(".vellum-article");
  const scrollPane = document.getElementById("vellum-scroll");
  const toc = document.getElementById("vellum-toc");
  const tocNav = document.getElementById("vellum-toc-nav");
  const tocCollapseBtn = chrome.querySelector('[data-vellum="toc-collapse"]');
  const tocExpandBtn = chrome.querySelector('[data-vellum="toc-expand"]');
  const tocToggleBtn = chrome.querySelector('[data-vellum="toc-toggle"]');

  function toggleTocSidebar() {
    if (!toc) return;
    let mobile = false;
    try {
      mobile = window.matchMedia("(max-width: 880px)").matches;
    } catch (e) {}
    if (mobile) {
      toc.classList.toggle("is-open");
      toc.classList.remove("is-collapsed");
    } else {
      toc.classList.toggle("is-collapsed");
      toc.classList.remove("is-open");
    }
    updateTocToggleButton();
  }
  const status = document.getElementById("vellum-status");
  const lightbox = document.getElementById("vellum-lightbox");
  const stage = document.getElementById("vellum-lightbox-stage");
  const content = document.getElementById("vellum-lightbox-content");
  const hint = document.getElementById("vellum-lightbox-hint");

  const THEME_KEY = "vellum-theme";
  const THEME_ORDER = ["dark", "system", "light"];
  const THEME_ICON = { dark: "\u263E", system: "\u25D0", light: "\u2600" };

  function nextTheme(mode) {
    const i = THEME_ORDER.indexOf(mode);
    return THEME_ORDER[(i + 1) % THEME_ORDER.length];
  }

  function themeLabel(mode) {
    return mode.charAt(0).toUpperCase() + mode.slice(1);
  }

  function updateThemeButton(mode) {
    const themeBtn = chrome.querySelector('[data-vellum="theme"]');
    if (!themeBtn) return;
    const next = nextTheme(mode);
    themeBtn.textContent = THEME_ICON[mode] || mode;
    themeBtn.title = "Theme: " + themeLabel(mode) + " (click for " + themeLabel(next) + ")";
    themeBtn.setAttribute("aria-label", themeBtn.title);
  }

  function applyTheme(mode) {
    if (THEME_ORDER.indexOf(mode) < 0) mode = "system";
    document.documentElement.dataset.vellumTheme = mode;
    try {
      localStorage.setItem(THEME_KEY, mode);
    } catch (e) {}
    updateThemeButton(mode);
  }

  function initTheme() {
    let mode = "system";
    try {
      const saved = localStorage.getItem(THEME_KEY);
      if (THEME_ORDER.indexOf(saved) >= 0) mode = saved;
    } catch (e) {}
    applyTheme(mode);
    try {
      window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", function () {
        if (document.documentElement.dataset.vellumTheme === "system") {
          updateThemeButton("system");
        }
      });
    } catch (e) {}
  }

  initTheme();

  function tocSidebarVisible() {
    if (!toc) return false;
    if (toc.classList.contains("is-collapsed")) return false;
    try {
      if (window.matchMedia("(max-width: 880px)").matches) {
        return toc.classList.contains("is-open");
      }
    } catch (e) {}
    return true;
  }

  function updateTocToggleButton() {
    if (!tocToggleBtn) return;
    const visible = tocSidebarVisible();
    tocToggleBtn.textContent = visible ? "\u00AB" : "\u00BB";
    const label = visible ? "Hide table of contents" : "Show table of contents";
    tocToggleBtn.title = label;
    tocToggleBtn.setAttribute("aria-label", label);
  }

  updateTocToggleButton();
  try {
    window.matchMedia("(max-width: 880px)").addEventListener("change", updateTocToggleButton);
  } catch (e) {}

  function focusScrollPane() {
    if (scrollPane) scrollPane.focus({ preventScroll: true });
  }

  function setStatus(msg, ok) {
    if (!status) return;
    status.hidden = !msg;
    status.textContent = msg || "";
    status.classList.toggle("is-error", Boolean(msg) && !ok);
    if (msg && ok) {
      window.setTimeout(function () {
        if (status.textContent === msg) {
          status.hidden = true;
        }
      }, 2500);
    }
  }

  function actionURL(name) {
    return "/_vellum/" + name + "?path=" + encodeURIComponent(source);
  }

  chrome.querySelectorAll("[data-vellum]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      const action = btn.getAttribute("data-vellum");
      if (action === "theme") {
        applyTheme(nextTheme(document.documentElement.dataset.vellumTheme || "system"));
        return;
      }
      if (action === "pdf") {
        window.location.assign(actionURL("pdf"));
        return;
      }
      if (action === "toc-toggle") {
        toggleTocSidebar();
        return;
      }
      if (action === "toc-expand") {
        cancelTOCFill();
        setTOCCollapsed(false);
        return;
      }
      if (action === "toc-collapse") {
        cancelTOCFill();
        collapseToFirstBranch();
        return;
      }
      if (action === "clipboard" || action === "reveal") {
        btn.disabled = true;
        fetch(actionURL(action), { method: "POST" })
          .then(function (resp) {
            return resp.text().then(function (text) {
              if (!resp.ok) {
                throw new Error(text || resp.statusText);
              }
              return text;
            });
          })
          .then(function () {
            setStatus(action === "clipboard" ? "Copied to clipboard" : "Revealed in Finder", true);
          })
          .catch(function (err) {
            setStatus(err.message || String(err), false);
          })
          .finally(function () {
            btn.disabled = false;
          });
      }
    });
  });

  function headingLevel(el) {
    return parseInt(el.tagName.charAt(1), 10);
  }

  function ensureID(el, used) {
    if (el.id) return el.id;
    const base = (el.textContent || "section")
      .trim()
      .toLowerCase()
      .replace(/[^\w]+/g, "-")
      .replace(/^-+|-+$/g, "") || "section";
    let id = base;
    let n = 2;
    while (used[id] || document.getElementById(id)) {
      id = base + "-" + n;
      n += 1;
    }
    el.id = id;
    used[id] = true;
    return id;
  }

  function buildTOC() {
    if (!article || !tocNav || !toc) return;
    const headings = Array.prototype.slice.call(
      article.querySelectorAll("h1, h2, h3, h4, h5, h6")
    );
    if (!headings.length) {
      toc.hidden = true;
      return;
    }
    toc.hidden = false;
    const used = {};
    const root = document.createElement("ol");
    const stack = [{ level: 0, ol: root }];

    headings.forEach(function (h) {
      const level = headingLevel(h);
      const id = ensureID(h, used);
      while (stack.length > 1 && stack[stack.length - 1].level >= level) {
        stack.pop();
      }
      const parent = stack[stack.length - 1];
      const li = document.createElement("li");
      li.className = "vellum-toc-item";
      li.dataset.level = String(level);

      const row = document.createElement("div");
      row.className = "vellum-toc-row";
      const twist = document.createElement("button");
      twist.type = "button";
      twist.className = "vellum-toc-twist";
      twist.setAttribute("aria-label", "Toggle section");
      twist.textContent = "▾";
      twist.hidden = true;
      const a = document.createElement("a");
      // Fragment-only hrefs resolve against <base> (the source directory),
      // which would navigate off the Markdown page.
      a.href = location.pathname + location.search + "#" + encodeURIComponent(id);
      a.textContent = (h.textContent || "").trim() || id;
      row.appendChild(twist);
      row.appendChild(a);
      li.appendChild(row);

      const kids = document.createElement("ol");
      kids.className = "vellum-toc-kids";
      li.appendChild(kids);
      parent.ol.appendChild(li);
      stack.push({ level: level, ol: kids, li: li, twist: twist });
    });

    root.querySelectorAll(".vellum-toc-item").forEach(function (li) {
      const kids = li.querySelector(":scope > .vellum-toc-kids");
      const twist = li.querySelector(":scope > .vellum-toc-row > .vellum-toc-twist");
      if (!kids || !twist) return;
      if (kids.children.length === 0) {
        kids.remove();
        return;
      }
      twist.hidden = false;
      twist.addEventListener("click", function (ev) {
        ev.preventDefault();
        cancelTOCFill();
        setItemCollapsed(li, !li.classList.contains("is-collapsed"));
      });
    });

    tocNav.replaceChildren(root);
  }

  let tocFillGen = 0;

  function cancelTOCFill() {
    tocFillGen += 1;
  }

  function setItemCollapsed(li, collapsed) {
    const kids = li.querySelector(":scope > .vellum-toc-kids");
    if (!kids) return;
    li.classList.toggle("is-collapsed", collapsed);
    const twist = li.querySelector(":scope > .vellum-toc-row > .vellum-toc-twist");
    if (twist) twist.textContent = collapsed ? "▸" : "▾";
    updateTOCActionButtons();
  }

  function tocItemsIn(ol) {
    return Array.prototype.filter.call(ol.children, function (li) {
      return li.classList && li.classList.contains("vellum-toc-item");
    });
  }

  function findBranchOl() {
    let ol = tocNav.querySelector(":scope > ol");
    while (ol) {
      const items = tocItemsIn(ol);
      if (items.length > 1) return ol;
      if (items.length === 1) {
        ol = items[0].querySelector(":scope > .vellum-toc-kids");
      } else {
        return null;
      }
    }
    return null;
  }

  function collapseToFirstBranch() {
    if (!tocNav) return;
    const branchOl = findBranchOl();
    if (!branchOl) {
      setTOCCollapsed(true);
      return;
    }
    tocNav.querySelectorAll(".vellum-toc-item").forEach(function (li) {
      const kids = li.querySelector(":scope > .vellum-toc-kids");
      if (!kids) return;
      if (kids.contains(branchOl) || kids === branchOl) {
        setItemCollapsed(li, false);
      } else if (branchOl.contains(li)) {
        setItemCollapsed(li, true);
      } else {
        setItemCollapsed(li, true);
      }
    });
    updateTOCActionButtons();
  }

  function tocFullyExpanded() {
    if (!tocNav) return true;
    let expandable = false;
    const items = tocNav.querySelectorAll(".vellum-toc-item");
    for (let i = 0; i < items.length; i++) {
      const li = items[i];
      if (!li.querySelector(":scope > .vellum-toc-kids")) continue;
      expandable = true;
      if (li.classList.contains("is-collapsed")) return false;
    }
    return expandable;
  }

  function updateTOCActionButtons() {
    if (tocExpandBtn) tocExpandBtn.disabled = tocFullyExpanded();
  }

  function setTOCCollapsed(collapsed) {
    if (!tocNav) return;
    tocNav.querySelectorAll(".vellum-toc-item").forEach(function (li) {
      const kids = li.querySelector(":scope > .vellum-toc-kids");
      if (!kids) return;
      li.classList.toggle("is-collapsed", collapsed);
      const twist = li.querySelector(":scope > .vellum-toc-row > .vellum-toc-twist");
      if (twist) twist.textContent = collapsed ? "▸" : "▾";
    });
    updateTOCActionButtons();
  }

  // Expandable nodes in BFS order: one tree-depth at a time, top to bottom.
  function tocExpandQueue() {
    const queue = [];
    function walk(ol, depth) {
      if (!ol) return;
      const items = [];
      Array.prototype.forEach.call(ol.children, function (li) {
        if (!li.classList || !li.classList.contains("vellum-toc-item")) return;
        if (li.querySelector(":scope > .vellum-toc-kids")) items.push(li);
      });
      items.forEach(function (li) {
        queue[depth] = queue[depth] || [];
        queue[depth].push(li);
      });
      Array.prototype.forEach.call(ol.children, function (li) {
        if (!li.classList || !li.classList.contains("vellum-toc-item")) return;
        walk(li.querySelector(":scope > .vellum-toc-kids"), depth + 1);
      });
    }
    walk(tocNav.querySelector(":scope > ol"), 0);
    const flat = [];
    queue.forEach(function (level) {
      level.forEach(function (li) {
        flat.push(li);
      });
    });
    return flat;
  }

  function tocOverflows() {
    return toc.scrollHeight > toc.clientHeight + 1;
  }

  function fillTOC() {
    if (!toc || !tocNav) return;
    const gen = (tocFillGen += 1);
    setTOCCollapsed(true);
    const items = tocExpandQueue();

    function step(i) {
      if (gen !== tocFillGen) return;
      if (i >= items.length) {
        updateTOCActionButtons();
        return;
      }
      setItemCollapsed(items[i], false);
      if (tocOverflows()) {
        setItemCollapsed(items[i], true);
        updateTOCActionButtons();
        return;
      }
      window.setTimeout(function () {
        step(i + 1);
      }, 40);
    }

    function start() {
      if (gen !== tocFillGen) return;
      if (toc.clientHeight === 0) {
        requestAnimationFrame(start);
        return;
      }
      step(0);
    }
    requestAnimationFrame(start);
  }

  buildTOC();
  fillTOC();
  focusScrollPane();

  const lb = {
    scale: 1,
    x: 0,
    y: 0,
    dragging: false,
    px: 0,
    py: 0,
    ox: 0,
    oy: 0,
    naturalW: 0,
    naturalH: 0,
  };

  function applyTransform() {
    if (!content) return;
    content.style.transform =
      "translate(" + lb.x + "px," + lb.y + "px) scale(" + lb.scale + ")";
  }

  function fitContent() {
    if (!stage || !content) return;
    const pad = 32;
    const sw = stage.clientWidth - pad;
    const sh = stage.clientHeight - pad;
    const w = lb.naturalW || content.offsetWidth || 1;
    const h = lb.naturalH || content.offsetHeight || 1;
    lb.scale = Math.min(1, sw / w, sh / h);
    lb.x = (stage.clientWidth - w * lb.scale) / 2;
    lb.y = (stage.clientHeight - h * lb.scale) / 2;
    applyTransform();
    if (hint) {
      hint.textContent =
        Math.round(lb.scale * 100) + "% · scroll to zoom · drag to pan · Esc to close";
    }
  }

  function svgViewBoxSize(svg) {
    const vb = svg.viewBox && svg.viewBox.baseVal;
    if (vb && vb.width > 0 && vb.height > 0) {
      return { w: vb.width, h: vb.height };
    }
    return null;
  }

  function prepareSvgForLightbox(svg) {
    const clone = svg.cloneNode(true);
    const size = svgViewBoxSize(clone);
    clone.removeAttribute("width");
    clone.removeAttribute("height");
    clone.style.removeProperty("max-width");
    clone.style.removeProperty("width");
    clone.style.removeProperty("height");
    if (size) {
      clone.setAttribute("width", String(size.w));
      clone.setAttribute("height", String(size.h));
    }
    return clone;
  }

  function svgDataUrl(svg) {
    const xml = new XMLSerializer().serializeToString(prepareSvgForLightbox(svg));
    return "data:image/svg+xml;charset=utf-8," + encodeURIComponent(xml);
  }

  function measureNode(node) {
    if (node.tagName === "IMG") {
      lb.naturalW = node.naturalWidth || node.width || node.offsetWidth;
      lb.naturalH = node.naturalHeight || node.height || node.offsetHeight;
      return;
    }
    lb.naturalW = node.clientWidth || node.offsetWidth;
    lb.naturalH = node.clientHeight || node.offsetHeight;
  }

  function openLightbox(el) {
    if (!lightbox || !content || !stage) return;
    content.replaceChildren();
    lb.naturalW = 0;
    lb.naturalH = 0;

    let displayEl;
    const svg =
      el.tagName === "svg"
        ? el
        : el.classList && el.classList.contains("mermaid-svg")
          ? el.querySelector("svg")
          : null;

    if (svg) {
      // Serialize to a data URL so styles/defs stay self-contained — cloning
      // inline SVG breaks on duplicate ids (#my-svg) and width="100%" sizing.
      const size = svgViewBoxSize(svg);
      displayEl = document.createElement("img");
      displayEl.alt = "";
      displayEl.src = svgDataUrl(svg);
      if (size) {
        lb.naturalW = size.w;
        lb.naturalH = size.h;
      }
    } else if (el.tagName === "IMG") {
      displayEl = el.cloneNode(true);
      if (displayEl.removeAttribute) {
        displayEl.removeAttribute("width");
        displayEl.removeAttribute("height");
      }
      displayEl.style.maxWidth = "none";
      displayEl.style.maxHeight = "none";
      displayEl.style.width = "auto";
      displayEl.style.height = "auto";
    } else {
      return;
    }

    content.appendChild(displayEl);
    lightbox.hidden = false;
    document.body.style.overflow = "hidden";
    const ready = function () {
      if (!lb.naturalW || !lb.naturalH) measureNode(displayEl);
      fitContent();
    };
    if (displayEl.tagName === "IMG" && !displayEl.complete) {
      displayEl.addEventListener("load", ready, { once: true });
    } else {
      if (displayEl.tagName === "IMG" && displayEl.naturalWidth) {
        lb.naturalW = lb.naturalW || displayEl.naturalWidth;
        lb.naturalH = lb.naturalH || displayEl.naturalHeight;
      }
      requestAnimationFrame(ready);
    }
  }

  function closeLightbox() {
    if (!lightbox) return;
    lightbox.hidden = true;
    document.body.style.overflow = "";
    if (content) content.replaceChildren();
    focusScrollPane();
  }

  function zoomAt(clientX, clientY, factor) {
    if (!stage) return;
    const rect = stage.getBoundingClientRect();
    const sx = clientX - rect.left;
    const sy = clientY - rect.top;
    const next = Math.min(8, Math.max(0.1, lb.scale * factor));
    const ratio = next / lb.scale;
    lb.x = sx - (sx - lb.x) * ratio;
    lb.y = sy - (sy - lb.y) * ratio;
    lb.scale = next;
    applyTransform();
    if (hint) {
      hint.textContent =
        Math.round(lb.scale * 100) + "% · scroll to zoom · drag to pan · Esc to close";
    }
  }

  if (article) {
    article.addEventListener("click", function (ev) {
      const t = ev.target;
      if (!(t instanceof Element)) return;
      const img = t.closest("img");
      if (img && article.contains(img)) {
        ev.preventDefault();
        openLightbox(img);
        return;
      }
      const mermaid = t.closest(".mermaid-svg");
      if (mermaid && article.contains(mermaid)) {
        ev.preventDefault();
        openLightbox(mermaid);
        return;
      }
      const svg = t.closest("svg");
      if (svg && article.contains(svg) && !t.closest("a")) {
        ev.preventDefault();
        openLightbox(svg);
      }
    });
  }

  if (lightbox) {
    lightbox.querySelectorAll("[data-vellum]").forEach(function (btn) {
      btn.addEventListener("click", function (ev) {
        ev.stopPropagation();
        const action = btn.getAttribute("data-vellum");
        if (action === "lb-close") closeLightbox();
        if (action === "lb-reset") fitContent();
        if (action === "lb-in") {
          zoomAt(stage.clientWidth / 2, stage.clientHeight / 2, 1.25);
        }
        if (action === "lb-out") {
          zoomAt(stage.clientWidth / 2, stage.clientHeight / 2, 0.8);
        }
      });
    });
  }

  if (stage) {
    stage.addEventListener(
      "wheel",
      function (ev) {
        ev.preventDefault();
        const factor = ev.deltaY < 0 ? 1.12 : 1 / 1.12;
        zoomAt(ev.clientX, ev.clientY, factor);
      },
      { passive: false }
    );
    stage.addEventListener("pointerdown", function (ev) {
      if (ev.button !== 0) return;
      lb.dragging = true;
      lb.px = ev.clientX;
      lb.py = ev.clientY;
      lb.ox = lb.x;
      lb.oy = lb.y;
      stage.classList.add("is-panning");
      stage.setPointerCapture(ev.pointerId);
    });
    stage.addEventListener("pointermove", function (ev) {
      if (!lb.dragging) return;
      lb.x = lb.ox + (ev.clientX - lb.px);
      lb.y = lb.oy + (ev.clientY - lb.py);
      applyTransform();
    });
    function endPan() {
      lb.dragging = false;
      stage.classList.remove("is-panning");
    }
    stage.addEventListener("pointerup", endPan);
    stage.addEventListener("pointercancel", endPan);
  }

  document.addEventListener("keydown", function (ev) {
    if (lightbox && !lightbox.hidden) {
      if (ev.key === "Escape") closeLightbox();
      if (ev.key === "+" || ev.key === "=") {
        ev.preventDefault();
        zoomAt(stage.clientWidth / 2, stage.clientHeight / 2, 1.25);
      }
      if (ev.key === "-" || ev.key === "_") {
        ev.preventDefault();
        zoomAt(stage.clientWidth / 2, stage.clientHeight / 2, 0.8);
      }
      if (ev.key === "0") fitContent();
    }
  });
})();
