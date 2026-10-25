import { api } from "../api.js";
import { setLeaveGuard } from "../app.js";
import { esc, euros, euros0, frDate, norm, typeBadge, statusBadge, docHref, downloadCSV } from "../util.js";
import { toast, showError, confirmDialog } from "../ui.js";

const CIVILITIES = ["M.", "Mme", "M. et Mme", "Mlle", ""];
const civLabel = (c) => c || "Société / autre";

export async function customersView() {
  const main = document.getElementById("main");
  const all = await api.get("/api/customers");
  main.innerHTML = `
    <div class="page-head"><h1>Clients <span class="muted small">(${all.length})</span></h1>
      <div class="actions">
        <label class="btn" title="Fichier CSV avec une ligne de titres : Nom, Prénom, Adresse, Code postal, Ville, Téléphone, Email…">Importer (CSV)<input type="file" accept=".csv,text/csv" id="csvIn" hidden></label>
        <button class="btn" id="csvOut" type="button">Exporter (CSV)</button>
        <a class="btn primary" href="#/clients/nouveau">+ Nouveau client</a></div></div>
    <div class="card">
      <div class="toolbar"><input type="search" id="q" placeholder="Rechercher un nom, une ville…" autofocus>
        <span class="muted small" id="count"></span></div>
      <div class="scroll" style="max-height:72vh"><table class="list"><thead><tr><th>Client</th><th>Ville</th><th class="num">Documents</th>
        <th class="num">Facturé HT</th><th>Dernier document</th></tr></thead><tbody id="rows"></tbody></table></div>
    </div>`;
  const q = main.querySelector("#q");
  const draw = () => {
    const s = norm(q.value);
    const list = all.filter((c) => !s || norm(c.display + " " + c.city + " " + c.zip + " " + c.phone + " " + c.email).includes(s));
    main.querySelector("#count").textContent = `${list.length} client(s)`;
    main.querySelector("#rows").innerHTML = list.slice(0, 500).map((c) => `<tr class="click" data-id="${c.id}">
      <td><b>${esc(c.display)}</b>${c.is_business ? ' <span class="badge">pro</span>' : ""}</td><td>${esc(c.city)}</td>
      <td class="num">${c.doc_count}</td><td class="num">${euros0(c.invoiced_cents)}</td><td class="nowrap">${frDate(c.last_date)}</td></tr>`).join("");
  };
  q.addEventListener("input", draw);
  main.querySelector("#csvOut").addEventListener("click", () => downloadCSV("clients.csv", [
    ["Civilité", "Nom", "Prénom", "Adresse", "Code postal", "Ville", "Téléphone", "Portable", "Email", "Professionnel", "Notes"],
    ...all.map((c) => [c.civility, c.last_name, c.first_name, c.address.replace(/\n/g, ", "), c.zip, c.city, c.phone, c.mobile, c.email, c.is_business ? "oui" : "", c.notes]),
  ]));
  main.querySelector("#csvIn").addEventListener("change", async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    try {
      const res = await fetch("/api/customers/import", { method: "POST", body: file, headers: { "X-Token": window.ARTIMAN.token } });
      const r = await res.json();
      if (!res.ok) throw new Error(r.error);
      toast(`${r.created} client(s) importé(s)${r.skipped ? `, ${r.skipped} déjà présent(s) ignoré(s)` : ""}`, "ok");
      customersView();
    } catch (err) { showError(err); }
  });
  main.querySelector("#rows").addEventListener("click", (e) => {
    const tr = e.target.closest("tr[data-id]");
    if (tr) location.hash = `#/clients/${tr.dataset.id}`;
  });
  draw();
}

function formHTML(c) {
  return `
    <div class="form-grid">
      <label class="f">Civilité<select name="civility">${CIVILITIES.map((v) => `<option value="${esc(v)}" ${v === c.civility ? "selected" : ""}>${esc(civLabel(v))}</option>`).join("")}</select></label>
      <label class="f">Nom * <input name="last_name" value="${esc(c.last_name)}" required></label>
      <label class="f">Prénom<input name="first_name" value="${esc(c.first_name)}"></label>
      <label class="f wide">Adresse<textarea name="address" rows="2">${esc(c.address)}</textarea></label>
      <label class="f">Code postal<input name="zip" value="${esc(c.zip)}"></label>
      <label class="f span2">Ville<input name="city" value="${esc(c.city)}"></label>
      <label class="f">Téléphone<input name="phone" value="${esc(c.phone)}"></label>
      <label class="f">Portable<input name="mobile" value="${esc(c.mobile)}"></label>
      <label class="f">Email<input name="email" type="email" value="${esc(c.email)}"></label>
      <label class="check wide"><input type="checkbox" name="is_business" ${c.is_business ? "checked" : ""}> Client professionnel (entreprise, syndic, mairie…)</label>
      <label class="check wide"><input type="checkbox" name="archived" ${c.archived ? "checked" : ""}> Archivé (masqué de la liste)</label>
      <label class="f wide">Notes internes<textarea name="notes" rows="2">${esc(c.notes)}</textarea></label>
    </div>`;
}

