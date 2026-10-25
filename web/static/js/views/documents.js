import { api } from "../api.js";
import { esc, euros, euros0, frDate, typeBadge, statusBadge, docHref, debounce, downloadCSV, centsCSV } from "../util.js";
import { barChart } from "../ui.js";

function table(docs) {
  if (!docs.length) return `<p class="muted">Aucun document.</p>`;
  const shown = docs.slice(0, 600);
  return `<table class="list"><thead><tr><th>Date</th><th>Type</th><th>N°</th><th>Client</th><th>Objet</th><th class="num">HT</th><th class="num">TTC</th><th>Statut</th></tr></thead>
    <tbody>${shown.map((d) => `<tr class="click" data-href="${docHref(d)}"><td class="nowrap">${frDate(d.date)}</td><td>${typeBadge(d.type)}</td>
      <td class="nowrap">${esc(d.label)}</td><td>${esc(d.customer)}<div class="muted small">${esc(d.city)}</div></td>
      <td>${esc(d.title)}${d.legacy ? ' <span class="badge legacy">historique</span>' : ""}${d.flags ? ` <span class="badge warn" title="${esc(d.flags)}">!</span>` : ""}</td>
      <td class="num">${euros(d.ht_cents)}</td><td class="num">${euros(d.ttc_cents)}</td><td>${statusBadge(d.status, d.type)}</td></tr>`).join("")}</tbody></table>
    ${docs.length > 600 ? `<p class="muted small">${docs.length - 600} documents supplémentaires : affinez la recherche.</p>` : ""}`;
}

function bindRows(el) {
  el.querySelectorAll("tr[data-href]").forEach((tr) => tr.addEventListener("click", () => (location.hash = tr.dataset.href)));
}

function exportCSV(docs) {
  downloadCSV("artiman-documents.csv", [["date", "type", "numero", "client", "ville", "objet", "total_ht", "total_ttc", "statut", "historique"],
    ...docs.map((d) => [d.date, d.type, d.label, d.customer, d.city, d.title, centsCSV(d.ht_cents), centsCSV(d.ttc_cents), d.status, d.legacy ? "oui" : ""])]);
}

/** Quote list (type "devis") or invoice list (type "facture", credit notes included). */
export async function documentsView(type) {
  const main = document.getElementById("main");
  const isQuote = type === "devis";
  const statuses = isQuote
    ? [["", "Tous"], ["open", "En cours"]]
    : [["", "Toutes"], ["unpaid", "À encaisser"], ["open", "Brouillons"]];
  main.innerHTML = `
    <div class="page-head"><h1>${isQuote ? "Devis" : "Factures et avoirs"}</h1><div class="actions">
      ${isQuote ? `<a class="btn primary" href="#/devis/nouveau">+ Nouveau devis</a>` : `<a class="btn" href="#/factures/nouveau">+ Facture sans devis</a>`}</div></div>
    <div class="card">
      <div class="toolbar">
        <input type="search" id="q" placeholder="Client, ville, objet ou texte des lignes (ex. « lasure »)">
        <select id="status">${statuses.map(([v, l]) => `<option value="${v}">${l}</option>`).join("")}</select>
        <select id="year"><option value="">Toutes les années</option></select>
        <button class="btn" id="csv" type="button">Exporter CSV</button>
        <span class="muted small" id="count"></span>
      </div>
      <div class="scroll" style="max-height:70vh" id="res"></div>
    </div>`;
  const yearSel = main.querySelector("#year");
  for (let y = new Date().getFullYear(); y >= 2007; y--) yearSel.insertAdjacentHTML("beforeend", `<option>${y}</option>`);
  let docs = [];
  const load = async () => {
    const p = new URLSearchParams({ q: main.querySelector("#q").value, status: main.querySelector("#status").value, year: yearSel.value });
    p.set("type", isQuote ? "devis" : "");
    docs = await api.get("/api/documents?" + p);
    if (!isQuote) docs = docs.filter((d) => d.type !== "devis");
    main.querySelector("#res").innerHTML = table(docs);
    const sum = docs.filter((d) => d.status !== "draft").reduce((s, d) => s + d.ht_cents, 0);
    main.querySelector("#count").textContent = `${docs.length} document(s) · ${euros0(sum)} HT`;
    bindRows(main);
  };
  main.querySelector("#q").addEventListener("input", debounce(load, 250));
  main.querySelector("#status").addEventListener("change", load);
  yearSel.addEventListener("change", load);
  main.querySelector("#csv").addEventListener("click", () => exportCSV(docs));
  await load();
  main.querySelector("#q").focus();
}

