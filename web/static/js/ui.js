// Toasts, modals, confirmations and pickers.
import { esc, debounce, norm } from "./util.js";
import { api, withToken } from "./api.js";

export function toast(msg, kind = "") {
  const el = document.createElement("div");
  el.className = "toast " + kind;
  el.textContent = msg;
  document.getElementById("toasts").appendChild(el);
  setTimeout(() => el.remove(), kind === "error" ? 7000 : 3500);
}

export const showError = (e) => toast(e.message || String(e), "error");

/** Opens a modal; returns { el, close }. onClose runs when it is dismissed. */
export function modal(html, { wide = false, onClose } = {}) {
  const bg = document.createElement("div");
  bg.className = "modal-bg";
  bg.innerHTML = `<div class="modal ${wide ? "wide" : ""}">${html}</div>`;
  document.body.appendChild(bg);
  const close = () => { bg.remove(); document.removeEventListener("keydown", onKey); onClose && onClose(); };
  const onKey = (e) => { if (e.key === "Escape") close(); };
  document.addEventListener("keydown", onKey);
  bg.addEventListener("mousedown", (e) => { if (e.target === bg) close(); });
  bg.querySelectorAll("[data-close]").forEach((b) => b.addEventListener("click", close));
  return { el: bg.querySelector(".modal"), close };
}

export function confirmDialog(title, message, { ok = "Confirmer", danger = false } = {}) {
  return new Promise((resolve) => {
    let done = false;
    const m = modal(`
      <div class="modal-head"><h2>${esc(title)}</h2></div>
      <div>${message}</div>
      <div class="modal-foot"><button class="btn" data-close type="button">Annuler</button>
      <button class="btn ${danger ? "danger" : "primary"}" data-ok type="button">${esc(ok)}</button></div>`,
      { onClose: () => { if (!done) resolve(false); } });
    m.el.querySelector("[data-ok]").addEventListener("click", () => { done = true; m.close(); resolve(true); });
    m.el.querySelector("[data-ok]").focus();
  });
}

/** Shows a print page in a modal with Print and PDF actions. */
export function previewDialog(title, printUrl, actions = "") {
  const m = modal(`
    <div class="modal-head"><h2>${esc(title)}</h2><div class="actions" style="display:flex;gap:6px">
      ${actions}
      <button class="btn primary" data-print type="button">Imprimer</button>
      <button class="btn" data-close type="button">Fermer</button></div></div>
    <iframe class="preview-frame" src="${withToken(printUrl + (printUrl.includes("?") ? "&" : "?") + "preview=1")}"></iframe>`, { wide: true });
  m.el.querySelector("[data-print]").addEventListener("click", () => m.el.querySelector("iframe").contentWindow.print());
  return m;
}

/** Description library: search, insert, remove. Resolves with the chosen text or null. */
export function libraryPicker(current = "") {
  return new Promise((resolve) => {
    let chosen = null;
    const m = modal(`
      <div class="modal-head"><h2>Bibliothèque de descriptions</h2><button class="btn" data-close type="button">Fermer</button></div>
      <input type="search" placeholder="Rechercher (ex. lasure, façade, plafond…)" style="width:100%">
      <div class="pick-list"></div>
      <div class="modal-foot" style="justify-content:space-between">
        <button class="btn sm" data-add type="button" ${current.trim() ? "" : "disabled"}>Ajouter la description actuelle à la bibliothèque</button>
        <span class="muted small">Clic : insérer · Suppr : retirer de la bibliothèque</span>
      </div>`, { onClose: () => resolve(chosen) });
    const input = m.el.querySelector("input");
    const list = m.el.querySelector(".pick-list");
    let items = [];
    let sel = 0;
    const render = () => {
      list.innerHTML = items.map((t, i) => `<div data-i="${i}" class="${i === sel ? "sel" : ""}">${esc(t)}</div>`).join("") ||
        `<div class="muted">Aucune description</div>`;
    };
    const load = debounce(async () => {
      try { items = await api.get("/api/templates?q=" + encodeURIComponent(input.value)); sel = 0; render(); } catch (e) { showError(e); }
    }, 150);
    input.addEventListener("input", load);
    input.addEventListener("keydown", async (e) => {
      if (e.key === "ArrowDown") { sel = Math.min(sel + 1, items.length - 1); render(); e.preventDefault(); }
      if (e.key === "ArrowUp") { sel = Math.max(sel - 1, 0); render(); e.preventDefault(); }
      if (e.key === "Enter" && items[sel]) { chosen = items[sel]; m.close(); }
      if (e.key === "Delete" && items[sel] && !input.value.length) {
        await api.del("/api/templates?text=" + encodeURIComponent(items[sel])); load();
      }
    });
    list.addEventListener("click", (e) => {
      const d = e.target.closest("[data-i]");
      if (d) { chosen = items[+d.dataset.i]; m.close(); }
    });
    m.el.querySelector("[data-add]").addEventListener("click", async () => {
      try { await api.post("/api/templates", { text: current }); toast("Ajoutée à la bibliothèque", "ok"); load(); } catch (e) { showError(e); }
    });
    load();
    input.focus();
  });
}

