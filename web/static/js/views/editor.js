// Line editor shared by quotes and draft invoices.
import { esc, euros, parseDec, fmtInput, lineTotal, rate } from "../util.js";
import { libraryPicker } from "../ui.js";

const blank = (kind, vat, unit = "U") => ({ kind, label: "", description: "", unit, qty_c: kind === "item" ? 100 : 0, unit_price_cents: 0, vat_bp: vat });

/**
 * Mounts the editor in `root`.
 * opts: { lines, units, vatRates, defaultVat, onChange }
 * Returns { lines(), errors(), addItem() }.
 */
export function createEditor(root, opts) {
  let lines = (opts.lines || []).map((l) => ({ ...l, _qty: fmtInput(l.qty_c), _pu: fmtInput(l.unit_price_cents) }));
  if (!lines.length) lines.push({ ...blank("item", opts.defaultVat), _qty: "1,00", _pu: "" });
  const units = opts.units;
  const vats = opts.vatRates;

  const unitOptions = (u) => {
    const list = units.includes(u) || !u ? units : [u, ...units];
    return list.map((x) => `<option ${x === u ? "selected" : ""}>${esc(x)}</option>`).join("");
  };
  const vatOptions = (v) => {
    const list = vats.includes(v) ? vats : [v, ...vats];
    return list.map((x) => `<option value="${x}" ${x === v ? "selected" : ""}>${x === 0 ? (opts.franchise ? "Sans TVA" : "0 % (autoliq.)") : rate(x)}</option>`).join("");
  };
  const actions = (i) => `<td class="c-act">
    <button tabindex="-1" class="btn icon sm" data-act="up" title="Monter" type="button" ${i === 0 ? "disabled" : ""}>↑</button><button tabindex="-1" class="btn icon sm" data-act="down" title="Descendre" type="button" ${i === lines.length - 1 ? "disabled" : ""}>↓</button><button tabindex="-1" class="btn icon sm" data-act="dup" title="Dupliquer la ligne" type="button">⧉</button><button tabindex="-1" class="btn icon sm danger" data-act="del" title="Supprimer la ligne" type="button">✕</button></td>`;

  function rowHTML(l, i) {
    const handle = `<td class="handle" title="Glisser pour déplacer">⋮⋮</td>`;
    if (l.kind === "section") {
      return `<tbody class="line section" data-i="${i}"><tr class="item-main section">${handle}
        <td colspan="6"><input class="label" data-f="label" value="${esc(l.label)}" placeholder="Titre de section (ex. SALLE DE BAINS)"></td>${actions(i)}</tr>
        <tr class="line-sep"><td colspan="8"></td></tr></tbody>`;
    }
    if (l.kind === "text") {
      return `<tbody class="line text" data-i="${i}"><tr class="item-main">${handle}
        <td colspan="6"><textarea class="desc" data-f="description" rows="1" placeholder="Texte libre (remarque, condition…)">${esc(l.description || l.label)}</textarea></td>${actions(i)}</tr>
        <tr class="line-sep"><td colspan="8"></td></tr></tbody>`;
    }
    const bad = (s, dec) => Number.isNaN(parseDec(s, dec)) ? ' style="border-color:#c63b37"' : "";
    return `<tbody class="line item" data-i="${i}"><tr class="item-main">${handle}
      <td class="c-label"><input class="label" data-f="label" value="${esc(l.label)}" placeholder="Désignation (ex. MURS SALON)"></td>
      <td class="c-qty"><input class="num" data-f="qty" value="${esc(l._qty)}" inputmode="decimal"${bad(l._qty)}></td>
      <td class="c-unit"><select data-f="unit">${unitOptions(l.unit)}</select></td>
      <td class="c-pu"><input class="num" data-f="pu" value="${esc(l._pu)}" inputmode="decimal" placeholder="0,00"${bad(l._pu)}></td>
      <td class="c-vat"><select data-f="vat">${vatOptions(l.vat_bp)}</select></td>
      <td class="total c-total" data-total>${euros(lineTotal(l))}</td>${actions(i)}</tr>
      <tr><td></td><td colspan="5"><div class="desc-row"><textarea class="desc" data-f="description" rows="1" placeholder="Description des travaux (ex. Lessivage + 2 couches de peinture satinée)">${esc(l.description)}</textarea>
        <button tabindex="-1" class="btn icon sm" data-act="lib" title="Bibliothèque de descriptions" type="button">📚</button></div></td><td></td><td></td></tr>
      <tr class="line-sep"><td colspan="8"></td></tr></tbody>`;
  }

  function render(focus) {
    root.innerHTML = `<table class="lines"><thead><tr><th></th><th>Désignation / description</th><th class="num">Quantité</th><th>Unité</th>
      <th class="num">P.U. HT</th><th>TVA</th><th class="num">Total HT</th><th></th></tr></thead>${lines.map(rowHTML).join("")}</table>
      <div class="add-row"><button class="btn" data-add="item" type="button">+ Ligne</button><button class="btn" data-add="section" type="button">+ Section</button>
      <button class="btn" data-add="text" type="button">+ Texte libre</button><span class="muted small" style="align-self:center">Entrée : champ suivant · Ctrl+Entrée : nouvelle ligne · 📚 : bibliothèque · glisser ⋮⋮ pour réordonner</span></div>`;
    root.querySelectorAll("textarea.desc").forEach(grow);
    if (focus) {
      const el = root.querySelector(`tbody[data-i="${focus.i}"] [data-f="${focus.f || "label"}"]`) ||
        root.querySelector(`tbody[data-i="${focus.i}"] [data-f]`);
      if (el) { el.focus(); if (el.select && focus.select) el.select(); }
    }
  }

  function grow(t) { t.style.height = "auto"; t.style.height = Math.max(34, t.scrollHeight + 2) + "px"; }
  const changed = () => opts.onChange && opts.onChange();

  root.addEventListener("input", (e) => {
    const f = e.target.dataset.f;
    const tb = e.target.closest("tbody[data-i]");
    if (!f || !tb) return;
    const l = lines[+tb.dataset.i];
    const v = e.target.value;
    if (f === "label") l.label = v;
    if (f === "description") { l.description = v; grow(e.target); }
    if (f === "qty") { l._qty = v; const n = parseDec(v); e.target.style.borderColor = Number.isNaN(n) ? "#c63b37" : ""; if (!Number.isNaN(n)) l.qty_c = n; }
    if (f === "pu") { l._pu = v; const n = parseDec(v); e.target.style.borderColor = Number.isNaN(n) ? "#c63b37" : ""; if (!Number.isNaN(n)) l.unit_price_cents = n; }
    if (f === "qty" || f === "pu") tb.querySelector("[data-total]").textContent = euros(lineTotal(l));
    changed();
  });
  root.addEventListener("change", (e) => {
    const f = e.target.dataset.f;
    const tb = e.target.closest("tbody[data-i]");
    if (!f || !tb) return;
    const l = lines[+tb.dataset.i];
    if (f === "unit") l.unit = e.target.value;
    if (f === "vat") { l.vat_bp = +e.target.value; opts.defaultVat = l.vat_bp; }
    if (f === "qty" && !Number.isNaN(parseDec(l._qty))) { l._qty = fmtInput(l.qty_c) || ""; e.target.value = l._qty; }
    if (f === "pu" && !Number.isNaN(parseDec(l._pu))) { l._pu = fmtInput(l.unit_price_cents); e.target.value = l._pu; }
    changed();
  });

  root.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
      const tb = e.target.closest("tbody[data-i]");
      e.preventDefault();
      addLine("item", tb ? +tb.dataset.i + 1 : lines.length);
      return;
    }
    if (e.key !== "Enter" || e.target.tagName === "TEXTAREA" || !e.target.dataset.f) return;
    e.preventDefault();
    const fields = [...root.querySelectorAll("[data-f]")];
    const idx = fields.indexOf(e.target);
    if (idx === fields.length - 1) { addLine("item"); return; }
    const next = fields[idx + 1];
    next.focus();
    if (next.select) next.select();
  });

  function addLine(kind, at = lines.length) {
    const prev = [...lines].reverse().find((l) => l.kind === "item");
    const l = { ...blank(kind, prev ? prev.vat_bp : opts.defaultVat, prev ? prev.unit : "U") };
    l._qty = kind === "item" ? "1,00" : "";
    l._pu = "";
    lines.splice(at, 0, l);
    render({ i: at, f: kind === "text" ? "description" : "label" });
    changed();
  }

  root.addEventListener("click", async (e) => {
    const add = e.target.closest("[data-add]");
    if (add) { addLine(add.dataset.add); return; }
    const btn = e.target.closest("[data-act]");
    if (!btn) return;
    const i = +btn.closest("tbody[data-i]").dataset.i;
    const act = btn.dataset.act;
    if (act === "up" && i > 0) { [lines[i - 1], lines[i]] = [lines[i], lines[i - 1]]; render(); }
    if (act === "down" && i < lines.length - 1) { [lines[i + 1], lines[i]] = [lines[i], lines[i + 1]]; render(); }
    if (act === "dup") { lines.splice(i + 1, 0, { ...lines[i], id: undefined }); render({ i: i + 1 }); }
    if (act === "del") { lines.splice(i, 1); if (!lines.length) lines.push({ ...blank("item", opts.defaultVat), _qty: "1,00", _pu: "" }); render(); }
    if (act === "lib") {
      const t = await libraryPicker(lines[i].description || "");
      if (t) {
        lines[i].description = lines[i].description.trim() ? lines[i].description.trimEnd() + "\n" + t : t;
        render({ i, f: "description" });
      }
    }
    changed();
  });

  // Drag and drop by the ⋮⋮ handle.
  let dragFrom = null;
  root.addEventListener("mousedown", (e) => {
    const h = e.target.closest(".handle");
    if (h) h.closest("tbody").draggable = true;
  });
  root.addEventListener("dragstart", (e) => {
    const tb = e.target.closest("tbody[data-i]");
    if (!tb) return;
    dragFrom = +tb.dataset.i;
    tb.classList.add("dragging");
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", String(dragFrom));
  });
  root.addEventListener("dragover", (e) => {
    const tb = e.target.closest("tbody[data-i]");
    if (dragFrom === null || !tb) return;
    e.preventDefault();
    root.querySelectorAll(".drop-before").forEach((x) => x.classList.remove("drop-before"));
    tb.classList.add("drop-before");
  });
  root.addEventListener("drop", (e) => {
    const tb = e.target.closest("tbody[data-i]");
    if (dragFrom === null || !tb) return;
    e.preventDefault();
    let to = +tb.dataset.i;
    const [l] = lines.splice(dragFrom, 1);
    if (to > dragFrom) to--;
    lines.splice(to, 0, l);
    dragFrom = null;
    render();
    changed();
  });
  root.addEventListener("dragend", () => {
    dragFrom = null;
    root.querySelectorAll("tbody").forEach((t) => { t.draggable = false; t.classList.remove("dragging", "drop-before"); });
  });

  render();
  return {
    lines: () => lines.map(({ _qty, _pu, ...l }) => (l.kind === "text" ? { ...l, label: "" } : l)),
    errors: () => {
      const errs = [];
      lines.forEach((l, i) => {
        if (l.kind !== "item") return;
        if (Number.isNaN(parseDec(l._qty))) errs.push(`Ligne ${i + 1} : quantité invalide`);
        if (Number.isNaN(parseDec(l._pu))) errs.push(`Ligne ${i + 1} : prix invalide`);
      });
      return errs;
    },
    focusFirst: () => { const el = root.querySelector("[data-f]"); if (el) el.focus(); },
  };
}

