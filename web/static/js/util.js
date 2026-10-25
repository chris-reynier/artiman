// Shared helpers: formatting, parsing and totals (same rules as internal/domain).

export const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

const nf2 = new Intl.NumberFormat("fr-FR", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const nf0 = new Intl.NumberFormat("fr-FR", { maximumFractionDigits: 0 });

export const euros = (cents) => (cents == null ? "—" : nf2.format(cents / 100) + " €");
export const euros0 = (cents) => (cents == null ? "—" : nf0.format(Math.round(cents / 100)) + " €");
export const dec2 = (v) => nf2.format(v / 100);
export const rate = (bp) => String(bp / 100).replace(".", ",") + " %";

export function frDate(iso) {
  if (!iso) return "—";
  const [y, m, d] = iso.slice(0, 10).split("-");
  return `${d}/${m}/${y}`;
}

export function todayISO() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

export function addDays(iso, days) {
  const d = new Date(iso + "T12:00:00");
  d.setDate(d.getDate() + days);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** Parses "1 234,56" / "1234.56" into an integer scaled by 10^dec. Returns NaN when invalid. */
export function parseDec(s, dec = 2) {
  s = String(s ?? "").replace(/[\s\u00a0\u202f€]/g, "").replace(",", ".");
  if (s === "") return 0;
  if (!/^[-+]?\d*(\.\d*)?$/.test(s)) return NaN;
  const neg = s.startsWith("-");
  s = s.replace(/^[-+]/, "");
  let [i, f = ""] = s.split(".");
  if (i === "") i = "0";
  let extra = "";
  if (f.length > dec) { extra = f.slice(dec); f = f.slice(0, dec); }
  f = f.padEnd(dec, "0");
  let v = parseInt(i + f, 10);
  if (extra && extra[0] >= "5") v++;
  return neg ? -v : v;
}

/** Formats a scaled integer for an input field ("20,50"). */
export const fmtInput = (v, dec = 2) => (v === 0 || v == null ? "" : (v / 10 ** dec).toFixed(dec).replace(".", ","));

/** Half away from zero, like domain.RoundDiv. */
export function roundDiv(a, b) {
  if (b < 0) { a = -a; b = -b; }
  return a >= 0 ? Math.floor((a + Math.floor(b / 2)) / b) : -Math.floor((-a + Math.floor(b / 2)) / b);
}

export const lineTotal = (l) => (l.kind === "item" ? roundDiv(l.qty_c * l.unit_price_cents, 100) : 0);

export function computeTotals(lines, discountBP = 0, advance = 0) {
  const gross = new Map();
  let g = 0;
  for (const l of lines) {
    if (l.kind !== "item") continue;
    const v = lineTotal(l);
    gross.set(l.vat_bp, (gross.get(l.vat_bp) || 0) + v);
    g += v;
  }
  const t = { gross_cents: g, ht_cents: 0, vat_cents: 0, vat: [], advance_cents: advance };
  for (const r of [...gross.keys()].sort((a, b) => a - b)) {
    const gr = gross.get(r);
    const base = gr - roundDiv(gr * discountBP, 10000);
    const vat = roundDiv(base * r, 10000);
    t.vat.push({ rate_bp: r, base_cents: base, vat_cents: vat });
    t.ht_cents += base;
    t.vat_cents += vat;
  }
  t.discount_cents = t.gross_cents - t.ht_cents;
  t.ttc_cents = t.ht_cents + t.vat_cents;
  t.net_cents = t.ttc_cents - advance;
  return t;
}

export const norm = (s) => String(s || "").normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();

export const STATUS = {
  draft: "Brouillon", sent: "Envoyé", accepted: "Accepté", refused: "Refusé", invoiced: "Facturé",
  issued: "À payer", partially_paid: "Payée en partie", paid: "Payée",
};

export function statusBadge(status, type) {
  let label = STATUS[status] || status;
  if (type === "avoir" && status !== "draft") label = "Émis";
  return `<span class="badge ${esc(status)}">${esc(label)}</span>`;
}

export function typeBadge(type) {
  const label = { devis: "Devis", facture: "Facture", avoir: "Avoir" }[type] || type;
  return `<span class="badge ${esc(type)}">${label}</span>`;
}

export function debounce(fn, ms = 250) {
  let t;
  return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); };
}

export function docHref(d) {
  return d.type === "devis" ? `#/devis/${d.id}` : `#/factures/${d.id}`;
}

export function downloadCSV(name, rows) {
  const csv = rows.map((r) => r.map((v) => `"${String(v ?? "").replace(/"/g, '""')}"`).join(";")).join("\r\n");
  const a = document.createElement("a");
  a.href = URL.createObjectURL(new Blob(["\uFEFF" + csv], { type: "text/csv;charset=utf-8" }));
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
}

export const centsCSV = (c) => (c == null ? "" : (c / 100).toFixed(2).replace(".", ","));
