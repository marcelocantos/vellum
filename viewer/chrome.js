(function () {
  const chrome = document.querySelector(".vellum-chrome");
  if (!chrome) return;

  const source = chrome.getAttribute("data-source") || "";
  const article = chrome.querySelector(".vellum-article");
  const scrollPane = document.getElementById("vellum-scroll");
  const toc = document.getElementById("vellum-toc");
  const tocNav = document.getElementById("vellum-toc-nav");
  const status = document.getElementById("vellum-status");
  const lightbox = document.getElementById("vellum-lightbox");
  const stage = document.getElementById("vellum-lightbox-stage");
  const content = document.getElementById("vellum-lightbox-content");
  const hint = document.getElementById("vellum-lightbox-hint");

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
      if (action === "pdf") {
        window.location.assign(actionURL("pdf"));
        return;
      }
      if (action === "toc-toggle") {
        if (!toc) return;
        toc.classList.toggle("is-collapsed");
        toc.classList.toggle("is-open");
        return;
      }
      if (action === "toc-expand") {
        cancelTOCFill();
        setTOCCollapsed(false);
        return;
      }
      if (action === "toc-collapse") {
        cancelTOCFill();
        setTOCCollapsed(true);
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
  }

  function setTOCCollapsed(collapsed) {
    if (!tocNav) return;
    tocNav.querySelectorAll(".vellum-toc-item").forEach(function (li) {
      setItemCollapsed(li, collapsed);
    });
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
      if (i >= items.length) return;
      setItemCollapsed(items[i], false);
      if (tocOverflows()) {
        setItemCollapsed(items[i], true);
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