/** Renders the totals box. */
export function totalsHTML(t, { advance = false, franchise = false } = {}) {
  let rows = "";
  if (t.discount_cents) rows += `<tr><td>Total brut HT</td><td class="num">${euros(t.gross_cents)}</td></tr><tr><td>Remise</td><td class="num">-${euros(t.discount_cents)}</td></tr>`;
  if (franchise) {
    rows += `<tr class="strong"><td>Total</td><td class="num">${euros(t.ttc_cents)}</td></tr>`;
    if (advance && t.advance_cents) rows += `<tr><td>Acompte</td><td class="num">-${euros(t.advance_cents)}</td></tr><tr class="strong"><td>Net à payer</td><td class="num">${euros(t.net_cents)}</td></tr>`;
    return `<table>${rows}</table><p class="muted small">TVA non applicable (franchise en base)</p>`;
  }
  rows += `<tr class="strong"><td>Total HT</td><td class="num">${euros(t.ht_cents)}</td></tr>`;
  for (const v of t.vat || []) rows += `<tr><td>TVA ${rate(v.rate_bp)}${(t.vat || []).length > 1 ? ` sur ${euros(v.base_cents)}` : ""}</td><td class="num">${euros(v.vat_cents)}</td></tr>`;
  rows += `<tr class="strong"><td>Total TTC</td><td class="num">${euros(t.ttc_cents)}</td></tr>`;
  if (advance && t.advance_cents) rows += `<tr><td>Acompte</td><td class="num">-${euros(t.advance_cents)}</td></tr><tr class="strong"><td>Net à payer</td><td class="num">${euros(t.net_cents)}</td></tr>`;
  return `<table>${rows}</table>`;
}
