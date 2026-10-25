import { api, withToken, fileURL } from "../api.js";
import { state, setLeaveGuard } from "../app.js";
import { esc, euros, frDate, todayISO, parseDec, fmtInput, computeTotals, statusBadge } from "../util.js";
import { toast, showError, confirmDialog, previewDialog, customerPicker } from "../ui.js";
import { createEditor, totalsHTML } from "./editor.js";

export async function invoiceView(id, query) {
  const main = document.getElementById("main");
  if (!id) {
    // Blank invoice without quote: create the draft right away.
    let cid = query && query.get("client");
    if (!cid) {
      const c = await customerPicker();
      if (!c) { location.hash = "#/factures"; return; }
      cid = c.id;
    }
    const r = await api.post("/api/invoices", { customer_id: +cid, date: todayISO(), lines: [] });
    history.replaceState(null, "", `#/factures/${r.id}`);
    return invoiceView(r.id);
  }
  const { invoice: inv, next_number } = await api.get(`/api/invoices/${id}`);
  if (inv.status === "draft") return draftInvoice(main, inv, next_number);
  return issuedInvoice(main, inv);
}

function draftInvoice(main, inv, nextNumber) {
  const boot = state.boot;
  const isCredit = inv.kind === "credit_note";
  const noun = isCredit ? "l'avoir" : "la facture";
  main.innerHTML = `
    <div class="page-head sticky-bar"><h1>${isCredit ? "Avoir" : "Facture"} brouillon <span class="badge draft">non émis${isCredit ? "" : "e"}</span>
      <span class="dirty-dot hidden" id="dirty">● non enregistré</span></h1>
      <div class="actions">
        <button class="btn primary" data-save type="button">Enregistrer</button>
        <button class="btn" id="preview" type="button">Aperçu</button>
        <button class="btn ok" id="issue" type="button">Émettre ${noun} n°${String(nextNumber).padStart(4, "0")}</button>
        <button class="btn danger" id="del" type="button">Supprimer le brouillon</button>
      </div></div>
    <div class="alert info"><div class="grow">Tant qu'${isCredit ? "il" : "elle"} n'est pas émis${isCredit ? "" : "e"}, ${noun} reste modifiable et n'a pas de numéro.
      À l'émission, ${isCredit ? "il reçoit le numéro suivant, est archivé en PDF et devient définitif" : "elle reçoit le numéro suivant, est archivée en PDF et devient définitive (une correction se fera par avoir)"}.
      ${inv.credited_label ? `<br>Avoir sur <b>${esc(inv.credited_label)}</b>.` : ""}${inv.quote_id ? `<br>Issue du <a href="#/devis/${inv.quote_id}">devis d'origine</a>.` : ""}</div></div>
    <div class="card"><div class="doc-head">
      <div class="f"><span class="muted small">Client</span><div style="display:flex;gap:8px;align-items:center">
        <b id="custName">${esc(inv.customer.display)}</b><span class="muted" id="custCity">${esc(inv.customer.city)}</span>
        <button class="btn sm" id="changeCust" type="button">Changer</button></div></div>
      <label class="f">Date de ${isCredit ? "l'avoir" : "facture"}<input type="date" name="date" value="${inv.date}"></label>
      <label class="f">${isCredit ? "" : "Acompte déjà versé (€)"}<input name="advance" value="${fmtInput(inv.advance_cents)}" placeholder="0,00" inputmode="decimal" ${isCredit ? 'type="hidden"' : ""}></label>
      <label class="f">Objet (facultatif)<input name="title" value="${esc(inv.title)}"></label>
      <label class="f">Adresse du chantier<input name="site" value="${esc(inv.site)}"></label>
      <label class="f">Remise globale (%)<input name="discount" value="${inv.discount_bp ? String(inv.discount_bp / 100).replace(".", ",") : ""}" placeholder="0" inputmode="decimal"></label>
    </div></div>
    <div class="card"><div id="editor"></div></div>
    <div class="card"><div class="editor-foot">
      <div class="left"><label class="f">Notes internes (non imprimées)<textarea name="notes" rows="2">${esc(inv.notes)}</textarea></label>
        <p class="muted small">Conditions et mentions légales imprimées : ${esc(boot.settings.payment_terms)}. Modifiables dans les Paramètres.</p></div>
      <div class="totals-box" id="totals"></div></div></div>`;
  let dirty = false;
  const markDirty = () => { dirty = true; main.querySelector("#dirty").classList.remove("hidden"); refresh(); };
  setLeaveGuard(() => dirty);
  const field = (n) => main.querySelector(`[name=${n}]`);
  const num = (n) => { const v = parseDec(field(n).value); return Number.isNaN(v) ? 0 : v; };
  const editor = createEditor(main.querySelector("#editor"), {
    lines: inv.lines, units: boot.units, vatRates: boot.vat_rates, defaultVat: boot.default_vat, franchise: boot.franchise, onChange: markDirty,
  });
  function refresh() { main.querySelector("#totals").innerHTML = totalsHTML(computeTotals(editor.lines(), num("discount"), num("advance")), { advance: true, franchise: boot.franchise }); }
  refresh();
  main.querySelectorAll("input[name], textarea[name]").forEach((el) => el.addEventListener("input", markDirty));
  main.querySelector("#changeCust").addEventListener("click", async () => {
    const c = await customerPicker();
    if (!c) return;
    inv.customer_id = c.id;
    main.querySelector("#custName").textContent = c.display;
    main.querySelector("#custCity").textContent = c.city;
    markDirty();
  });

  async function save(quiet) {
    const errs = editor.errors();
    if (errs.length) { showError(new Error(errs.join(" · "))); return false; }
    const body = { customer_id: inv.customer_id, date: field("date").value, title: field("title").value, site: field("site").value,
      discount_bp: num("discount"), advance_cents: num("advance"), notes: field("notes").value, lines: editor.lines() };
    try {
      await api.put(`/api/invoices/${inv.id}`, body);
      dirty = false;
      main.querySelector("#dirty").classList.add("hidden");
      if (!quiet) toast("Brouillon enregistré", "ok");
      return true;
    } catch (e) { showError(e); return false; }
  }
  main.querySelector("[data-save]").addEventListener("click", () => save(false));
  main.querySelector("#preview").addEventListener("click", async () => {
    if (dirty && !(await save(true))) return;
    previewDialog("Aperçu du brouillon", `/print/invoice/${inv.id}`);
  });
  main.querySelector("#del").addEventListener("click", async () => {
    if (!(await confirmDialog("Supprimer le brouillon ?", "<p>Le brouillon sera supprimé. Le devis d'origine redevient facturable.</p>", { ok: "Supprimer", danger: true }))) return;
    try { await api.del(`/api/invoices/${inv.id}`); dirty = false; toast("Brouillon supprimé", "ok"); location.hash = inv.quote_id ? `#/devis/${inv.quote_id}` : "#/factures"; } catch (e) { showError(e); }
  });
  main.querySelector("#issue").addEventListener("click", async () => {
    if (dirty && !(await save(true))) return;
    const t = computeTotals(editor.lines(), num("discount"), num("advance"));
    const n = String(nextNumber).padStart(4, "0");
    const ok = await confirmDialog(`Émettre ${noun} n°${n}`, `
      <p>Client : <b>${esc(main.querySelector("#custName").textContent)}</b><br>Total TTC : <b>${euros(t.ttc_cents)}</b>${t.advance_cents ? ` · Net à payer : <b>${euros(t.net_cents)}</b>` : ""}</p>
      <p>Après émission, ${noun} <b>ne pourra plus être modifiée ni supprimée</b> (obligation légale). Une erreur se corrige par un avoir.</p>`, { ok: `Émettre n°${n}` });
    if (!ok) return;
    try {
      toast("Émission et archivage PDF…");
      const r = await api.post(`/api/invoices/${inv.id}/issue`);
      if (r.warning) showError(new Error(r.warning)); else toast(`${r.invoice.label} ${isCredit ? "émis et archivé" : "émise et archivée"}`, "ok");
      dirty = false;
      invoiceView(inv.id);
    } catch (e) { showError(e); }
  });
}