function readForm(form, base) {
  const fd = new FormData(form);
  const c = { ...base };
  for (const k of ["civility", "last_name", "first_name", "address", "zip", "city", "phone", "mobile", "email", "notes"]) c[k] = fd.get(k) || "";
  c.is_business = fd.get("is_business") === "on";
  c.archived = fd.get("archived") === "on";
  return c;
}

export async function customerFormView(id, query) {
  const main = document.getElementById("main");
  const c = { civility: "M.", last_name: "", first_name: "", address: "", zip: "", city: "", phone: "", mobile: "", email: "", notes: "", is_business: false, archived: false };
  main.innerHTML = `
    <div class="page-head"><h1>Nouveau client</h1></div>
    <form class="card" id="f">${formHTML(c)}
      <div class="modal-foot"><a class="btn" href="#/clients">Annuler</a><button class="btn primary" data-save type="submit">Enregistrer</button></div></form>`;
  const form = main.querySelector("#f");
  let dirty = false;
  form.addEventListener("input", () => (dirty = true));
  setLeaveGuard(() => dirty);
  form.querySelector("[name=last_name]").focus();
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    try {
      const r = await api.post("/api/customers", readForm(form, c));
      dirty = false;
      toast("Client enregistré", "ok");
      const next = query && query.get("next");
      location.hash = next === "devis" ? `#/devis/nouveau?client=${r.id}` : `#/clients/${r.id}`;
    } catch (err) { showError(err); }
  });
}

export async function customerView(id) {
  const main = document.getElementById("main");
  const { customer: c, documents } = await api.get(`/api/customers/${id}`);
  const invoiced = documents.filter((d) => d.type !== "devis" && d.status !== "draft").reduce((s, d) => s + d.ht_cents, 0);
  main.innerHTML = `
    <div class="page-head"><h1>${esc(c.display)}</h1><div class="actions">
      <a class="btn primary" href="#/devis/nouveau?client=${c.id}">+ Nouveau devis</a>
      <a class="btn" href="#/factures/nouveau?client=${c.id}">+ Facture sans devis</a></div></div>
    <div class="split">
      <div class="card"><h2>Documents <span class="muted small">(${documents.length} · ${euros0(invoiced)} facturés HT)</span></h2>
        ${documents.length ? `<div class="scroll"><table class="list"><thead><tr><th>Date</th><th>Type</th><th>N°</th><th>Objet</th><th class="num">HT</th><th class="num">TTC</th><th>Statut</th></tr></thead>
        <tbody>${documents.map((d) => `<tr class="click" data-href="${docHref(d)}"><td class="nowrap">${frDate(d.date)}</td><td>${typeBadge(d.type)}</td>
          <td class="nowrap">${esc(d.label)}</td><td>${esc(d.title)}${d.legacy ? ' <span class="badge legacy">historique</span>' : ""}${d.flags ? ' <span class="badge warn" title="' + esc(d.flags) + '">!</span>' : ""}</td>
          <td class="num">${euros(d.ht_cents)}</td><td class="num">${euros(d.ttc_cents)}</td><td>${statusBadge(d.status, d.type)}</td></tr>`).join("")}</tbody></table></div>`
          : `<p class="muted">Aucun document pour ce client.</p>`}
      </div>
      <form class="card" id="f"><h2>Coordonnées</h2>${formHTML(c)}
        <div class="modal-foot" style="justify-content:space-between"><button class="btn danger" type="button" id="del">Supprimer</button>
          <button class="btn primary" data-save type="submit">Enregistrer</button></div></form>
    </div>`;
  main.querySelectorAll("tr[data-href]").forEach((tr) => tr.addEventListener("click", () => (location.hash = tr.dataset.href)));
  const form = main.querySelector("#f");
  form.querySelectorAll(".form-grid").forEach((g) => (g.style.gridTemplateColumns = "1fr 1fr"));
  form.querySelectorAll(".span2").forEach((x) => (x.style.gridColumn = "span 1"));
  let dirty = false;
  form.addEventListener("input", () => (dirty = true));
  setLeaveGuard(() => dirty);
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    try {
      await api.put(`/api/customers/${c.id}`, readForm(form, c));
      dirty = false;
      toast("Client enregistré", "ok");
      customerView(id);
    } catch (err) { showError(err); }
  });
  main.querySelector("#del").addEventListener("click", async () => {
    if (!(await confirmDialog("Supprimer ce client ?", `<p>${esc(c.display)} sera définitivement supprimé.</p>`, { ok: "Supprimer", danger: true }))) return;
    try {
      await api.del(`/api/customers/${c.id}`);
      dirty = false;
      toast("Client supprimé", "ok");
      location.hash = "#/clients";
    } catch (err) { showError(err); }
  });
}
