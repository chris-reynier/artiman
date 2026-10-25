// Router, layout, heartbeat and global state.
import { api } from "./api.js";
import { toast, showError, confirmDialog } from "./ui.js";
import { homeView } from "./views/home.js";
import { customersView, customerView, customerFormView } from "./views/customers.js";
import { documentsView, historyView } from "./views/documents.js";
import { quoteView } from "./views/quote.js";
import { invoiceView } from "./views/invoice.js";
import { settingsView } from "./views/settings.js";
import { backupsView } from "./views/backups.js";
import { welcomeView, needsWelcome } from "./views/welcome.js";

export const state = { boot: null, leaveGuard: null };

/** A view may register a guard returning true when there are unsaved changes. */
export function setLeaveGuard(fn) { state.leaveGuard = fn; }

const routes = [
  [/^$/, "home", () => homeView()],
  [/^clients$/, "clients", () => customersView()],
  [/^clients\/nouveau$/, "clients", (m, q) => customerFormView(null, q)],
  [/^clients\/(\d+)$/, "clients", (m) => customerView(+m[1])],
  [/^devis$/, "devis", () => documentsView("devis")],
  [/^devis\/nouveau$/, "devis", (m, q) => quoteView(null, q)],
  [/^devis\/(\d+)$/, "devis", (m) => quoteView(+m[1])],
  [/^factures$/, "factures", () => documentsView("facture")],
  [/^factures\/nouveau$/, "factures", (m, q) => invoiceView(null, q)],
  [/^factures\/(\d+)$/, "factures", (m) => invoiceView(+m[1])],
  [/^historique$/, "historique", () => historyView()],
  [/^parametres$/, "parametres", () => settingsView()],
  [/^sauvegardes$/, "sauvegardes", () => backupsView()],
  [/^bienvenue$/, "", () => welcomeView()],
];

let current = location.hash;

async function render() {
  const raw = location.hash.replace(/^#\/?/, "");
  const [path, qs] = raw.split("?");
  const query = new URLSearchParams(qs || "");
  const main = document.getElementById("main");
  if (needsWelcome() && path !== "bienvenue" && path !== "parametres" && path !== "sauvegardes") {
    location.hash = "#/bienvenue";
    return;
  }
  for (const [re, nav, view] of routes) {
    const m = path.match(re);
    if (!m) continue;
    document.querySelectorAll("#nav a").forEach((a) => a.classList.toggle("on", a.dataset.nav === nav));
    state.leaveGuard = null;
    main.scrollTop = 0;
    try {
      await view(m, query);
    } catch (e) {
      main.innerHTML = `<div class="alert danger">${e.message}</div>`;
    }
    return;
  }
  location.hash = "#/";
}

window.addEventListener("hashchange", async () => {
  if (state.leaveGuard && state.leaveGuard() && location.hash !== current) {
    const target = location.hash;
    history.replaceState(null, "", current);
    const ok = await confirmDialog("Modifications non enregistrées", "Quitter cette page sans enregistrer ?", { ok: "Quitter sans enregistrer", danger: true });
    if (!ok) return;
    state.leaveGuard = null;
    history.replaceState(null, "", target);
  }
  current = location.hash;
  render();
});

window.addEventListener("beforeunload", (e) => {
  if (state.leaveGuard && state.leaveGuard()) { e.preventDefault(); e.returnValue = ""; }
});

/** Re-reads settings and warnings (after the settings page is saved). */
export async function reloadBoot() {
  state.boot = await api.get("/api/bootstrap");
  updateBackupDot(state.boot.backup);
  if (state.boot.demo && !document.getElementById("demoBadge")) {
    document.title = "ArtiMan (démonstration)";
    document.querySelector(".brand").insertAdjacentHTML("afterend",
      '<div id="demoBadge" style="background:#eda100;color:#1b1400;border-radius:6px;padding:6px 8px;font-size:12px;margin:0 2px 10px;line-height:1.3">' +
      '<b>DÉMONSTRATION</b><br>Données fictives : vos vraies données ne sont pas concernées.</div>');
  }
  return state.boot;
}

function updateBackupDot(b) {
  const dot = document.getElementById("backupDot");
  dot.classList.toggle("hidden", !(b && (b.alert || b.external_late)));
  dot.style.background = b && b.alert ? "#c63b37" : "#f0a020";
}

document.getElementById("quitBtn").addEventListener("click", async () => {
  if (state.leaveGuard && state.leaveGuard()) {
    const ok = await confirmDialog("Modifications non enregistrées", "Quitter ArtiMan sans enregistrer ?", { ok: "Quitter", danger: true });
    if (!ok) return;
  }
  state.leaveGuard = null;
  try { await api.post("/api/quit"); } catch { /* already stopped */ }
  document.getElementById("main").innerHTML = `<div class="card" style="max-width:520px;margin:60px auto;text-align:center">
    <h2>ArtiMan est fermé</h2><p>Les données ont été sauvegardées. Vous pouvez fermer cette fenêtre.</p></div>`;
  setTimeout(() => window.close(), 400);
});

// Heartbeat: tells the server the window is still open; also refreshes the backup indicator.
setInterval(async () => {
  try { const r = await api.post("/api/ping"); updateBackupDot(r.backup); } catch { /* server stopped */ }
}, 20000);

document.addEventListener("keydown", (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
    const btn = document.querySelector("[data-save]");
    if (btn) { e.preventDefault(); btn.click(); }
  }
});

(async () => {
  try {
    await reloadBoot();
    api.post("/api/ping").catch(() => {});
    if (!location.hash) history.replaceState(null, "", "#/");
    current = location.hash;
    render();
  } catch (e) {
    showError(e);
  }
})();

export { toast };
