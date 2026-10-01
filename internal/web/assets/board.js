// Agent Board Web: progressive enhancement only. Every action is a plain
// form post or link and works without this script. The script adds:
// idempotency keys on submit (a double submit replays instead of repeating),
// the new-task modal without a page load, Escape to close, and live refresh
// of the board region when any process changes the Board.
(function () {
  "use strict";

  var POLL_MS = 4000;
  var live = document.getElementById("board-live");
  var modal = document.getElementById("new-task-modal");
  var status = document.getElementById("live-status");
  var dirty = false;

  function randomKey() {
    var bytes = new Uint8Array(16);
    window.crypto.getRandomValues(bytes);
    return "web-" + Array.prototype.map.call(bytes, function (b) {
      return ("0" + b.toString(16)).slice(-2);
    }).join("");
  }

  document.addEventListener("submit", function (event) {
    var form = event.target;
    if (!form.matches || !form.matches("form[data-write-form]")) return;
    var key = form.querySelector("input[name=idempotency_key]");
    if (key && !key.value) key.value = randomKey();
  }, true);

  // Drop one-shot banner parameters so a reload does not repeat them.
  (function cleanURL() {
    var url = new URL(window.location.href);
    var changed = false;
    ["notice", "error", "field", "new"].forEach(function (name) {
      if (url.searchParams.has(name)) { url.searchParams.delete(name); changed = true; }
    });
    if (changed) window.history.replaceState(null, "", url.pathname + url.search);
  })();

  function modalOpen() { return modal && !modal.hidden; }

  function showModal() {
    if (!modal) return;
    modal.hidden = false;
    var title = modal.querySelector("input[name=title]");
    if (title) title.focus();
  }

  function hideModal() { if (modal) modal.hidden = true; }

  document.addEventListener("click", function (event) {
    var target = event.target;
    if (!target.closest) return;
    if (target.closest("#new-task-btn")) {
      event.preventDefault();
      showModal();
    } else if (target.closest("[data-close-modal]")) {
      event.preventDefault();
      hideModal();
    } else if (target === modal) {
      hideModal();
    }
  });

  document.addEventListener("keydown", function (event) {
    if (event.key !== "Escape") return;
    if (modalOpen()) { hideModal(); return; }
    if (editing()) return; // never discard what someone is typing
    var close = document.getElementById("drawer-close");
    if (close) window.location.href = close.getAttribute("href");
  });

  if (!live) return;
  if (modalOpen()) showModal();

  live.addEventListener("input", function () { dirty = true; });

  function editing() {
    if (dirty) return true;
    var active = document.activeElement;
    return !!(active && active.closest && active.closest("#board-live form") &&
      active.matches("input[type=text], textarea, select"));
  }

  function setStatus(text) {
    if (!status) return;
    status.textContent = text || "";
    status.hidden = !text;
  }

  function swap(html, etag) {
    var openDetails = [];
    live.querySelectorAll("details[id][open]").forEach(function (d) { openDetails.push(d.id); });
    var drawer = document.getElementById("drawer");
    var drawerScroll = drawer ? drawer.scrollTop : 0;
    var pageScroll = window.scrollY;
    live.innerHTML = html;
    live.dataset.etag = etag;
    openDetails.forEach(function (id) {
      var d = document.getElementById(id);
      if (d) d.open = true;
    });
    drawer = document.getElementById("drawer");
    if (drawer) drawer.scrollTop = drawerScroll;
    window.scrollTo(0, pageScroll);
  }

  function poll() {
    var query = live.dataset.liveQuery;
    var headers = live.dataset.etag ? { "If-None-Match": live.dataset.etag } : {};
    window.fetch("/live" + (query ? "?" + query : ""), { headers: headers, cache: "no-store" })
      .then(function (response) {
        if (response.status === 304) { setStatus(dirty ? status.textContent : ""); return null; }
        if (!response.ok && response.status !== 404) throw new Error("status " + response.status);
        var etag = response.headers.get("ETag") || "";
        return response.text().then(function (html) {
          if (editing()) { setStatus(document.body.dataset.liveUpdated); return; }
          swap(html, etag);
          setStatus("");
        });
      })
      .catch(function () { setStatus(document.body.dataset.liveStale); })
      .then(function () { window.setTimeout(poll, POLL_MS); });
  }

  window.setTimeout(poll, POLL_MS);
})();
