(function () {
  const chrome = document.querySelector(".vellum-chrome");
  if (!chrome) return;

  const source = chrome.getAttribute("data-source") || "";
  // Where the viewer's own routes are, relative to this page. A page shown
  // inside T3 Code is reached through T3's gateway, where an absolute
  // /_vellum/ would address T3, so the server names a relative root.
  const apiRoot = chrome.getAttribute("data-api") || "/_vellum/";
  // An embedded page (data-embedded) is shown in another application's
  // frame: it has no storage, and cannot open windows or download.
  const embedded = chrome.hasAttribute("data-embedded");

  // Storage that outlives a reload. A sandboxed frame has no localStorage
  // or sessionStorage (reading either throws), so there the values are
  // kept as JSON in window.name, which a frame keeps across its own
  // navigations and reloads.
  const NAME_STORE_KEY = "vellum-store";
  function nameStore() {
    function read() {
      try {
        const all = JSON.parse(window.name || "{}");
        const mine = all && all[NAME_STORE_KEY];
        return mine && typeof mine === "object" ? mine : {};
      } catch (e) {
        return {};
      }
    }
    function write(values) {
      const all = {};
      all[NAME_STORE_KEY] = values;
      window.name = JSON.stringify(all);
    }
    return {
      getItem: function (key) {
        const values = read();
        return Object.prototype.hasOwnProperty.call(values, key) ? values[key] : null;
      },
      setItem: function (key, value) {
        const values = read();
        values[key] = String(value);
        write(values);
      },
      removeItem: function (key) {
        const values = read();
        delete values[key];
        write(values);
      },
    };
  }
  function browserStore(name) {
    try {
      const store = window[name];
      store.getItem(NAME_STORE_KEY);
      return store;
    } catch (e) {
      return null;
    }
  }
  const fallbackStore = nameStore();
  const localStore = browserStore("localStorage") || fallbackStore;
  const sessionStore = browserStore("sessionStorage") || fallbackStore;
  const article = chrome.querySelector(".vellum-article");
  const scrollPane = document.getElementById("vellum-scroll");
  const toc = document.getElementById("vellum-toc");
  const tocNav = document.getElementById("vellum-toc-nav");
  const tocCollapseBtn = chrome.querySelector('[data-vellum="toc-collapse"]');
  const tocExpandBtn = chrome.querySelector('[data-vellum="toc-expand"]');
  const tocToggleBtn = chrome.querySelector('[data-vellum="toc-toggle"]');
  const tocSplitter = document.getElementById("vellum-toc-splitter");

  const TOC_WIDTH_KEY = "vellum-toc-width";
  const TOC_WIDTH_DEFAULT = 264;
  const TOC_WIDTH_MIN = 160;
  const TOC_WIDTH_MAX = 560;
  const TOC_WIDTH_STEP = 16;

  function clampTocWidth(px) {
    return Math.min(TOC_WIDTH_MAX, Math.max(TOC_WIDTH_MIN, px));
  }

  function applyTocWidth(px) {
    chrome.style.setProperty("--vellum-toc-width", clampTocWidth(px) + "px");
  }

  function saveTocWidth(px) {
    try {
      localStore.setItem(TOC_WIDTH_KEY, String(clampTocWidth(px)));
    } catch (e) {}
  }

  function loadTocWidth() {
    try {
      const v = parseInt(localStore.getItem(TOC_WIDTH_KEY), 10);
      if (v >= TOC_WIDTH_MIN && v <= TOC_WIDTH_MAX) return v;
    } catch (e) {}
    return TOC_WIDTH_DEFAULT;
  }

  function tocSplitterEnabled() {
    if (!toc || toc.hidden) return false;
    if (toc.classList.contains("is-collapsed")) return false;
    try {
      if (window.matchMedia("(max-width: 880px)").matches) return false;
    } catch (e) {}
    return true;
  }

  function updateTocSplitter() {
    if (!tocSplitter) return;
    const on = tocSplitterEnabled();
    tocSplitter.hidden = !on;
    tocSplitter.setAttribute("aria-hidden", on ? "false" : "true");
  }

  applyTocWidth(loadTocWidth());
  updateTocSplitter();

  function toggleTocSidebar() {
    if (!toc || toc.hidden) return;
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
    updateTocSplitter();
  }

  // On a narrow window the panel lies over the page: a click outside it,
  // a link in it, or Escape closes it.
  function closeTocOverlay() {
    if (!toc || !toc.classList.contains("is-open")) return false;
    toc.classList.remove("is-open");
    updateTocToggleButton();
    return true;
  }

  const status = document.getElementById("vellum-status");
  const lightbox = document.getElementById("vellum-lightbox");
  const stage = document.getElementById("vellum-lightbox-stage");
  const content = document.getElementById("vellum-lightbox-content");
  const zoomLevel = document.getElementById("vellum-lightbox-level");

  // The open editor's handle (see the Editor section); declared here
  // because applyTheme runs during start-up and recolours it.
  let ed = null;

  const THEME_KEY = "vellum-theme";
  const THEME_MODES = ["dark", "system", "light"];

  function systemPrefersDark() {
    try {
      return window.matchMedia("(prefers-color-scheme: dark)").matches;
    } catch (e) {
      return false;
    }
  }

  function themeOrder() {
    return systemPrefersDark()
      ? ["system", "light", "dark"]
      : ["system", "dark", "light"];
  }

  function nextTheme(mode) {
    const order = themeOrder();
    const i = order.indexOf(mode);
    if (i < 0) return order[0];
    return order[(i + 1) % order.length];
  }

  function themeLabel(mode) {
    return mode.charAt(0).toUpperCase() + mode.slice(1);
  }

  function updateThemeButton(mode) {
    const themeBtn = chrome.querySelector('[data-vellum="theme"]');
    if (!themeBtn) return;
    const next = nextTheme(mode);
    themeBtn.title = "Theme: " + themeLabel(mode) + " (click for " + themeLabel(next) + ")";
    themeBtn.setAttribute("aria-label", themeBtn.title);
  }

  function applyTheme(mode) {
    if (THEME_MODES.indexOf(mode) < 0) mode = "system";
    document.documentElement.dataset.vellumTheme = mode;
    try {
      localStore.setItem(THEME_KEY, mode);
    } catch (e) {}
    updateThemeButton(mode);
    if (ed) ed.setDark(effectiveDark());
  }

  function effectiveDark() {
    const mode = document.documentElement.dataset.vellumTheme || "system";
    return mode === "dark" || (mode === "system" && systemPrefersDark());
  }

  function initTheme() {
    let mode = "system";
    try {
      const saved = localStore.getItem(THEME_KEY);
      if (THEME_MODES.indexOf(saved) >= 0) mode = saved;
    } catch (e) {}
    applyTheme(mode);
    try {
      window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", function () {
        updateThemeButton(document.documentElement.dataset.vellumTheme || "system");
        if (ed) ed.setDark(effectiveDark());
      });
    } catch (e) {}
  }

  initTheme();

  function tocSidebarVisible() {
    if (!toc || toc.hidden) return false;
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
    const label = visible ? "Hide contents" : "Show contents";
    tocToggleBtn.title = label;
    tocToggleBtn.setAttribute("aria-label", label);
    tocToggleBtn.setAttribute("aria-expanded", visible ? "true" : "false");
  }

  updateTocToggleButton();
  try {
    window.matchMedia("(max-width: 880px)").addEventListener("change", function () {
      updateTocToggleButton();
      updateTocSplitter();
    });
  } catch (e) {}

  if (tocSplitter) {
    let splitterDrag = false;

    tocSplitter.addEventListener("pointerdown", function (ev) {
      if (!tocSplitterEnabled() || ev.button !== 0) return;
      splitterDrag = true;
      tocSplitter.classList.add("is-dragging");
      tocSplitter.setPointerCapture(ev.pointerId);
      ev.preventDefault();
      applyTocWidth(ev.clientX - chrome.getBoundingClientRect().left);
    });

    tocSplitter.addEventListener("pointermove", function (ev) {
      if (!splitterDrag) return;
      applyTocWidth(ev.clientX - chrome.getBoundingClientRect().left);
    });

    function endSplitterDrag(ev) {
      if (!splitterDrag) return;
      splitterDrag = false;
      tocSplitter.classList.remove("is-dragging");
      const w = ev.clientX - chrome.getBoundingClientRect().left;
      applyTocWidth(w);
      saveTocWidth(w);
      try {
        tocSplitter.releasePointerCapture(ev.pointerId);
      } catch (e) {}
    }

    tocSplitter.addEventListener("pointerup", endSplitterDrag);
    tocSplitter.addEventListener("pointercancel", endSplitterDrag);

    tocSplitter.addEventListener("keydown", function (ev) {
      if (!tocSplitterEnabled()) return;
      let w = loadTocWidth();
      if (ev.key === "ArrowLeft") w -= TOC_WIDTH_STEP;
      else if (ev.key === "ArrowRight") w += TOC_WIDTH_STEP;
      else if (ev.key === "Home") w = TOC_WIDTH_MIN;
      else if (ev.key === "End") w = TOC_WIDTH_MAX;
      else return;
      ev.preventDefault();
      applyTocWidth(w);
      saveTocWidth(w);
    });
  }

  function focusScrollPane() {
    if (scrollPane) scrollPane.focus({ preventScroll: true });
  }

  function scheduleFocusScrollPane() {
    window.setTimeout(function () {
      if (lightbox && !lightbox.hidden) return;
      if (editorIsOpen()) return;
      if (!scrollPane) return;
      const active = document.activeElement;
      if (active === scrollPane || scrollPane.contains(active)) return;
      focusScrollPane();
    }, 0);
  }

  if (scrollPane) {
    scrollPane.addEventListener("focusout", function () {
      if (lightbox && !lightbox.hidden) return;
      if (editorIsOpen()) return;
      scheduleFocusScrollPane();
    });
  }

  function setStatus(msg, ok) {
    if (!status) return;
    status.hidden = !msg;
    status.textContent = msg || "";
    status.classList.toggle("is-error", Boolean(msg) && !ok);
    status.title = msg && !ok ? "Click to dismiss" : "";
    if (msg && ok) {
      window.setTimeout(function () {
        if (status.textContent === msg) {
          status.hidden = true;
        }
      }, 2500);
    }
  }

  if (status) {
    status.addEventListener("click", function () {
      status.hidden = true;
    });
  }

  function actionURL(name) {
    return apiRoot + name + "?path=" + encodeURIComponent(source);
  }

  const SCROLL_KEY = "vellum-scroll:" + source;
  const TASK_CONSENT_PERM = "vellum-task-edit";
  const TASK_CONSENT_SESS = "vellum-task-edit";
  const consentEl = document.getElementById("vellum-consent");
  const goneEl = document.getElementById("vellum-gone");
  const waiting = chrome.getAttribute("data-waiting") === "1";
  let pendingTask = null;
  // The source stamp this page was rendered from (the server puts it on
  // the chrome). A task toggle sends it; a file changed since is refused.
  let loadStamp = chrome.getAttribute("data-stamp") || null;
  let toggleInFlight = 0;
  // Toggles run one at a time, each naming the stamp the previous one
  // left, so two quick clicks never race each other's write.
  let toggleChain = Promise.resolve();
  // A watch stamp that arrived while a toggle was in flight: once the
  // toggles settle, anything but our own write reloads the page.
  let watchedDuringToggle = null;

  function adoptStamp(stamp) {
    if (stamp) loadStamp = stamp;
  }

  // The server ends a write's answer, and a conflict's, with a line
  // "stamp <stamp>". It is read from the body, never from a response
  // header: a proxy between the page and the viewer may drop headers.
  function parseStampLine(text) {
    const lines = String(text || "").replace(/\s+$/, "").split("\n");
    const line = lines[lines.length - 1];
    if (line.indexOf("stamp ") === 0) return line.slice(6);
    return "";
  }

  // A failed answer's text for the reader, without its stamp line.
  function errorText(text, fallback) {
    const lines = String(text || "").replace(/\s+$/, "").split("\n");
    if (lines[lines.length - 1].indexOf("stamp ") === 0) lines.pop();
    return lines.join("\n").trim() || fallback;
  }

  function articleHeadings() {
    if (!article) return [];
    return Array.prototype.slice.call(article.querySelectorAll("h1, h2, h3, h4, h5, h6")).filter(function (h) {
      return !h.closest(".vellum-contents");
    });
  }

  function captureScroll() {
    if (!scrollPane) return null;
    const paneTop = scrollPane.getBoundingClientRect().top;
    const headings = articleHeadings();
    let id = "";
    let offset = 0;
    for (let i = 0; i < headings.length; i++) {
      const top = headings[i].getBoundingClientRect().top;
      if (top <= paneTop + 1) {
        id = headings[i].id || "";
        offset = paneTop - top;
      } else {
        break;
      }
    }
    return { id: id, offset: offset, y: scrollPane.scrollTop };
  }

  function captureFocus() {
    const el = document.activeElement;
    if (!(el instanceof Element) || !article || !article.contains(el)) return null;
    if (el instanceof HTMLInputElement && el.type === "checkbox") {
      const item = el.closest("[data-task-index]");
      const idx = item ? item.getAttribute("data-task-index") : null;
      if (idx !== null && idx !== "") return { taskIndex: idx };
    }
    if (el.id) return { id: el.id };
    return null;
  }

  function persistScroll() {
    try {
      const pos = captureScroll() || {};
      const focus = captureFocus();
      if (focus) pos.focus = focus;
      if (pos.focus || pos.id || typeof pos.y === "number") {
        sessionStore.setItem(SCROLL_KEY, JSON.stringify(pos));
      }
    } catch (e) {}
  }

  function restoreFocus(focus) {
    if (!focus || !article) return;
    let el = null;
    if (focus.taskIndex != null && focus.taskIndex !== "") {
      el = article.querySelector(
        '[data-task-index="' + CSS.escape(String(focus.taskIndex)) + '"] input[type=checkbox]'
      );
    } else if (focus.id) {
      el = document.getElementById(focus.id);
    }
    if (el && article.contains(el) && typeof el.focus === "function") el.focus();
  }

  function restoreScroll() {
    if (!scrollPane) return;
    let raw;
    try {
      raw = sessionStore.getItem(SCROLL_KEY);
    } catch (e) {
      return;
    }
    if (!raw) return;
    let pos;
    try {
      pos = JSON.parse(raw);
    } catch (e) {
      return;
    }
    try {
      sessionStore.removeItem(SCROLL_KEY);
    } catch (e) {}
    function apply() {
      if (typeof pos.sourceLine === "number") {
        scrollViewToSourceLine(pos.sourceLine, pos.sourceLines || 0);
        restoreFocus(pos.focus);
        return;
      }
      const max = Math.max(0, scrollPane.scrollHeight - scrollPane.clientHeight);
      if (pos.id && article) {
        const el = document.getElementById(pos.id);
        if (el && article.contains(el)) {
          const paneTop = scrollPane.getBoundingClientRect().top;
          const headingTop = el.getBoundingClientRect().top;
          const delta = headingTop - paneTop;
          const next = scrollPane.scrollTop + delta + (pos.offset || 0);
          scrollPane.scrollTop = Math.max(0, Math.min(max, next));
          restoreFocus(pos.focus);
          return;
        }
      }
      if (typeof pos.y === "number") {
        scrollPane.scrollTop = Math.max(0, Math.min(max, pos.y));
      }
      restoreFocus(pos.focus);
    }
    requestAnimationFrame(function () {
      requestAnimationFrame(apply);
    });
  }

  function reloadView() {
    persistScroll();
    location.reload();
  }

  function taskConsent() {
    try {
      if (localStore.getItem(TASK_CONSENT_PERM) === "1") return "always";
      if (sessionStore.getItem(TASK_CONSENT_SESS) === "1") return "session";
    } catch (e) {}
    return "";
  }

  function rememberConsent(kind) {
    try {
      if (kind === "always") localStore.setItem(TASK_CONSENT_PERM, "1");
      else sessionStore.setItem(TASK_CONSENT_SESS, "1");
    } catch (e) {}
  }

  function hideConsent() {
    if (consentEl) consentEl.hidden = true;
  }

  function showGone() {
    if (goneEl) goneEl.hidden = false;
  }

  function hideGone() {
    if (goneEl) goneEl.hidden = true;
  }

  function unescapePathURL(urlPath) {
    return String(urlPath || "")
      .split("/")
      .map(function (seg) {
        try {
          return decodeURIComponent(seg);
        } catch (e) {
          return seg;
        }
      })
      .join("/");
  }

  // The page for the document at urlPath (a path as the server writes it
  // in a URL). An embedded page names a document by one base64url segment
  // of that path, because its own URL is not the document's path.
  function documentURL(urlPath) {
    const docs = chrome.getAttribute("data-docs");
    if (!docs) return urlPath;
    const bytes = new TextEncoder().encode(unescapePathURL(urlPath));
    let bin = "";
    bytes.forEach(function (b) {
      bin += String.fromCharCode(b);
    });
    return docs + btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  function showConsent() {
    if (consentEl) consentEl.hidden = false;
  }

  // index and line name the task (the list item carries both); wasChecked
  // is the state the reader saw before clicking.
  function postTaskToggle(index, line, wasChecked) {
    const u =
      actionURL("task-toggle") +
      "&index=" + encodeURIComponent(String(index)) +
      "&line=" + encodeURIComponent(String(line)) +
      "&checked=" + (wasChecked ? "1" : "0") +
      "&stamp=" + encodeURIComponent(loadStamp || "");
    return fetch(u, { method: "POST" }).then(function (resp) {
      return resp.text().then(function (text) {
        if (!resp.ok) {
          throw new Error(errorText(text, resp.statusText));
        }
        return text;
      });
    });
  }

  function applyTaskToggle(input) {
    const item = input.closest("[data-task-index]");
    if (!item) return;
    const index = parseInt(item.getAttribute("data-task-index"), 10);
    const line = parseInt(item.getAttribute("data-task-line"), 10);
    if (isNaN(index) || isNaN(line)) return;
    // The box already shows the new state; the reader saw the other one.
    const wasChecked = !input.checked;
    // Record focus before the write. Watch will reload on the new stamp;
    // restoreScroll puts the same checkbox back under the keyboard.
    persistScroll();
    toggleInFlight++;
    toggleChain = toggleChain
      .then(function () {
        return postTaskToggle(index, line, wasChecked);
      })
      .then(function (text) {
        adoptStamp(parseStampLine(text));
      })
      .catch(function (err) {
        input.checked = wasChecked;
        setStatus(err.message || String(err), false);
      })
      .then(function () {
        toggleInFlight--;
        settleToggles();
      });
  }

  // After the last toggle, a watch stamp that is not our own write means
  // the file changed underneath: show it.
  function settleToggles() {
    if (toggleInFlight > 0 || watchedDuringToggle === null) return;
    const seen = watchedDuringToggle;
    watchedDuringToggle = null;
    if (seen !== loadStamp && !editorIsOpen()) reloadView();
  }

  function watchURL() {
    const u = new URL(actionURL("watch"), location.href);
    u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
    return u.href;
  }

  function startWatch() {
    if (!source || !window.WebSocket) return;
    let delay = 1000;
    let closed = false;

    function connect() {
      if (closed) return;
      let ws;
      try {
        ws = new WebSocket(watchURL());
      } catch (e) {
        scheduleReconnect();
        return;
      }
      ws.onopen = function () {
        delay = 1000;
      };
      ws.onmessage = function (ev) {
        const text = String(ev.data || "").replace(/\s+$/, "");
        if (text.indexOf("error ") === 0) {
          setStatus(text.slice(6) || "source unreadable", false);
          return;
        }
        if (text === "delete") {
          if (waiting) return;
          closed = true;
          try {
            ws.close();
          } catch (e) {}
          showGone();
          return;
        }
        if (text.indexOf("rename ") === 0) {
          closed = true;
          try {
            ws.close();
          } catch (e) {}
          const urlPath = text.slice(7);
          persistScroll();
          try {
            const raw = sessionStore.getItem(SCROLL_KEY);
            if (raw) sessionStore.setItem("vellum-scroll:" + unescapePathURL(urlPath), raw);
          } catch (e) {}
          location.replace(documentURL(urlPath));
          return;
        }
        if (text.indexOf("stamp ") !== 0) return;
        const stamp = text.slice(6);
        if (waiting) {
          closed = true;
          try {
            ws.close();
          } catch (e) {}
          reloadView();
          return;
        }
        if (loadStamp === null) {
          loadStamp = stamp;
          return;
        }
        if (editorIsOpen()) {
          editorNoticeStamp(stamp);
          return;
        }
        if (stamp === loadStamp) return;
        if (toggleInFlight > 0) {
          watchedDuringToggle = stamp;
          return;
        }
        reloadView();
      };
      ws.onclose = function () {
        scheduleReconnect();
      };
      ws.onerror = function () {
        try {
          ws.close();
        } catch (e) {}
      };
    }

    function scheduleReconnect() {
      if (closed) return;
      window.setTimeout(connect, delay);
      if (delay === 1000) delay = 2000;
      else if (delay === 2000) delay = 5000;
      else delay = 15000;
    }

    connect();
  }

  chrome.querySelectorAll("[data-vellum]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      const action = btn.getAttribute("data-vellum");
      if (action === "theme") {
        applyTheme(nextTheme(document.documentElement.dataset.vellumTheme || "system"));
        return;
      }
      if (action === "edit") {
        openEditor();
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
      if (action === "toc-close") {
        closeTocOverlay();
        return;
      }
      if (action === "top") {
        if (scrollPane) scrollPane.scrollTop = 0;
        focusScrollPane();
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
      if (action === "consent-session" || action === "consent-always") {
        rememberConsent(action === "consent-always" ? "always" : "session");
        hideConsent();
        if (pendingTask) {
          const pending = pendingTask;
          pendingTask = null;
          pending.input.checked = pending.checked;
          applyTaskToggle(pending.input);
        }
        return;
      }
      if (action === "consent-cancel") {
        hideConsent();
        if (pendingTask) {
          pendingTask.input.checked = !pendingTask.checked;
        }
        pendingTask = null;
        return;
      }
      if (action === "gone-dismiss") {
        hideGone();
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
            setStatus(
              action === "clipboard"
                ? "Copied to clipboard"
                : "Revealed in " + (btn.getAttribute("data-file-manager") || "the file manager"),
              true
            );
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

  // The bar names the file. Once the document's title (its first h1)
  // has scrolled out of the pane, the bar shows the title instead.
  function trackTitle() {
    const label = chrome.querySelector(".vellum-doc");
    const titleEl = chrome.querySelector(".vellum-doctitle");
    if (!label || !titleEl || !article || !scrollPane) return;
    const h1 = articleHeadings().find(function (h) {
      return h.tagName === "H1";
    });
    const text = h1 ? (h1.textContent || "").trim() : "";
    if (!text) return;
    titleEl.textContent = text;
    let scheduled = false;
    function apply() {
      scheduled = false;
      const gone = h1.getBoundingClientRect().bottom <= scrollPane.getBoundingClientRect().top;
      label.classList.toggle("is-titled", gone);
    }
    function schedule() {
      if (scheduled) return;
      scheduled = true;
      requestAnimationFrame(apply);
    }
    scrollPane.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    apply();
  }

  // Tables scroll horizontally themselves, so CSS sticky would attach to
  // that scroller instead of the document pane. Move the real header as a
  // unit: column widths, multi-row headings and horizontal scroll stay intact.
  if (article && scrollPane) {
    let pending = false;
    function pinTableHeads() {
      pending = false;
      const paneTop = scrollPane.getBoundingClientRect().top;
      const top = paneTop;
      article.querySelectorAll("table").forEach(function (table) {
        const head = table.tHead;
        if (!head) return;
        const previous = Number(head.dataset.vellumPinOffset || 0);
        const rect = head.getBoundingClientRect();
        const restTop = rect.top - previous;
        const offset = Math.max(0, Math.min(top - restTop,
          table.getBoundingClientRect().bottom - restTop - rect.height));
        head.dataset.vellumPinOffset = offset;
        head.style.setProperty("--vellum-table-pin", offset + "px");
      });
    }
    function scheduleTableHeads() {
      if (pending) return;
      pending = true;
      requestAnimationFrame(pinTableHeads);
    }
    scrollPane.addEventListener("scroll", scheduleTableHeads, { passive: true });
    new ResizeObserver(scheduleTableHeads).observe(article);
    window.addEventListener("resize", scheduleTableHeads);
    scheduleTableHeads();
  }

  // Heading element to its link in the contents panel.
  const tocLinks = new Map();

  function buildTOC() {
    if (!article || !tocNav || !toc) return;
    const headings = Array.prototype.slice.call(
      article.querySelectorAll("h1, h2, h3, h4, h5, h6")
    ).filter(function (h) {
      return !h.closest(".vellum-contents");
    });
    if (!headings.length) {
      toc.hidden = true;
      if (tocToggleBtn) tocToggleBtn.hidden = true;
      updateTocSplitter();
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
      twist.hidden = true;
      const a = document.createElement("a");
      // Fragment-only hrefs resolve against <base> (the source directory),
      // which would navigate off the Markdown page.
      a.href = location.pathname + location.search + "#" + encodeURIComponent(id);
      a.textContent = (h.textContent || "").trim() || id;
      a.addEventListener("click", function () {
        holdActiveHeading(h);
        closeTocOverlay();
      });
      tocLinks.set(h, a);
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
    updateTocSplitter();
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
    if (twist) twist.setAttribute("aria-expanded", collapsed ? "false" : "true");
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

  // Collapse-all is useful only when the tree is still more open than the
  // Lmin state: every ancestor of the first multi-item level is expanded,
  // and at least one expandable Lmin item is expanded. If any ancestor is
  // already collapsed (manually), or every Lmin item is collapsed, the
  // button would not collapse further.
  function tocCanCollapseFurther() {
    if (!tocNav) return false;
    const branchOl = findBranchOl();
    if (!branchOl) {
      const items = tocNav.querySelectorAll(".vellum-toc-item");
      for (let i = 0; i < items.length; i++) {
        const li = items[i];
        if (!li.querySelector(":scope > .vellum-toc-kids")) continue;
        if (!li.classList.contains("is-collapsed")) return true;
      }
      return false;
    }
    let node = branchOl.parentElement;
    while (node && tocNav.contains(node)) {
      if (node.classList && node.classList.contains("vellum-toc-item")) {
        if (node.classList.contains("is-collapsed")) return false;
      }
      node = node.parentElement;
    }
    const lminItems = tocItemsIn(branchOl);
    for (let i = 0; i < lminItems.length; i++) {
      const li = lminItems[i];
      if (!li.querySelector(":scope > .vellum-toc-kids")) continue;
      if (!li.classList.contains("is-collapsed")) return true;
    }
    return false;
  }

  function updateTOCActionButtons() {
    if (tocExpandBtn) tocExpandBtn.disabled = tocFullyExpanded();
    if (tocCollapseBtn) tocCollapseBtn.disabled = !tocCanCollapseFurther();
    scheduleActiveHeading();
  }

  // The contents panel marks the section being read: the last heading at
  // or above the top of the pane (the first one before any is reached,
  // the last one at the end of the document). When that heading's link is
  // inside a collapsed section, the section's own link is marked.
  const ACTIVE_HEADING_SLACK = 24;
  // After a click in the panel the clicked heading stays marked for this
  // long, so a short last section the pane cannot scroll to its top is
  // still the one shown.
  const ACTIVE_HEADING_HOLD_MS = 600;
  let activeLink = null;
  let activeHold = 0;
  let activeScheduled = false;

  function markActiveHeading(h) {
    let link = h ? tocLinks.get(h) : null;
    if (link) {
      for (let li = link.closest(".vellum-toc-item"); li; li = li.parentElement.closest(".vellum-toc-item")) {
        const up = li.parentElement.closest(".vellum-toc-item");
        if (up && up.classList.contains("is-collapsed")) {
          link = up.querySelector(":scope > .vellum-toc-row > a");
        }
      }
    }
    if (link === activeLink) return;
    if (activeLink) {
      activeLink.classList.remove("vellum-toc-active");
      activeLink.removeAttribute("aria-current");
    }
    activeLink = link;
    if (!link) return;
    link.classList.add("vellum-toc-active");
    link.setAttribute("aria-current", "location");
    // Keep it in view without moving the page.
    const r = link.getBoundingClientRect();
    const t = toc.getBoundingClientRect();
    if (!t.height) return;
    const margin = 8;
    if (r.top < t.top + margin) toc.scrollTop -= t.top + margin - r.top;
    else if (r.bottom > t.bottom - margin) toc.scrollTop += r.bottom - t.bottom + margin;
  }

  function updateActiveHeading() {
    activeScheduled = false;
    if (!scrollPane || !toc || Date.now() < activeHold) return;
    const headings = Array.from(tocLinks.keys());
    if (!headings.length) return;
    let current = headings[0];
    if (scrollPane.scrollTop + scrollPane.clientHeight >= scrollPane.scrollHeight - 2 && scrollPane.scrollTop > 0) {
      current = headings[headings.length - 1];
    } else {
      const line = scrollPane.getBoundingClientRect().top + ACTIVE_HEADING_SLACK;
      for (let i = 0; i < headings.length; i++) {
        if (headings[i].getBoundingClientRect().top > line) break;
        current = headings[i];
      }
    }
    markActiveHeading(current);
  }

  function scheduleActiveHeading() {
    if (activeScheduled) return;
    activeScheduled = true;
    requestAnimationFrame(updateActiveHeading);
  }

  function holdActiveHeading(h) {
    activeHold = Date.now() + ACTIVE_HEADING_HOLD_MS;
    markActiveHeading(h);
  }

  if (scrollPane) {
    scrollPane.addEventListener("scroll", scheduleActiveHeading, { passive: true });
    window.addEventListener("resize", scheduleActiveHeading);
  }

  function setTOCCollapsed(collapsed) {
    if (!tocNav) return;
    tocNav.querySelectorAll(".vellum-toc-item").forEach(function (li) {
      const kids = li.querySelector(":scope > .vellum-toc-kids");
      if (!kids) return;
      li.classList.toggle("is-collapsed", collapsed);
      const twist = li.querySelector(":scope > .vellum-toc-row > .vellum-toc-twist");
      if (twist) twist.setAttribute("aria-expanded", collapsed ? "false" : "true");
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

  // Mermaid diagrams and math reach the page as placeholders; the server
  // runs mmdc / KaTeX for one only when asked. Fetch each as it nears the
  // viewport (or on click) so first paint never waits on a subprocess.
  // A transport failure (daemon restarting, connection dropped) retries
  // with backoff; a render failure or a stale id shows the fragment's
  // source with the error and a Retry button, never a half state.
  function hydrateDeferred() {
    if (!article) return;
    const nodes = Array.prototype.slice.call(
      article.querySelectorAll(".vellum-deferred[data-vellum-fragment]")
    );
    if (!nodes.length) return;

    const RETRY_DELAYS_MS = [1000, 3000];
    const MESSAGE_MAX = 240;

    function isMermaid(el) {
      return el.classList.contains("vellum-deferred-mermaid");
    }

    function firstLine(text) {
      const line = String(text || "").trim().split("\n")[0];
      return line.length > MESSAGE_MAX ? line.slice(0, MESSAGE_MAX) + "…" : line;
    }

    function clearNote(el) {
      const note = el.querySelector(".vellum-deferred-note");
      if (note) note.remove();
    }

    function markLoading(el) {
      clearNote(el);
      el.classList.remove("is-failed");
      el.classList.add("is-loading");
      el.setAttribute("aria-busy", "true");
      if (isMermaid(el)) el.setAttribute("aria-label", "Diagram, loading");
    }

    function markFailed(el, message) {
      el.classList.remove("is-loading");
      el.classList.add("is-failed");
      el.removeAttribute("aria-busy");
      clearNote(el);
      const label = (isMermaid(el) ? "Diagram failed to render" : "Math failed to render") + ": " + message;
      const note = document.createElement("span");
      note.className = "vellum-deferred-note";
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "vellum-deferred-retry";
      btn.title = label;
      btn.setAttribute("aria-label", label + ". Retry");
      if (isMermaid(el)) {
        const text = document.createElement("span");
        text.textContent = label + ". ";
        note.appendChild(text);
        btn.textContent = "Retry";
        el.setAttribute("aria-label", label);
      } else {
        btn.textContent = "↻";
        el.title = label;
      }
      btn.addEventListener("click", function (ev) {
        ev.preventDefault();
        ev.stopPropagation();
        load(el, 0);
      });
      note.appendChild(btn);
      el.insertBefore(note, el.firstChild);
    }

    function load(el, attempt) {
      if (el.getAttribute("data-vellum-loading") === "1") return;
      el.setAttribute("data-vellum-loading", "1");
      markLoading(el);
      const id = el.getAttribute("data-vellum-fragment") || "";
      fetch(actionURL("fragment") + "&id=" + encodeURIComponent(id))
        .then(function (resp) {
          return resp.text().then(function (text) {
            if (resp.ok) return text;
            const err = new Error(
              resp.status === 404
                ? "no longer in the document (reload to refresh)"
                : firstLine(text) || "HTTP " + resp.status
            );
            // Gateway-style statuses mean the daemon is not there to
            // answer; anything else is a definitive answer.
            err.transient = resp.status === 502 || resp.status === 503 || resp.status === 504;
            throw err;
          });
        })
        .then(function (text) {
          const tpl = document.createElement("template");
          tpl.innerHTML = text;
          const node = tpl.content.firstElementChild;
          if (!node) throw new Error("empty fragment");
          el.replaceWith(node);
        })
        .catch(function (err) {
          el.removeAttribute("data-vellum-loading");
          // fetch rejects with a TypeError when the connection fails.
          const transient = Boolean(err && (err.transient || err instanceof TypeError));
          if (transient && attempt < RETRY_DELAYS_MS.length) {
            window.setTimeout(function () {
              if (document.contains(el)) load(el, attempt + 1);
            }, RETRY_DELAYS_MS[attempt]);
            return;
          }
          const message = transient
            ? "the view server did not answer (" + (err && err.message ? err.message : err) + ")"
            : err && err.message
              ? err.message
              : String(err);
          markFailed(el, message);
        });
    }

    if (typeof IntersectionObserver !== "function") {
      nodes.forEach(function (n) {
        load(n, 0);
      });
      return;
    }
    const io = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (entry) {
          if (!entry.isIntersecting) return;
          io.unobserve(entry.target);
          load(entry.target, 0);
        });
      },
      { root: scrollPane || null, rootMargin: "1200px 0px" }
    );
    nodes.forEach(function (n) {
      io.observe(n);
    });
    article.addEventListener("click", function (ev) {
      const t = ev.target;
      if (!(t instanceof Element)) return;
      const el = t.closest(".vellum-deferred");
      if (el && article.contains(el) && !el.classList.contains("is-failed")) {
        ev.preventDefault();
        io.unobserve(el);
        load(el, 0);
      }
    });
  }

  // --- Editor ---------------------------------------------------------
  // A modal CodeMirror editor over the Markdown source. The bundle
  // (/_vellum/editor/) loads on first use. Saves are PUTs naming the
  // stamp the buffer was read at; the server answers 409 when the disk
  // moved on, and the user chooses to overwrite or reload. While the
  // editor is open the page does not live-reload; closing it reloads
  // once if anything was written, scrolled to the section the cursor
  // was in.
  const editorRoot = document.getElementById("vellum-editor");
  const editorHost = document.getElementById("vellum-editor-host");
  const editorBanner = document.getElementById("vellum-editor-banner");
  const editorStateEl = document.getElementById("vellum-editor-state");
  const editorTitle = document.getElementById("vellum-editor-title");
  const editorSaveBtn = editorRoot ? editorRoot.querySelector('[data-vellum="ed-save"]') : null;
  const editorSrc = chrome.getAttribute("data-editor") || "";
  const editBtn = chrome.querySelector('[data-vellum="edit"]');
  let editorModule = null;
  let edStamp = "";
  let edSaving = 0;
  let viewStale = false;
  let edOpened = null;

  function editorIsOpen() {
    return Boolean(editorRoot && !editorRoot.hidden);
  }

  function editURL() {
    return actionURL("edit");
  }

  // The source arrives after a first line "stamp <stamp>".
  function fetchSource() {
    return fetch(editURL() + "&stampline=1", { cache: "no-store" }).then(function (resp) {
      return resp.text().then(function (text) {
        if (!resp.ok) throw new Error(text.trim() || resp.statusText);
        const nl = text.indexOf("\n");
        if (text.indexOf("stamp ") !== 0 || nl < 0) throw new Error("the viewer sent no stamp for this file");
        return { text: text.slice(nl + 1), stamp: text.slice(6, nl) };
      });
    });
  }

  function setEditorState(msg, kind) {
    if (!editorStateEl) return;
    editorStateEl.textContent = msg || "";
    editorStateEl.dataset.kind = kind || "";
  }

  function showEditorBanner(message, actions) {
    if (!editorBanner) return;
    const text = document.createElement("span");
    text.textContent = message;
    const row = document.createElement("span");
    row.className = "vellum-editor-banner-actions";
    actions.forEach(function (a) {
      const b = document.createElement("button");
      b.type = "button";
      b.textContent = a.label;
      b.className = a.primary ? "vellum-tbtn is-primary" : "vellum-tbtn";
      b.addEventListener("click", a.run);
      row.appendChild(b);
    });
    editorBanner.replaceChildren(text, row);
    editorBanner.hidden = false;
    const first = row.querySelector("button");
    if (first) first.focus();
  }

  function hideEditorBanner() {
    if (!editorBanner) return;
    editorBanner.hidden = true;
    editorBanner.replaceChildren();
  }

  function onEditorDirty(dirty) {
    if (editorSaveBtn) editorSaveBtn.disabled = !dirty;
    if (dirty) setEditorState("Unsaved changes", "dirty");
    else if (editorStateEl && editorStateEl.dataset.kind === "dirty") setEditorState("", "");
  }

  // Scroll mapping between the page and the source. The server tags
  // rendered blocks with data-source-line and data-source-end (the
  // block's first and last source lines). A block's top is at its first
  // line and its bottom at the start of the line after its last; between
  // those points position is interpolated linearly in both directions.
  // That also covers untagged blocks (fenced code, diagrams, math), the
  // blank lines between blocks, and long wrapped paragraphs.
  //
  // Anchors are [{y, line}] in scroll-content coordinates, sorted by y,
  // with line non-decreasing (a point that would go backwards is
  // dropped). Skipped: footnotes (rendered at the end, defined
  // anywhere), the generated contents block and table internals (a
  // table's head moves while it is pinned).
  function sourceAnchors(totalLines) {
    const pts = [{ y: 0, line: 1 }];
    if (article && scrollPane) {
      const origin = scrollPane.getBoundingClientRect().top - scrollPane.scrollTop;
      article.querySelectorAll("[data-source-line]").forEach(function (el) {
        if (el.closest(".footnotes, .vellum-contents")) return;
        const table = el.closest("table");
        if (table && table !== el) return;
        const first = parseInt(el.getAttribute("data-source-line"), 10);
        const last = parseInt(el.getAttribute("data-source-end"), 10);
        if (!(first > 0)) return;
        const r = el.getBoundingClientRect();
        if (!r.height) return;
        pts.push({ y: r.top - origin, line: first });
        if (last >= first) pts.push({ y: r.bottom - origin, line: last + 1 });
      });
      pts.push({ y: scrollPane.scrollHeight, line: Math.max(totalLines, 1) + 1 });
    }
    pts.sort(function (a, b) {
      return a.y - b.y || a.line - b.line;
    });
    const out = [];
    pts.forEach(function (p) {
      if (out.length && p.line < out[out.length - 1].line) return;
      out.push(p);
    });
    return out;
  }

  function mapAnchors(anchors, value, from, to) {
    for (let i = anchors.length - 1; i >= 0; i--) {
      const a = anchors[i];
      if (a[from] > value) continue;
      const b = anchors[i + 1];
      if (!b || b[from] === a[from]) return a[to];
      return a[to] + ((value - a[from]) / (b[from] - a[from])) * (b[to] - a[to]);
    }
    return anchors[0][to];
  }

  // The (fractional) source line at the top of what the reader sees.
  function viewSourceLine(totalLines) {
    if (!scrollPane || scrollPane.scrollTop < 1) return 1;
    return mapAnchors(sourceAnchors(totalLines), scrollPane.scrollTop, "y", "line");
  }

  // Scroll the page so source line (fractional) is at the top of what
  // the reader sees.
  function scrollViewToSourceLine(line, totalLines) {
    if (!scrollPane) return;
    if (!(line > 1)) {
      scrollPane.scrollTop = 0;
      return;
    }
    scrollPane.scrollTop = mapAnchors(sourceAnchors(totalLines), line, "line", "y");
  }

  // Carrying a page selection into the editor. The page shows rendered
  // text, the source has Markdown around it (emphasis marks, link URLs,
  // tags, entities, list and task markers, fences), so the selection is
  // matched by its words, in order, allowing anything between them that
  // renders as nothing or as punctuation. The search runs near the
  // source lines of the blocks the selection starts and ends in. Only
  // the first and last few words are matched: that keeps the regex
  // cheap, and lets a selection run through rendered math, whose text is
  // not in the source.
  const SEL_WORD_RE = /[\p{L}\p{N}_]+/gu;
  const SEL_EDGE_WORDS = 8;
  // Lines searched around a tagged block's span, and around a position
  // interpolated for an untagged one (fenced code, diagrams).
  const SEL_TAGGED_MARGIN = 2;
  const SEL_MAPPED_MARGIN = 12;
  const SEL_MAX_CANDIDATES = 200;
  // How long a selection captured when the Edit button is pressed stays
  // usable, in case the click itself disturbs the page selection.
  const SEL_PRESS_TTL_MS = 2000;
  const SEL_GAP =
    "(?:" +
    [
      "&#?\\w+;", // entity
      "!\\[[^\\]\\n]*\\]\\([^)\\n]*\\)", // image (alt text is not selectable)
      "\\]\\([^)\\n]*\\)", // the URL half of a link
      "<[^>\\n]*>", // inline HTML tag
      "\\[[ xX]\\][ \\t]", // task marker
      "\\n[ \\t>]*\\d+[.)][ \\t]+", // ordered list marker
      "\\n[ \\t>]*(?:`{3,}|~{3,})[^\\n]*", // code fence line
      "[^\\p{L}\\p{N}_]", // any other non-word character
    ].join("|") +
    ")*?";
  let pressedSelection = null;

  function selectionPattern(words) {
    return new RegExp(
      words
        .map(function (w) {
          return w.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
        })
        .join(SEL_GAP),
      "gu"
    );
  }

  // One end of the page selection: its tagged block's source span and the
  // number of words before the point within that block, or failing a
  // tagged block, its y in scroll-content coordinates.
  function selectionEdge(node, offset) {
    const el = node.nodeType === Node.TEXT_NODE ? node.parentElement : node;
    const block = el ? el.closest("[data-source-line]") : null;
    if (block && article.contains(block)) {
      const before = document.createRange();
      before.setStart(block, 0);
      before.setEnd(node, offset);
      return {
        first: parseInt(block.getAttribute("data-source-line"), 10) || 0,
        last: parseInt(block.getAttribute("data-source-end"), 10) || 0,
        wordsBefore: (before.toString().match(SEL_WORD_RE) || []).length,
      };
    }
    const point = document.createRange();
    point.setStart(node, offset);
    point.collapse(true);
    let rect = point.getBoundingClientRect();
    if (!rect.top && !rect.height && el) rect = el.getBoundingClientRect();
    const origin = scrollPane.getBoundingClientRect().top - scrollPane.scrollTop;
    return { first: 0, last: 0, wordsBefore: -1, y: rect.top - origin };
  }

  function captureViewSelection() {
    if (!article || !scrollPane || !window.getSelection) return null;
    const sel = window.getSelection();
    if (!sel || sel.rangeCount === 0 || sel.isCollapsed) return null;
    const range = sel.getRangeAt(0);
    if (!article.contains(range.commonAncestorContainer)) return null;
    const words = range.toString().match(SEL_WORD_RE);
    if (!words) return null;
    return {
      words: words,
      start: selectionEdge(range.startContainer, range.startOffset),
      end: selectionEdge(range.endContainer, range.endOffset),
      at: Date.now(),
    };
  }

  // The source range [from, to) for a captured selection, as offsets into
  // text with "\n" line endings (the editor's buffer), or null.
  function findSourceSelection(text, cap, totalLines) {
    const src = String(text).replace(/\r\n?/g, "\n");
    const lineStarts = [0];
    for (let i = 0; i < src.length; i++) if (src.charCodeAt(i) === 10) lineStarts.push(i + 1);
    const lineOffset = function (n) {
      return lineStarts[Math.max(1, Math.min(lineStarts.length, n)) - 1];
    };
    const lineEnd = function (n) {
      return n >= lineStarts.length ? src.length : lineStarts[Math.max(1, n)];
    };
    const lineOf = function (offset) {
      let lo = 0;
      let hi = lineStarts.length - 1;
      while (lo < hi) {
        const mid = (lo + hi + 1) >> 1;
        if (lineStarts[mid] <= offset) lo = mid;
        else hi = mid - 1;
      }
      return lo + 1;
    };
    let anchors = null;
    function span(edge) {
      if (edge.first > 0) {
        return { from: edge.first - SEL_TAGGED_MARGIN, to: Math.max(edge.last, edge.first) + SEL_TAGGED_MARGIN, at: edge.first };
      }
      if (!anchors) anchors = sourceAnchors(totalLines);
      const line = Math.floor(mapAnchors(anchors, edge.y, "y", "line"));
      return { from: line - SEL_MAPPED_MARGIN, to: line + SEL_MAPPED_MARGIN, at: line };
    }
    // The candidate closest to the edge: by words before it within the
    // block when the block is tagged, else by line.
    function best(re, from, to, edge, at, useEnd) {
      let pick = null;
      let pickScore = Infinity;
      re.lastIndex = from;
      for (let n = 0; n < SEL_MAX_CANDIDATES; n++) {
        const m = re.exec(src);
        if (!m || m.index >= to) break;
        const hit = { from: m.index, to: m.index + m[0].length };
        const point = useEnd ? hit.to : hit.from;
        const score =
          edge && edge.wordsBefore >= 0
            ? Math.abs((src.slice(lineOffset(edge.first), point).match(SEL_WORD_RE) || []).length - edge.wordsBefore)
            : Math.abs(lineOf(point) - at);
        if (score < pickScore) {
          pick = hit;
          pickScore = score;
        }
        if (m[0].length === 0) re.lastIndex++;
      }
      return pick;
    }
    const s = span(cap.start);
    const headRe = selectionPattern(cap.words.slice(0, SEL_EDGE_WORDS));
    const head =
      best(headRe, lineOffset(s.from), lineEnd(s.to), cap.start, s.at, false) ||
      best(headRe, 0, src.length, null, s.at, false);
    if (!head) return null;
    if (cap.words.length <= SEL_EDGE_WORDS) return head;
    const e = span(cap.end);
    const tailRe = selectionPattern(cap.words.slice(-SEL_EDGE_WORDS));
    let tail = best(tailRe, Math.max(head.from, lineOffset(e.from)), lineEnd(e.to), cap.end, e.at, true);
    if (!tail || tail.to < head.to) tail = best(tailRe, head.from, src.length, null, e.at, true);
    if (!tail || tail.to < head.to) return head;
    return { from: head.from, to: tail.to };
  }

  if (editBtn) {
    editBtn.addEventListener("pointerdown", function () {
      pressedSelection = captureViewSelection();
    });
  }

  function openEditor() {
    if (!editorRoot || !editorHost || !editorSrc || editorIsOpen()) return;
    // Read before the modal takes focus.
    let selection = captureViewSelection();
    if (!selection && pressedSelection && Date.now() - pressedSelection.at < SEL_PRESS_TTL_MS) {
      selection = pressedSelection;
    }
    pressedSelection = null;
    editorRoot.hidden = false;
    if (editBtn) editBtn.setAttribute("aria-expanded", "true");
    if (editorTitle) editorTitle.textContent = source.split(/[\\/]/).pop();
    if (editorSaveBtn) editorSaveBtn.disabled = true;
    hideEditorBanner();
    setEditorState("Loading…", "");
    if (!editorModule) editorModule = import(editorSrc);
    Promise.all([editorModule, fetchSource()])
      .then(function (r) {
        if (!editorIsOpen() || ed) return;
        const mod = r[0];
        const src = r[1];
        edStamp = src.stamp;
        ed = mod.createEditor({
          parent: editorHost,
          doc: src.text,
          dark: effectiveDark(),
          onSave: saveEditor,
          onClose: closeEditor,
          onDirty: onEditorDirty,
        });
        const totalLines = src.text.split(/\r\n|\r|\n/).length;
        ed.scrollToLine(viewSourceLine(totalLines));
        const found = selection ? findSourceSelection(src.text, selection, totalLines) : null;
        if (found) ed.select(found.from, found.to);
        // Where the editor settled (after the cursor snaps to its visual
        // line). A close from here, unmoved, returns the page to exactly
        // where it was instead of mapping back: the two sides wrap at
        // different widths, so a round trip would drift by a line.
        const handle = ed;
        const pageTop = scrollPane ? scrollPane.scrollTop : 0;
        requestAnimationFrame(function () {
          requestAnimationFrame(function () {
            if (ed === handle) edOpened = { line: handle.topLine(), pageTop: pageTop };
          });
        });
        setEditorState("", "");
        ed.focus();
      })
      .catch(function (err) {
        // A failed import is cached by the browser per URL; forget the
        // promise so the next attempt retries.
        editorModule = null;
        setEditorState("", "");
        showEditorBanner("Could not open the editor: " + (err && err.message ? err.message : err), [
          { label: "Close", run: finishCloseEditor },
        ]);
      });
  }

  function saveEditor() {
    if (!ed) return Promise.resolve(false);
    if (edSaving) return Promise.resolve(false);
    const handle = ed;
    const snap = handle.snapshot();
    const headers = { "Content-Type": "text/markdown; charset=utf-8" };
    edSaving++;
    setEditorState("Saving…", "");
    return fetch(editURL() + "&stamp=" + encodeURIComponent(edStamp), {
      method: "PUT",
      headers: headers,
      body: handle.getText(),
    })
      .then(function (resp) {
        return resp.text().then(function (body) {
          const stamp = parseStampLine(body);
          if (resp.status === 409) {
            setEditorState("Not saved", "error");
            editorConflict(stamp, "The file changed on disk since you opened it.");
            return false;
          }
          if (!resp.ok) throw new Error(errorText(body, resp.statusText));
          edStamp = stamp;
          viewStale = true;
          if (ed === handle) {
            handle.markSaved(snap);
            hideEditorBanner();
            if (!handle.isDirty()) setEditorState("Saved", "saved");
          }
          return true;
        });
      })
      .catch(function (err) {
        setEditorState("Save failed: " + (err && err.message ? err.message : err), "error");
        return false;
      })
      .finally(function () {
        edSaving--;
      });
  }

  function reloadEditorFromDisk() {
    if (!ed) return;
    const handle = ed;
    fetchSource()
      .then(function (src) {
        if (ed !== handle) return;
        handle.setText(src.text);
        edStamp = src.stamp;
        hideEditorBanner();
        setEditorState("Reloaded from disk", "saved");
        handle.focus();
      })
      .catch(function (err) {
        setEditorState("Reload failed: " + (err && err.message ? err.message : err), "error");
      });
  }

  function editorConflict(diskStamp, message) {
    showEditorBanner(message, [
      {
        label: "Overwrite",
        run: function () {
          edStamp = diskStamp;
          saveEditor();
        },
      },
      { label: "Reload from disk", run: reloadEditorFromDisk },
      {
        label: "Keep editing",
        run: function () {
          hideEditorBanner();
          if (ed) ed.focus();
        },
      },
    ]);
  }

  // A stamp from the watch socket while the editor is open.
  function editorNoticeStamp(stamp) {
    if (stamp !== loadStamp) viewStale = true;
    if (!ed || edSaving || stamp === edStamp) return;
    // Our own save's stamp can arrive before the PUT answers; edSaving
    // covers that. Anything else is an outside write: follow it when
    // there is nothing to lose, otherwise ask.
    if (!ed.isDirty()) {
      reloadEditorFromDisk();
      return;
    }
    editorConflict(stamp, "The file changed on disk.");
  }

  function closeEditor() {
    if (!editorIsOpen()) return;
    if (ed && ed.isDirty()) {
      showEditorBanner("You have unsaved changes.", [
        {
          label: "Save and close",
          primary: true,
          run: function () {
            saveEditor().then(function (ok) {
              if (ok) finishCloseEditor();
            });
          },
        },
        { label: "Discard", run: finishCloseEditor },
        {
          label: "Keep editing",
          run: function () {
            hideEditorBanner();
            if (ed) ed.focus();
          },
        },
      ]);
      return;
    }
    finishCloseEditor();
  }

  function finishCloseEditor() {
    if (!editorRoot) return;
    // The editor's scroll position, measured while it is still laid out,
    // becomes the page's.
    let at = null;
    let unmoved = false;
    if (ed) {
      at = { line: ed.topLine(), lines: ed.lineCount() };
      unmoved = Boolean(edOpened) && Math.abs(at.line - edOpened.line) < 0.01;
      ed.destroy();
      ed = null;
    }
    const opened = edOpened;
    edOpened = null;
    hideEditorBanner();
    setEditorState("", "");
    editorRoot.hidden = true;
    if (editBtn) editBtn.setAttribute("aria-expanded", "false");
    if (viewStale) {
      viewStale = false;
      persistScroll();
      if (at) {
        try {
          sessionStore.setItem(SCROLL_KEY, JSON.stringify({ sourceLine: at.line, sourceLines: at.lines }));
        } catch (e) {}
      }
      location.reload();
      return;
    }
    if (unmoved && scrollPane) scrollPane.scrollTop = opened.pageTop;
    else if (at) scrollViewToSourceLine(at.line, at.lines);
    focusScrollPane();
  }

  if (editorRoot) {
    editorRoot.addEventListener("click", function (ev) {
      const t = ev.target;
      if (!(t instanceof Element)) return;
      const btn = t.closest("[data-vellum]");
      if (!btn || !editorRoot.contains(btn)) return;
      const action = btn.getAttribute("data-vellum");
      if (action === "ed-save") saveEditor();
      if (action === "ed-close") closeEditor();
    });
    // Escape inside CodeMirror is handled by its keymap; this covers
    // focus on the bar or banner. A key CodeMirror handled is already
    // default-prevented, and its target may be gone (the search panel's
    // field, removed as Escape closed the panel), so test that first.
    editorRoot.addEventListener("keydown", function (ev) {
      if (ev.key === "Escape" && !ev.defaultPrevented && !(ev.target instanceof Element && ev.target.closest(".cm-editor"))) {
        ev.preventDefault();
        closeEditor();
      }
      if ((ev.metaKey || ev.ctrlKey) && !ev.altKey && (ev.key === "s" || ev.key === "S")) {
        ev.preventDefault();
        saveEditor();
      }
    });
  }

  window.addEventListener("beforeunload", function (ev) {
    if (ed && ed.isDirty()) {
      ev.preventDefault();
      ev.returnValue = "";
    }
  });

  // "e" opens the editor from the page, like GitHub's "." for its editor.
  document.addEventListener("keydown", function (ev) {
    if (ev.key !== "e" && ev.key !== "E") return;
    if (ev.metaKey || ev.ctrlKey || ev.altKey || ev.defaultPrevented) return;
    if (!editorSrc || editorIsOpen()) return;
    if ((lightbox && !lightbox.hidden) || (consentEl && !consentEl.hidden)) return;
    const t = ev.target;
    if (t instanceof Element && t.closest("input, textarea, select, [contenteditable]")) return;
    ev.preventDefault();
    openEditor();
  });

  buildTOC();
  fillTOC();
  trackTitle();
  restoreScroll();
  hydrateDeferred();
  focusScrollPane();
  startWatch();

  if (article) {
    // Only the checkbox itself toggles. Label text stays selectable and
    // does not synthesize a click onto the input.
    article.addEventListener("change", function (ev) {
      const t = ev.target;
      if (!(t instanceof HTMLInputElement) || t.type !== "checkbox") return;
      const item = t.closest("[data-task-index]");
      if (!item || !article.contains(item)) return;
      if (!taskConsent()) {
        pendingTask = { input: t, checked: t.checked };
        showConsent();
        return;
      }
      applyTaskToggle(t);
    });
  }

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
    if (zoomLevel) zoomLevel.textContent = Math.round(lb.scale * 100) + "%";
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
      if (svg.closest(".mermaid-svg")) displayEl.className = "is-diagram";
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
    scheduleFocusScrollPane();
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
    if (zoomLevel) zoomLevel.textContent = Math.round(lb.scale * 100) + "%";
  }

  // A remote image that may be an SVG stands alone but is not measured
  // yet (fit_remote.go): the viewer fetches it now, not before the page.
  // One it can measure becomes a figure; one it cannot (the browser may
  // reach what the viewer does not) stays as it is.
  if (article) {
    article.querySelectorAll("img[data-vellum-fit-pending]").forEach(function (img) {
      const src = img.getAttribute("src") || "";
      fetch(actionURL("svg-fit") + "&src=" + encodeURIComponent(src))
        .then(function (resp) {
          return resp.status === 200 ? resp.json() : null;
        })
        .then(function (fit) {
          img.removeAttribute("data-vellum-fit-pending");
          if (!fit || !(fit.natural > 0)) return;
          img.style.setProperty("--vellum-svg-natural", fit.natural + "px");
          img.style.setProperty("--vellum-svg-ems", String(fit.ems));
          const block = img.closest("[data-vellum-figure-pending]");
          if (block) {
            block.removeAttribute("data-vellum-figure-pending");
            block.setAttribute("data-vellum-figure", "");
          }
        })
        .catch(function () {});
    });
  }

  if (article) {
    article.addEventListener("click", function (ev) {
      const t = ev.target;
      if (!(t instanceof Element)) return;
      // A placeholder has nothing to show in the lightbox yet.
      if (t.closest(".vellum-deferred")) return;
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

  if (consentEl) {
    consentEl.addEventListener("click", function (ev) {
      if (ev.target === consentEl) {
        hideConsent();
        if (pendingTask) {
          pendingTask.input.checked = !pendingTask.checked;
        }
        pendingTask = null;
      }
    });
  }

  if (goneEl) {
    goneEl.addEventListener("click", function (ev) {
      if (ev.target === goneEl) hideGone();
    });
  }

  // Inside T3 Code the page is a card in a sandboxed frame.
  if (embedded) {
    // The frame cannot open a window, and following a link to another
    // site would replace the document with a page that has no way back.
    // Links between documents and within this one work as usual.
    if (article) {
      article.addEventListener("click", function (ev) {
        const a = ev.target && ev.target.closest ? ev.target.closest("a") : null;
        if (!a || !article.contains(a)) return;
        if (a.hasAttribute("data-vellum-local")) {
          ev.preventDefault();
          setStatus("Only Markdown files open here.", false);
          return;
        }
        const href = a.getAttribute("href") || "";
        if (/^[a-z][a-z0-9+.-]*:/i.test(href)) {
          ev.preventDefault();
          setStatus("This link leads outside the document: " + href, false);
        }
      });
    }

    // T3's bridge: the card starts at the height the server declared.
    // Ask for the whole window where the host can give it (a dialog on
    // desktop and web), else for the tallest card it allows.
    const BRIDGE_CARD_HEIGHT = 900;
    window.addEventListener("message", function (ev) {
      const msg = ev.data;
      const port = ev.ports && ev.ports[0];
      if (!msg || msg.type !== "t3.bridge.init" || !port || !msg.surfaceId) return;
      const features = Array.isArray(msg.features) ? msg.features : [];
      // Both: a host that cannot maximize this kind of card still
      // resizes it, and a maximized card ignores its size.
      if (features.indexOf("surface.resize") >= 0) {
        port.postMessage({ type: "surface.update", surfaceId: msg.surfaceId, size: { height: BRIDGE_CARD_HEIGHT } });
      }
      if (features.indexOf("surface.maximize") >= 0) {
        port.postMessage({ type: "surface.update", surfaceId: msg.surfaceId, maximized: true });
      }
    });
    // The host sends its init on load; a page that started listening
    // late asks for it again.
    try {
      window.parent.postMessage({ type: "t3.bridge.hello" }, "*");
    } catch (e) {}
  }

  document.addEventListener("keydown", function (ev) {
    if (goneEl && !goneEl.hidden && ev.key === "Escape") {
      hideGone();
      return;
    }
    if (ev.key === "Escape" && !ev.defaultPrevented && !editorIsOpen() && (!lightbox || lightbox.hidden) && (!consentEl || consentEl.hidden) && closeTocOverlay()) {
      return;
    }
    if (consentEl && !consentEl.hidden && ev.key === "Escape") {
      hideConsent();
      if (pendingTask) {
        pendingTask.input.checked = !pendingTask.checked;
      }
      pendingTask = null;
      return;
    }
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
