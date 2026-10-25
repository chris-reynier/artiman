import { api, withToken, fileURL } from "../api.js";
import { state, setLeaveGuard } from "../app.js";
import { esc, euros, frDate, todayISO, addDays, parseDec, computeTotals, statusBadge, STATUS } from "../util.js";
import { toast, showError, confirmDialog, previewDialog, customerPicker } from "../ui.js";
import { createEditor, totalsHTML } from "./editor.js";

export async function quoteView(id, query) {
  const main = document.getElementById("main");
  const boot = state.boot;
  const st = boot.settings;
  let q, editable = true;
  if (id) {
    const r = await api.get(`/api/quotes/${id}`);
    q = r.quote;
    editable = r.editable;
  } else {
    let customer = null;
    const cid = query && query.get("client");
    if (cid) customer = (await api.get(`/api/customers/${cid}`)).customer;
    else {
      customer = await customerPicker();
      if (!customer) { location.hash = "#/devis"; return; }
    }
    const today = todayISO();
    q = { id: 0, customer_id: customer.id, customer, date: today, valid_until: addDays(today, +st.quote_validity_days || 30), title: "", site: "",
      discount_bp: 0, status: "draft", deposit_terms: st.deposit_terms, notes: "", lines: [], number: "" };
  }
  if (!editable) return readOnlyQuote(main, q);

  const title = q.id ? `Devis ${esc(q.number)}` : "Nouveau devis";
  main.innerHTML = `
    <div class="page-head sticky-bar"><h1>${title} ${q.id ? statusBadge(q.status) : ""} <span class="dirty-dot hidden" id="dirty">● non enregistré</span></h1>
      <div class="actions">
        <button class="btn primary" data-save type="button">Enregistrer</button>
        ${q.id ? `<button class="btn" id="preview" type="button">Aperçu / Imprimer</button>
        <button class="btn" id="pdf" type="button">PDF</button>
        <select id="status" class="btn" title="Statut commercial">${["draft", "sent", "accepted", "refused"].map((s) => `<option value="${s}" ${s === q.status ? "selected" : ""}>${STATUS[s]}</option>`).join("")}</select>
        <button class="btn ok" id="invoice" type="button">Transformer en facture</button>
        <button class="btn" id="dup" type="button">Dupliquer</button>
        <button class="btn danger" id="del" type="button">Supprimer</button>` : ""}
      </div></div>
    <div class="card">
      <div class="doc-head">
        <div class="f"><span class="muted small">Client</span><div style="display:flex;gap:8px;align-items:center">
          <b id="custName">${esc(q.customer.display)}</b><span class="muted" id="custCity">${esc(q.customer.city)}</span>
          <button class="btn sm" id="changeCust" type="button">Changer</button></div></div>
        <label class="f">Date du devis<input type="date" name="date" value="${q.date}"></label>
        <label class="f">Valable jusqu'au<input type="date" name="valid_until" value="${q.valid_until}"></label>
        <label class="f">Objet (facultatif)<input name="title" value="${esc(q.title)}" placeholder="ex. Rénovation peinture appartement"></label>
        <label class="f">Adresse du chantier (si différente)<input name="site" value="${esc(q.site)}"></label>
        <label class="f">Remise globale (%)<input name="discount" value="${q.discount_bp ? String(q.discount_bp / 100).replace(".", ",") : ""}" placeholder="0" inputmode="decimal"></label>
      </div>
    </div>
    <div class="card"><div id="editor"></div></div>
    <div class="card"><div class="editor-foot">
      <div class="left">
        <label class="f">Conditions d'acompte (imprimées sur le devis)<textarea name="deposit_terms" rows="2">${esc(q.deposit_terms)}</textarea></label>
        <label class="f" style="margin-top:8px">Notes internes (non imprimées)<textarea name="notes" rows="2">${esc(q.notes)}</textarea></label>
      </div>
      <div class="totals-box" id="totals"></div>
    </div></div>`;

  let dirty = !q.id;
  const markDirty = () => { dirty = true; main.querySelector("#dirty").classList.remove("hidden"); refresh(); };
  setLeaveGuard(() => dirty);
  const field = (n) => main.querySelector(`[name=${n}]`);
  const editor = createEditor(main.querySelector("#editor"), {
    lines: q.lines, units: boot.units, vatRates: boot.vat_rates, defaultVat: boot.default_vat, franchise: boot.franchise, onChange: markDirty,
  });
  function discountBP() { const v = parseDec(field("discount").value); return Number.isNaN(v) ? 0 : v; }
  function refresh() { main.querySelector("#totals").innerHTML = totalsHTML(computeTotals(editor.lines(), discountBP()), { franchise: boot.franchise }); }
  refresh();
  if (!q.id) dirty = true;
  main.querySelectorAll("input[name], textarea[name]").forEach((el) => el.addEventListener("input", markDirty));
  main.querySelector("#dirty").classList.toggle("hidden", !dirty);
  // Keep the validity date in step with the quote date unless it was changed by hand.
  const validity = +st.quote_validity_days || 30;
  let prevDate = field("date").value;
  field("date").addEventListener("change", () => {
    const d = field("date").value;
    if (d && field("valid_until").value === addDays(prevDate, validity)) field("valid_until").value = addDays(d, validity);
    prevDate = d;
  });

  main.querySelector("#changeCust").addEventListener("click", async () => {
    const c = await customerPicker();
    if (!c) return;
    q.customer_id = c.id;
    q.customer = c;
    main.querySelector("#custName").textContent = c.display;
    main.querySelector("#custCity").textContent = c.city;
    markDirty();
  });

  async function save({ quiet = false } = {}) {
    const errs = editor.errors();
    if (errs.length) { showError(new Error(errs.join(" · "))); return null; }
    const d = discountBP();
    const body = { ...q, date: field("date").value, valid_until: field("valid_until").value, title: field("title").value, site: field("site").value,
      discount_bp: d, deposit_terms: field("deposit_terms").value, notes: field("notes").value, lines: editor.lines() };
    delete body.customer;
    try {
      const r = q.id ? await api.put(`/api/quotes/${q.id}`, body) : await api.post("/api/quotes", body);
      dirty = false;
      if (!quiet) toast("Devis enregistré", "ok");
      return r.id;
    } catch (e) { showError(e); return null; }
  }

  main.querySelector("[data-save]").addEventListener("click", async () => {
    const wasNew = !q.id;
    const nid = await save();
    if (nid == null) return;
    if (wasNew) location.hash = `#/devis/${nid}`;
    else quoteView(nid);
  });
  if (!q.id) { setTimeout(() => editor.focusFirst(), 50); return; }

  const ensureSaved = async () => (dirty ? (await save({ quiet: true })) != null : true);
  main.querySelector("#preview").addEventListener("click", async () => {
    if (!(await ensureSaved())) return;
    previewDialog(`Devis ${q.number}`, `/print/quote/${q.id}`);
  });
  main.querySelector("#pdf").addEventListener("click", async () => {
    if (!(await ensureSaved())) return;
    await makeQuotePDF(q.id);
  });
  main.querySelector("#status").addEventListener("change", async (e) => {
    try { await api.post(`/api/quotes/${q.id}/status`, { status: e.target.value }); toast("Statut mis à jour", "ok"); } catch (err) { showError(err); }
  });
  main.querySelector("#invoice").addEventListener("click", async () => {
    if (!(await ensureSaved())) return;
    if (!(await confirmDialog("Transformer en facture", `<p>Une facture <b>brouillon</b> va être créée à partir de ce devis. Vous pourrez la vérifier
      (acompte, date…) avant de l'émettre.</p>`, { ok: "Créer la facture" }))) return;
    try { const r = await api.post(`/api/quotes/${q.id}/invoice`); dirty = false; location.hash = `#/factures/${r.id}`; } catch (e) { showError(e); }
  });
  main.querySelector("#dup").addEventListener("click", async () => {
    if (!(await ensureSaved())) return;
    try { const r = await api.post(`/api/quotes/${q.id}/duplicate`); toast("Copie créée", "ok"); location.hash = `#/devis/${r.id}`; } catch (e) { showError(e); }
  });
  main.querySelector("#del").addEventListener("click", async () => {
    if (!(await confirmDialog("Supprimer ce devis ?", `<p>Le devis ${esc(q.number)} sera définitivement supprimé.</p>`, { ok: "Supprimer", danger: true }))) return;
    try { await api.del(`/api/quotes/${q.id}`); dirty = false; toast("Devis supprimé", "ok"); location.hash = "#/devis"; } catch (e) { showError(e); }
  });
}