function issuedInvoice(main, inv) {
  const isCredit = inv.kind === "credit_note";
  const legacy = inv.legacy;
  const remaining = inv.remaining_cents;
  main.innerHTML = `
    <div class="page-head"><h1>${esc(inv.label)} ${statusBadge(inv.status, isCredit ? "avoir" : "facture")} ${legacy ? '<span class="badge legacy">historique</span>' : ""}</h1>
      <div class="actions">
        <button class="btn" id="print" type="button">Imprimer</button>
        ${inv.pdf_path ? `<a class="btn" target="_blank" href="${fileURL("documents", inv.pdf_path)}">PDF archivé</a>` : legacy ? "" : `<button class="btn" id="pdf" type="button">Archiver en PDF</button>`}
        ${legacy && inv.legacy_source ? `<a class="btn" href="${fileURL("legacy", inv.legacy_source)}">Document Word d'origine</a>` : ""}
        ${!isCredit ? `<button class="btn danger" id="credit" type="button">Créer un avoir</button>` : ""}
      </div></div>
    ${legacy ? `<div class="alert info"><div class="grow">Facture importée de l'ancien ArtiMan (lecture seule).${inv.legacy_flags ? `<br><b>À vérifier :</b> ${esc(inv.legacy_flags)}` : ""}</div></div>` : ""}
    <div class="split">
      <iframe class="preview-frame" style="height:80vh" src="${withToken(`/print/invoice/${inv.id}?preview=1`)}"></iframe>
      <div>
        <div class="card"><h2>Résumé</h2><dl class="kv">
          <dt>Client</dt><dd><a href="#/clients/${inv.customer_id}">${esc(inv.party.display || inv.customer.display)}</a></dd>
          <dt>Date</dt><dd>${frDate(inv.date)}</dd>
          ${inv.due_date && !isCredit ? `<dt>Échéance</dt><dd>${frDate(inv.due_date)}</dd>` : ""}
          <dt>Total HT</dt><dd>${euros(inv.totals.ht_cents)}</dd>
          <dt>Total TTC</dt><dd><b>${euros(inv.totals.ttc_cents)}</b></dd>
          ${inv.advance_cents ? `<dt>Acompte</dt><dd>${euros(inv.advance_cents)}</dd>` : ""}
          ${!isCredit ? `<dt>Paiement</dt><dd>${inv.status === "paid" ? `Payée${inv.paid_at ? " le " + frDate(inv.paid_at) : ""}` : `<b>À encaisser : ${euros(Math.max(remaining, 0))}</b>`}</dd>` : ""}
          ${inv.quote_id ? `<dt>Devis</dt><dd><a href="#/devis/${inv.quote_id}">voir le devis</a></dd>` : ""}
          ${inv.credited_label ? `<dt>Annule</dt><dd><a href="#/factures/${inv.credits_invoice_id}">${esc(inv.credited_label)}</a></dd>` : ""}
          ${(inv.credit_notes || []).length ? `<dt>Avoirs</dt><dd>${inv.credit_notes.map(esc).join(", ")}</dd>` : ""}
          ${inv.issued_at && !legacy ? `<dt>Émise le</dt><dd>${esc(inv.issued_at)}</dd>` : ""}
        </dl></div>
        ${!isCredit && !legacy ? `<div class="card"><h2>Paiement</h2>
          ${inv.status === "paid"
            ? `<div class="alert ok">Facture payée${inv.paid_at ? " le " + frDate(inv.paid_at) : ""}.</div>
               <button class="btn sm" id="unpaid" type="button">Remettre « à encaisser »</button>`
            : `<form id="pay" style="display:flex;gap:8px;align-items:flex-end;flex-wrap:wrap">
                <label class="f">Payée le<input type="date" name="date" value="${todayISO()}"></label>
                <button class="btn primary" type="submit">Marquer comme payée</button></form>`}
          <p class="muted small" style="margin-top:8px">ArtiMan n'enregistre pas les montants encaissés : seulement le statut de la facture.</p>
        </div>` : ""}
      </div>
    </div>`;
  main.querySelector("#print").addEventListener("click", () => main.querySelector("iframe").contentWindow.print());
  const pdf = main.querySelector("#pdf");
  if (pdf) pdf.addEventListener("click", async () => {
    try { const r = await api.post(`/api/invoices/${inv.id}/pdf`); toast("PDF archivé", "ok"); window.open(fileURL("documents", r.pdf_path), "_blank"); invoiceView(inv.id); } catch (e) { showError(e); }
  });
  const credit = main.querySelector("#credit");
  if (credit) credit.addEventListener("click", async () => {
    if (!(await confirmDialog("Créer un avoir", `<p>Un avoir brouillon reprenant toutes les lignes de ${esc(inv.label)} en négatif va être créé.
      Vous pourrez l'ajuster (avoir partiel) avant de l'émettre.</p>`, { ok: "Créer l'avoir", danger: true }))) return;
    try { const r = await api.post(`/api/invoices/${inv.id}/credit-note`); location.hash = `#/factures/${r.id}`; } catch (e) { showError(e); }
  });
  const pay = main.querySelector("#pay");
  if (pay) pay.addEventListener("submit", async (e) => {
    e.preventDefault();
    try {
      await api.post(`/api/invoices/${inv.id}/paid`, { paid: true, date: new FormData(pay).get("date") });
      toast("Facture marquée payée", "ok");
      invoiceView(inv.id);
    } catch (err) { showError(err); }
  });
  const unpaid = main.querySelector("#unpaid");
  if (unpaid) unpaid.addEventListener("click", async () => {
    try { await api.post(`/api/invoices/${inv.id}/paid`, { paid: false }); invoiceView(inv.id); } catch (err) { showError(err); }
  });
}
