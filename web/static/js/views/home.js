import { api } from "../api.js";
import { state } from "../app.js";
import { esc, euros, euros0, frDate, typeBadge, statusBadge, docHref } from "../util.js";
import { barChart } from "../ui.js";

export async function homeView() {
  const main = document.getElementById("main");
  const [stats, unpaid, open, recent] = await Promise.all([
    api.get("/api/stats"),
    api.get("/api/documents?status=unpaid"),
    api.get("/api/documents?type=devis&status=open"),
    api.get("/api/documents?limit=12"),
  ]);
  const boot = state.boot;
  const year = boot.today.slice(0, 4);
  const yearRow = (y) => stats.years.find((r) => r.year === y) || { invoiced_cents: 0, invoices: 0, quotes: 0 };
  const cur = yearRow(year), prev = yearRow(String(+year - 1));
  const b = boot.backup;

  let alerts = "";
  if (b.alert) alerts += `<div class="alert danger"><div class="grow"><b>Attention :</b> ${esc(b.alert)}</div><a class="btn sm" href="#/sauvegardes">Sauvegardes</a></div>`;
  for (const w of boot.warnings) alerts += `<div class="alert warn"><div class="grow">${esc(w)}</div><a class="btn sm" href="#/parametres">Paramètres</a></div>`;
  if (b.external_late) {
    alerts += `<div class="alert info"><div class="grow">${b.external_dir ? "Aucune copie externe depuis plus de 7 jours : branchez la clé USB ou vérifiez le dossier de sauvegarde externe."
      : "Aucune copie de sauvegarde hors de ce PC : choisissez une clé USB ou un dossier OneDrive."}</div><a class="btn sm" href="#/sauvegardes">Configurer</a></div>`;
  }
  if (!boot.pdf) alerts += `<div class="alert warn">Microsoft Edge est introuvable : les PDF ne pourront pas être archivés automatiquement (l'impression reste possible).</div>`;

  const docRow = (d) => `<tr class="click" data-href="${docHref(d)}"><td class="nowrap">${frDate(d.date)}</td><td>${typeBadge(d.type)}</td>
    <td>${esc(d.customer)}<div class="muted small">${esc(d.title)}</div></td><td class="num">${euros(d.ttc_cents)}</td><td>${statusBadge(d.status, d.type)}</td></tr>`;

  main.innerHTML = `
    <div class="page-head"><h1>Accueil</h1><div class="actions">
      <a class="btn" href="#/clients/nouveau">+ Nouveau client</a><a class="btn primary" href="#/devis/nouveau">+ Nouveau devis</a></div></div>
    ${alerts}
    <div class="tiles">
      <div class="card tile"><div class="v">${euros0(cur.invoiced_cents)}</div><div class="l">Facturé HT en ${year}</div></div>
      <div class="card tile"><div class="v">${euros0(prev.invoiced_cents)}</div><div class="l">Facturé HT en ${+year - 1}</div></div>
      <a class="card tile" href="#/factures"><div class="v">${stats.unpaid_count}</div><div class="l">Factures à encaisser · ${euros0(stats.unpaid_cents)} TTC</div></a>
      <a class="card tile" href="#/devis"><div class="v">${stats.open_quotes}</div><div class="l">Devis en cours · ${euros0(stats.open_cents)} HT</div></a>
      <div class="card tile"><div class="v">${stats.customers}</div><div class="l">Clients</div></div>
    </div>
    <div class="grid2">
      <div class="card"><h2>Factures à encaisser</h2>${unpaid.length ? `<div class="scroll" style="max-height:300px"><table class="list"><tbody>${unpaid.map(docRow).join("")}</tbody></table></div>`
        : `<p class="muted">Aucune facture en attente de règlement.</p>`}</div>
      <div class="card"><h2>Devis en cours</h2>${open.length ? `<div class="scroll" style="max-height:300px"><table class="list"><tbody>${open.map(docRow).join("")}</tbody></table></div>`
        : `<p class="muted">Aucun devis en cours. <a href="#/devis/nouveau">Créer un devis</a></p>`}</div>
    </div>
    <div class="grid2" style="margin-top:14px">
      <div class="card"><h2>Chiffre d'affaires facturé HT par année</h2><div class="chart" id="chartCA"></div></div>
      <div class="card"><h2>Derniers documents</h2><div class="scroll" style="max-height:300px"><table class="list"><tbody>${recent.map(docRow).join("")}</tbody></table></div></div>
    </div>`;
  main.querySelectorAll("tr[data-href]").forEach((tr) => tr.addEventListener("click", () => (location.hash = tr.dataset.href)));
  barChart(main.querySelector("#chartCA"), stats.years.slice(-12).map((y) => ({ label: y.year, v: y.invoiced_cents })),
    [{ key: "v", label: "CA HT", color: "#2a78d6" }],
    { axis: (v) => (v >= 100000 ? Math.round(v / 100000) + " k€" : Math.round(v / 100) + " €"), tip: euros0 },
    (r) => (location.hash = `#/historique?year=${r.label}`));
}
