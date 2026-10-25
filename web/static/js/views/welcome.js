import { api, withToken } from "../api.js";
import { state, reloadBoot } from "../app.js";
import { esc } from "../util.js";
import { toast, showError } from "../ui.js";

/** First-run assistant: shown while the company identity is missing. */
export async function welcomeView() {
  const main = document.getElementById("main");
  const { settings: s } = await api.get("/api/settings");
  const v = (k) => esc(s[k] || "");
  main.innerHTML = `
    <div style="max-width:860px;margin:0 auto">
    <div class="page-head"><h1>Bienvenue dans ArtiMan</h1></div>
    <p>Quelques informations pour préparer vos devis et factures. Elles s'impriment sur chaque document et restent modifiables dans les
      <b>Paramètres</b>. Vos données restent sur cet ordinateur.</p>
    <form id="f">
    <div class="card"><h2>1. Votre entreprise</h2><div class="form-grid">
      <label class="f span2">Nom de l'entreprise ou votre nom *<input name="company_name" value="${v("company_name")}" required placeholder="ex. Dupont Peinture"></label>
      <label class="f">Ville (pour « Ville le … »)<input name="company_city" value="${v("company_city")}"></label>
      <label class="f wide">Adresse (une ligne par ligne imprimée) *<textarea name="company_address" rows="2" required placeholder="12 rue des Lilas&#10;73000 CHAMBERY">${v("company_address")}</textarea></label>
      <label class="f">Téléphone<input name="company_phone" value="${v("company_phone")}"></label>
      <label class="f">Autre téléphone<input name="company_phone2" value="${v("company_phone2")}"></label>
      <label class="f">Email<input name="company_email" type="email" value="${v("company_email")}"></label>
      <label class="f">SIRET *<input name="company_siret" value="${v("company_siret")}" required placeholder="14 chiffres"></label>
      <label class="f">Code APE<input name="company_ape" value="${v("company_ape")}" placeholder="ex. 4334 Z"></label>
      <label class="f wide">Activités (imprimées sous le nom si vous n'avez pas de logo)<textarea name="company_trades" rows="2" placeholder="Peinture&#10;Revêtements muraux">${v("company_trades")}</textarea></label>
      <div class="wide" style="display:flex;gap:12px;align-items:center">
        <label class="btn">Ajouter mon logo (PNG, facultatif)<input type="file" accept="image/png" id="logo" hidden></label>
        <span class="muted small" id="logoState">Sans logo, le nom de l'entreprise est imprimé en en-tête.</span>
      </div>
    </div></div>

    <div class="card"><h2>2. TVA</h2>
      <label class="check"><input type="radio" name="vat_regime" value="normal" ${s.vat_regime !== "franchise" ? "checked" : ""}>
        Je facture la TVA (société, entreprise individuelle au réel)</label>
      <label class="check" style="margin-top:6px"><input type="radio" name="vat_regime" value="franchise" ${s.vat_regime === "franchise" ? "checked" : ""}>
        Je suis en franchise de TVA (micro-entreprise : « TVA non applicable, art. 293 B du CGI »)</label>
    </div>

    <div class="card"><h2>3. Assurance décennale</h2>
      <label class="f">Assureur, n° de contrat, zone couverte
        <input name="insurance" value="${v("insurance")}" placeholder="ex. MAAF Assurances, contrat n° 123456, France métropolitaine"></label>
      <p class="muted small">Obligatoire sur les devis et factures des artisans du bâtiment. Vous pouvez la compléter plus tard.</p>
    </div>

    <div class="card"><h2>4. Numérotation des factures</h2>
      <label class="check"><input type="radio" name="numbering" value="new" checked> Je commence : ma première facture portera le n°0001</label>
      <label class="check" style="margin-top:6px"><input type="radio" name="numbering" value="continue"> J'ai déjà des factures : la prochaine portera le n°
        <input name="invoice_start" type="number" min="1" value="${s.invoice_start !== "1" ? v("invoice_start") : ""}" style="width:110px"></label>
      <p class="muted small">La loi impose une numérotation continue, sans trou : reprenez la suite de votre dernière facture.</p>
    </div>

    <div class="card"><h2>5. Sauvegarde hors de cet ordinateur (conseillé)</h2>
      <label class="f">Dossier d'une clé USB ou de OneDrive
        <input name="backup_external_dir" value="${v("backup_external_dir")}" placeholder="ex. E:\\  ou  C:\\Users\\Nom\\OneDrive\\ArtiMan"></label>
      <p class="muted small">ArtiMan y recopie ses sauvegardes automatiquement quand le dossier est disponible.</p>
    </div>

    <div class="modal-foot" style="justify-content:space-between;align-items:center">
      <span class="muted small">Pour essayer sans rien saisir, fermez ArtiMan et lancez « Essayer la démo » dans le dossier du programme.</span>
      <button class="btn primary" type="submit">Commencer</button>
    </div>
    </form></div>`;

  const form = main.querySelector("#f");
  main.querySelector("#logo").addEventListener("change", async (e) => {
    const file = e.target.files[0];
    if (!file) return;
    try {
      const res = await fetch(withToken("/api/logo"), { method: "POST", body: file, headers: { "X-Token": window.ARTIMAN.token } });
      if (!res.ok) throw new Error((await res.json()).error);
      main.querySelector("#logoState").textContent = "Logo enregistré ✓";
    } catch (err) { showError(err); }
  });
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const fd = new FormData(form);
    const body = {};
    for (const k of ["company_name", "company_city", "company_address", "company_phone", "company_phone2", "company_email", "company_siret",
      "company_ape", "company_trades", "vat_regime", "insurance", "backup_external_dir"]) body[k] = String(fd.get(k) || "").trim();
    const siret = body.company_siret.replace(/\s/g, "");
    if (!/^\d{14}$/.test(siret)) { showError(new Error("Le SIRET doit comporter 14 chiffres.")); return; }
    body.invoice_start = fd.get("numbering") === "continue" ? String(fd.get("invoice_start") || "") : "1";
    if (!/^\d+$/.test(body.invoice_start) || +body.invoice_start < 1) { showError(new Error("Numéro de la prochaine facture invalide.")); return; }
    try {
      await api.put("/api/settings", body);
      await reloadBoot();
      toast("C'est prêt ! Vous pouvez faire votre premier devis.", "ok");
      location.hash = "#/";
    } catch (err) { showError(err); }
  });
}

/** True when the first-run assistant must be shown. */
export const needsWelcome = () => !String((state.boot && state.boot.settings.company_name) || "").trim();