/** Customer picker with search; resolves with a customer or null. "new" opens the creation form. */
export function customerPicker() {
  return new Promise(async (resolve) => {
    let chosen = null;
    const m = modal(`
      <div class="modal-head"><h2>Choisir un client</h2><div style="display:flex;gap:6px">
        <a class="btn" href="#/clients/nouveau" data-new>+ Nouveau client</a><button class="btn" data-close type="button">Fermer</button></div></div>
      <input type="search" placeholder="Nom, ville…" style="width:100%">
      <div class="pick-list"></div>`, { onClose: () => resolve(chosen) });
    const input = m.el.querySelector("input");
    const list = m.el.querySelector(".pick-list");
    m.el.querySelector("[data-new]").addEventListener("click", () => m.close());
    let all = [];
    try { all = await api.get("/api/customers"); } catch (e) { showError(e); }
    let shown = [];
    let sel = 0;
    const render = () => {
      const q = norm(input.value);
      shown = all.filter((c) => !q || norm(c.display + " " + c.city).includes(q)).slice(0, 80);
      sel = Math.min(sel, Math.max(shown.length - 1, 0));
      list.innerHTML = shown.map((c, i) => `<div data-i="${i}" class="${i === sel ? "sel" : ""}"><b>${esc(c.display)}</b>
        <span class="muted"> ${esc(c.city)}</span></div>`).join("") || `<div class="muted">Aucun client</div>`;
    };
    input.addEventListener("input", () => { sel = 0; render(); });
    input.addEventListener("keydown", (e) => {
      if (e.key === "ArrowDown") { sel = Math.min(sel + 1, shown.length - 1); render(); e.preventDefault(); }
      if (e.key === "ArrowUp") { sel = Math.max(sel - 1, 0); render(); e.preventDefault(); }
      if (e.key === "Enter" && shown[sel]) { chosen = shown[sel]; m.close(); }
    });
    list.addEventListener("click", (e) => {
      const d = e.target.closest("[data-i]");
      if (d) { chosen = shown[+d.dataset.i]; m.close(); }
    });
    render();
    input.focus();
  });
}

/** Simple bar chart (one or two series) with hover tooltips. */
export function barChart(el, rows, series, fmt, onClick) {
  const W = 640, H = 240, L = 54, R = 8, T = 10, B = 24;
  const maxV = Math.max(1, ...rows.flatMap((r) => series.map((s) => r[s.key] || 0)));
  const p = 10 ** Math.floor(Math.log10(maxV));
  const max = [1, 2, 5, 10].map((m) => m * p).find((v) => v >= maxV) || maxV;
  const bw = (W - L - R) / Math.max(rows.length, 1);
  const inner = Math.min(bw * 0.72, 34 * series.length);
  const sw = (inner - 2 * (series.length - 1)) / series.length;
  const y = (v) => T + (H - T - B) * (1 - v / max);
  let s = `<svg viewBox="0 0 ${W} ${H}" role="img">`;
  for (let k = 0; k <= 4; k++) {
    const v = (max * k) / 4, yy = y(v);
    s += `<line x1="${L}" x2="${W - R}" y1="${yy}" y2="${yy}" stroke="#e6e4de"/>`;
    s += `<text x="${L - 6}" y="${yy + 4}" text-anchor="end" font-size="10.5" fill="#85837c">${esc(fmt.axis(v))}</text>`;
  }
  rows.forEach((r, i) => {
    const x0 = L + i * bw + (bw - inner) / 2;
    series.forEach((se, j) => {
      const v = Math.max(0, r[se.key] || 0), x = x0 + j * (sw + 2), yy = y(v), h = H - B - yy, rad = Math.min(3, sw / 2, h);
      if (h > 0) s += `<path d="M${x},${H - B}V${yy + rad}Q${x},${yy} ${x + rad},${yy}H${x + sw - rad}Q${x + sw},${yy} ${x + sw},${yy + rad}V${H - B}Z" fill="${se.color}"/>`;
    });
    s += `<rect data-i="${i}" x="${L + i * bw}" y="${T}" width="${bw}" height="${H - T - B}" fill="transparent" style="cursor:${onClick ? "pointer" : "default"}"/>`;
    if (rows.length <= 12 || i % 2 === 0) s += `<text x="${L + i * bw + bw / 2}" y="${H - 7}" text-anchor="middle" font-size="10.5" fill="#55544f">${esc(r.label)}</text>`;
  });
  el.innerHTML = s + `</svg><div class="tip"></div>`;
  const tip = el.querySelector(".tip");
  el.querySelectorAll("rect[data-i]").forEach((h) => {
    h.addEventListener("mousemove", (ev) => {
      const r = rows[+h.dataset.i], box = el.getBoundingClientRect();
      tip.innerHTML = `<div><b>${esc(r.label)}</b></div>` + series.map((se) =>
        `<div><span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:${se.color};margin-right:5px"></span>${esc(se.label)} : <b>${esc(fmt.tip(r[se.key] || 0))}</b></div>`).join("");
      tip.style.display = "block";
      let x = ev.clientX - box.left + 12;
      if (x + tip.offsetWidth > box.width) x = ev.clientX - box.left - tip.offsetWidth - 12;
      tip.style.left = x + "px";
      tip.style.top = ev.clientY - box.top - 10 + "px";
      h.setAttribute("fill", "rgba(127,127,127,.08)");
    });
    h.addEventListener("mouseleave", () => { tip.style.display = "none"; h.setAttribute("fill", "transparent"); });
    if (onClick) h.addEventListener("click", () => onClick(rows[+h.dataset.i]));
  });
}