async function makeQuotePDF(id) {
  toast("Génération du PDF…");
  try {
    const r = await api.post(`/api/quotes/${id}/pdf`);
    toast("PDF enregistré dans data\\documents\\" + r.pdf_path.replaceAll("/", "\\"), "ok");
    window.open(fileURL("documents", r.pdf_path), "_blank");
  } catch (e) { showError(e); }
}

function readOnlyQuote(main, q) {
  const legacy = q.legacy;
  main.innerHTML = `
    <div class="page-head"><h1>Devis ${q.number ? esc(q.number) : "du " + frDate(q.date)} ${statusBadge(q.status)} ${legacy ? '<span class="badge legacy">historique</span>' : ""}</h1>
      <div class="actions">
        <button class="btn primary" id="dup" type="button">Dupliquer en nouveau devis</button>
        ${q.invoice_id ? `<a class="btn" href="#/factures/${q.invoice_id}">Voir ${esc(q.invoice_label)}</a>` : ""}
        <button class="btn" id="print" type="button">Imprimer</button>
        ${legacy ? "" : `<button class="btn" id="pdf" type="button">PDF</button>`}
        ${legacy && q.legacy_source ? `<a class="btn" href="${fileURL("legacy", q.legacy_source)}">Document Word d'origine</a>` : ""}
      </div></div>
    ${legacy ? `<div class="alert info"><div class="grow">Devis importé de l'ancien ArtiMan (lecture seule). Client : <a href="#/clients/${q.customer_id}">${esc(q.customer.display)}</a>.
      ${q.legacy_flags ? `<br><b>À vérifier :</b> ${esc(q.legacy_flags)}` : ""}</div></div>`
      : `<div class="alert info">Ce devis a été facturé : il n'est plus modifiable. Dupliquez-le pour en faire un nouveau.</div>`}
    <iframe class="preview-frame" style="height:78vh" src="${withToken(`/print/quote/${q.id}?preview=1`)}"></iframe>`;
  main.querySelector("#dup").addEventListener("click", async () => {
    try { const r = await api.post(`/api/quotes/${q.id}/duplicate`); toast("Nouveau devis créé à partir de celui-ci", "ok"); location.hash = `#/devis/${r.id}`; } catch (e) { showError(e); }
  });
  main.querySelector("#print").addEventListener("click", () => main.querySelector("iframe").contentWindow.print());
  const pdf = main.querySelector("#pdf");
  if (pdf) pdf.addEventListener("click", () => makeQuotePDF(q.id));
}
