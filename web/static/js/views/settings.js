import { api, withToken } from "../api.js";
import { reloadBoot, setLeaveGuard } from "../app.js";
import { esc } from "../util.js";
import { toast, showError } from "../ui.js";

const field = (s, key, label, { type = "text", rows = 0, hint = "", wide = false } = {}) => `
  <label class="f ${wide || rows ? "wide" : ""}">${label}
    ${rows ? `<textarea name="${key}" rows="${rows}">${esc(s[key])}</textarea>` : `<input name="${key}" type="${type}" value="${esc(s[key])}">`}
    ${hint ? `<span class="muted small">${hint}</span>` : ""}</label>`;

export async function settingsView() {
  const main = document.getElementById("main");
  const { settings: s, next_number, issued } = await api.get("/api/settings");
  main.innerHTML = `
    <div class="page-head sticky-bar"><h1>Paramètres</h1><div class="actions"><button class="btn primary" data-save type="button">Enregistrer</button></div></div>
    <form id="f">
    <div class="card"><h2>Entreprise</h2><div class="form-grid">
      ${field(s, "company_name", "Nom")}
      ${field(s, "company_city", "Ville (pour « Ville le … »)")}
      ${field(s, "company_phone", "Téléphone 1")}
      ${field(s, "company_phone2", "Téléphone 2")}
      ${field(s, "company_email", "Email")}
      ${field(s, "company_siret", "SIRET")}
      ${field(s, "company_ape", "Code APE")}
      ${field(s, "company_vat_number", "N° de TVA intracommunautaire", { hint: "Facultatif" })}
      ${field(s, "company_address", "Adresse (une ligne par ligne imprimée)", { rows: 2 })}
      ${field(s, "company_trades", "Activités (une par ligne)", { rows: 2, hint: "Imprimées sous le nom de l'entreprise quand aucun logo n'est installé." })}
      <div class="wide" style="display:flex;gap:14px;align-items:center">
        <img src="/logo?${Date.now()}" alt="logo" style="height:70px;border:1px solid var(--border);border-radius:6px;background:#fff;padding:4px"
          onerror="this.style.display='none'; this.nextElementSibling.style.display='inline'">
        <span class="muted small" style="display:none">Aucun logo : le nom et les activités sont imprimés à la place.</span>
        <label class="btn">Changer le logo (PNG)<input type="file" accept="image/png" id="logo" hidden></label>
      </div>
    </div></div>

    <div class="card"><h2>Mentions légales <span class="muted small">— à vérifier avec votre comptable</span></h2><div class="form-grid">
      ${field(s, "insurance", "Assurance décennale (assureur, n° de contrat, zone couverte)", { wide: true, hint: "Obligatoire pour les artisans du bâtiment. Ex. : MAAF Assurances, contrat n° 123456, couverture France métropolitaine." })}
      ${field(s, "payment_days", "Délai de paiement (jours, 0 = à réception)", { type: "number" })}
      ${field(s, "payment_terms", "Conditions de règlement (imprimées)", { wide: true })}
      ${field(s, "late_penalty", "Pénalités de retard", { rows: 2 })}
      ${field(s, "recovery_fee", "Indemnité forfaitaire (clients professionnels)", { rows: 2 })}
      ${field(s, "no_discount_terms", "Escompte", { wide: true })}
      <label class="check wide"><input type="checkbox" id="legalok" ${s.legal_todo === "1" ? "" : "checked"}> Mentions vérifiées (masque le rappel sur l'accueil)</label>
    </div></div>

    <div class="card"><h2>TVA</h2><div class="form-grid">
      <label class="f wide">Régime de TVA
        <select name="vat_regime">
          <option value="normal" ${s.vat_regime !== "franchise" ? "selected" : ""}>Assujetti à la TVA (société, entreprise individuelle au réel)</option>
          <option value="franchise" ${s.vat_regime === "franchise" ? "selected" : ""}>Franchise en base de TVA (micro-entreprise : TVA non applicable)</option>
        </select>
        <span class="muted small">En franchise, les devis et factures sont établis sans TVA avec la mention ci-dessous.</span></label>
      ${field(s, "vat_franchise_note", "Mention de franchise", { wide: true })}
      ${field(s, "reduced_vat_note", "Mention pour la TVA à taux réduit (5,5 % / 10 %)", { rows: 2, hint: "Imprimée quand une ligne est à 5,5 % ou 10 % (travaux dans un logement de plus de 2 ans). Le texte exact est à valider avec votre comptable." })}
    </div></div>

    <div class="card"><h2>Devis et factures</h2><div class="form-grid">
      ${field(s, "quote_validity_days", "Validité des devis (jours)", { type: "number" })}
      ${field(s, "default_vat_bp", "TVA par défaut (points de base : 1000 = 10 %)", { type: "number" })}
      ${field(s, "vat_rates_bp", "Taux de TVA proposés (points de base)", { hint: "550 = 5,5 % · 1000 = 10 % · 2000 = 20 % · 0 = autoliquidation" })}
      ${field(s, "units", "Unités proposées (séparées par des virgules)")}
      ${field(s, "deposit_terms", "Conditions d'acompte par défaut (devis)", { rows: 2 })}
      <label class="f">Numéro de la prochaine facture
        <input name="invoice_start" type="number" value="${esc(issued ? next_number : s.invoice_start)}" ${issued ? "readonly" : ""}>
        <span class="muted small">${issued ? "Des factures ont été émises : la numérotation continue automatiquement (n° " + next_number + ")." : "Si vous aviez déjà des factures, indiquez le numéro qui suit la dernière, pour garder une numérotation continue."}</span></label>
    </div></div>

    <div class="card"><h2>Sauvegardes</h2><div class="form-grid">
      ${field(s, "backup_external_dir", "Dossier de copie externe (clé USB, OneDrive…)", { wide: true, hint: "Ex. : E:\\ ou C:\\Users\\Nom\\OneDrive\\ArtiMan. Une copie y est faite après chaque sauvegarde quand il est disponible." })}
      ${field(s, "backup_keep", "Nombre de sauvegardes récentes conservées", { type: "number", hint: "En plus : une par mois et la plus complète sont toujours gardées." })}
    </div></div>
    </form>
    <p class="muted small" style="margin-top:14px">ArtiMan v${esc(window.ARTIMAN.version)} est un logiciel libre, gratuit et fourni sans garantie (licence MIT).
      Vous restez responsable de vos documents commerciaux : faites vérifier vos mentions légales par votre comptable.</p>`;
  const form = main.querySelector("#f");
  let dirty = false;
  form.addEventListener("input", () => (dirty = true));
  setLeaveGuard(() => dirty);
  main.querySelector("[data-save]").addEventListener("click", async () => {
    const body = {};
    for (const el of form.querySelectorAll("[name]")) if (!el.readOnly) body[el.name] = el.value;
    body.legal_todo = main.querySelector("#legalok").checked ? "0" : "1";
    try {
      await api.put("/api/settings", body);
      dirty = false;
      await reloadBoot();
      toast("Paramètres enregistrés", "ok");
    } catch (e) { showError(e); }
  });
  main.querySelector("#logo").addEventListener("change", async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    try {
      const res = await fetch(withToken("/api/logo"), { method: "POST", body: file, headers: { "X-Token": window.ARTIMAN.token } });
      if (!res.ok) throw new Error((await res.json()).error);
      toast("Logo remplacé", "ok");
      settingsView();
    } catch (err) { showError(err); }
  });
}
