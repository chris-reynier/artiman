import { api } from "../api.js";
import { reloadBoot } from "../app.js";
import { esc } from "../util.js";
import { toast, showError, confirmDialog } from "../ui.js";

const fmtTime = (t) => (!t || t.startsWith("0001") ? "jamais" : new Date(t).toLocaleString("fr-FR", { dateStyle: "short", timeStyle: "short" }));
const size = (b) => (b > 1e6 ? (b / 1e6).toFixed(1) + " Mo" : Math.round(b / 1e3) + " Ko");
const REASONS = { demarrage: "Démarrage", fermeture: "Fermeture", auto: "Automatique", manuelle: "Manuelle", emission: "Émission de facture",
  "avant-restauration": "Avant restauration", "avant-mise-a-jour": "Avant mise à jour" };

export async function backupsView() {
  const main = document.getElementById("main");
  const { status: st, backups } = await api.get("/api/backups");
  main.innerHTML = `
    <div class="page-head"><h1>Sauvegardes</h1><div class="actions">
      <button class="btn primary" id="now" type="button">Sauvegarder maintenant</button>
      <button class="btn" id="ext" type="button" ${st.external_dir ? "" : "disabled"}>Copier vers le dossier externe</button>
      <button class="btn" id="open" type="button">Ouvrir le dossier</button></div></div>
    ${st.alert ? `<div class="alert danger"><b>Attention :</b>&nbsp;${esc(st.alert)}</div>` : ""}
    <div class="grid2">
      <div class="card"><h2>État</h2><dl class="kv">
        <dt>Dernière sauvegarde</dt><dd>${fmtTime(st.last_backup)}</dd>
        <dt>Sauvegardes sur ce PC</dt><dd>${st.count} <span class="muted small">(${esc(st.dir)})</span></dd>
        <dt>Dossier externe</dt><dd>${st.external_dir ? esc(st.external_dir) + (st.external_ok ? ' <span class="badge paid">disponible</span>' : ' <span class="badge refused">non disponible</span>') : '<span class="muted">non configuré</span>'}</dd>
        <dt>Dernière copie externe</dt><dd>${fmtTime(st.last_external)} ${st.external_late ? '<span class="badge warn">à faire</span>' : ""}</dd>
      </dl>
      <p class="muted small">Une sauvegarde est faite au démarrage, à la fermeture, à chaque émission de facture et toutes les 6 heures si la base a changé.
        Le dossier externe se règle dans les <a href="#/parametres">Paramètres</a>.</p></div>
      <div class="card"><h2>Restaurer</h2><p>Remplace les données actuelles par une sauvegarde. Une copie de l'état actuel est faite juste avant, la restauration est donc réversible.</p>
        <p class="muted small">Choisissez la sauvegarde dans la liste ci-dessous.</p></div>
    </div>
    <div class="card" style="margin-top:14px"><h2>Sauvegardes disponibles</h2><div class="scroll"><table class="list"><thead><tr><th>Date</th><th>Origine</th><th class="num">Taille</th><th></th></tr></thead>
      <tbody>${backups.map((b) => `<tr><td>${fmtTime(b.time)}</td><td>${esc(REASONS[b.reason] || b.reason)}</td><td class="num">${size(b.size)}</td>
        <td class="num"><button class="btn sm" data-restore="${esc(b.name)}" type="button">Restaurer</button></td></tr>`).join("")}</tbody></table></div></div>`;
  main.querySelector("#now").addEventListener("click", async () => {
    try { await api.post("/api/backups"); toast("Sauvegarde effectuée", "ok"); await reloadBoot(); backupsView(); } catch (e) { showError(e); }
  });
  main.querySelector("#ext").addEventListener("click", async () => {
    try { await api.post("/api/backups/external"); toast("Copie externe effectuée", "ok"); await reloadBoot(); backupsView(); } catch (e) { showError(e); }
  });
  main.querySelector("#open").addEventListener("click", () => api.post("/api/open-folder", { what: "backups" }).catch(showError));
  main.querySelectorAll("[data-restore]").forEach((b) => b.addEventListener("click", async () => {
    if (!(await confirmDialog("Restaurer cette sauvegarde ?", `<p>Les données actuelles seront remplacées par la sauvegarde <b>${esc(b.dataset.restore)}</b>.</p>
      <p>L'état actuel est sauvegardé juste avant (origine « Avant restauration »).</p>`, { ok: "Restaurer", danger: true }))) return;
    try { await api.post("/api/backups/restore", { name: b.dataset.restore }); toast("Sauvegarde restaurée", "ok"); await reloadBoot(); location.hash = "#/"; } catch (e) { showError(e); }
  }));
}