/** Full history: yearly figures, chart, top customers and every document. */
export async function historyView() {
  const main = document.getElementById("main");
  const stats = await api.get("/api/stats");
  const params = new URLSearchParams(location.hash.split("?")[1] || "");
  main.innerHTML = `
    <div class="page-head"><h1>Historique</h1></div>
    <div class="tiles">
      <div class="card tile"><div class="v">${stats.customers}</div><div class="l">clients</div></div>
      <div class="card tile"><div class="v">${stats.quotes}</div><div class="l">devis</div></div>
      <div class="card tile"><div class="v">${stats.invoices}</div><div class="l">factures</div></div>
      <div class="card tile"><div class="v">${euros0(stats.total_cents)}</div><div class="l">CA facturé HT cumulé</div></div>
    </div>
    <div class="grid2">
      <div class="card"><h2>Devis et factures par année</h2>
        <div class="small muted" style="margin-bottom:6px"><span style="display:inline-block;width:10px;height:10px;background:#2a78d6;border-radius:2px"></span> Devis
        &nbsp; <span style="display:inline-block;width:10px;height:10px;background:#eb6834;border-radius:2px"></span> Factures</div>
        <div class="chart" id="chart"></div></div>
      <div class="card"><h2>Tableau annuel</h2><div class="scroll" style="max-height:300px"><table class="list"><thead><tr><th>Année</th><th class="num">Devis</th>
        <th class="num">Montant devisé HT</th><th class="num">Factures</th><th class="num">CA facturé HT</th><th class="num">Clients</th></tr></thead><tbody>
        ${stats.years.slice().reverse().map((y) => `<tr class="click" data-year="${y.year}"><td>${y.year}</td><td class="num">${y.quotes}</td><td class="num">${euros0(y.quoted_cents)}</td>
          <td class="num">${y.invoices}</td><td class="num">${euros0(y.invoiced_cents)}</td><td class="num">${y.customers}</td></tr>`).join("")}</tbody></table></div></div>
    </div>
    <div class="grid2" style="margin-top:14px">
      <div class="card"><h2>Meilleurs clients (CA facturé HT)</h2><table class="list"><tbody>
        ${stats.top_customers.map((c) => `<tr class="click" data-href="#/clients/${c.customer_id}"><td>${esc(c.customer)} <span class="muted">${esc(c.city)}</span></td>
          <td class="num">${c.label} fact.</td><td class="num">${euros0(c.ht_cents)}</td></tr>`).join("")}</tbody></table></div>
      <div class="card"><h2>Rechercher dans tout l'historique</h2>
        <div class="toolbar"><input type="search" id="q" placeholder="Client, ville, objet ou texte des lignes">
          <select id="type"><option value="">Tous</option><option value="devis">Devis</option><option value="facture">Factures</option><option value="avoir">Avoirs</option></select>
          <select id="year"><option value="">Toutes les années</option>${stats.years.slice().reverse().map((y) => `<option>${y.year}</option>`).join("")}</select>
          <button class="btn" id="csv" type="button">CSV</button></div>
        <div class="muted small" id="count"></div></div>
    </div>
    <div class="card" style="margin-top:14px"><div class="scroll" style="max-height:70vh" id="res"></div></div>`;
  barChart(main.querySelector("#chart"), stats.years.map((y) => ({ label: y.year, q: y.quotes, f: y.invoices })),
    [{ key: "q", label: "Devis", color: "#2a78d6" }, { key: "f", label: "Factures", color: "#eb6834" }],
    { axis: (v) => String(Math.round(v)), tip: (v) => String(v) }, (r) => { main.querySelector("#year").value = r.label; load(); });
  bindRows(main);
  main.querySelectorAll("tr[data-year]").forEach((tr) => tr.addEventListener("click", () => { main.querySelector("#year").value = tr.dataset.year; load(); }));
  let docs = [];
  const load = async () => {
    const p = new URLSearchParams({ q: main.querySelector("#q").value, type: main.querySelector("#type").value, year: main.querySelector("#year").value });
    docs = await api.get("/api/documents?" + p);
    main.querySelector("#res").innerHTML = table(docs);
    main.querySelector("#count").textContent = `${docs.length} document(s)`;
    bindRows(main.querySelector("#res"));
  };
  main.querySelector("#q").addEventListener("input", debounce(load, 250));
  main.querySelector("#type").addEventListener("change", load);
  main.querySelector("#year").addEventListener("change", load);
  main.querySelector("#csv").addEventListener("click", () => exportCSV(docs));
  if (params.get("year")) main.querySelector("#year").value = params.get("year");
  await load();
}
