package store

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
)

// Defaults hold no company identity: the real values come from data/import/settings.json on first run
// (kept outside the repository) or are typed in the Settings screen.
// Keys ending in "_todo" mark values the user still has to confirm.
var Defaults = map[string]string{
	"company_name":        "",
	"company_trades":      "",
	"company_address":     "",
	"company_city":        "",
	"company_phone":       "",
	"company_phone2":      "",
	"company_email":       "",
	"company_siret":       "",
	"company_ape":         "",
	"company_vat_number":  "",
	"insurance":           "",
	"late_penalty":        "En cas de retard de paiement, pénalités au taux de 3 fois le taux d'intérêt légal en vigueur.",
	"recovery_fee":        "Pour les clients professionnels : indemnité forfaitaire pour frais de recouvrement de 40 € (art. L441-10 du Code de commerce).",
	"payment_days":        "0",
	"payment_terms":       "Règlement à la réception de la facture",
	"no_discount_terms":   "Pas d'escompte pour paiement anticipé.",
	"quote_validity_days": "30",
	"deposit_terms":       "*30% versés à la signature du devis\n*30% versés au début des travaux",
	"default_vat_bp":      "1000",
	"vat_regime":          "normal", // "normal" or "franchise" (micro-entreprise, art. 293 B du CGI)
	"vat_franchise_note":  "TVA non applicable, art. 293 B du CGI",
	"reduced_vat_note":    "", // printed when a line uses 5.5 % or 10 % (wording to confirm with an accountant)
	"vat_rates_bp":        "550,1000,2000,0",
	"units":               "U,m²,ml,h,forfait,kg,l",
	"invoice_start":       "1",
	"backup_external_dir": "",
	"backup_keep":         "30",
	"legal_todo":          "1",
}

// Settings returns all settings merged over the defaults.
func (s *Store) Settings() (map[string]string, error) {
	out := map[string]string{}
	for k, v := range Defaults {
		out[k] = v
	}
	rows, err := s.DB.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// Setting returns a single setting.
func (s *Store) Setting(key string) string {
	var v string
	if err := s.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return Defaults[key]
	}
	return v
}

// SettingInt returns a setting as an integer (0 on error).
func (s *Store) SettingInt(key string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(s.Setting(key)), 10, 64)
	return v
}

// SaveSettings upserts the given keys. Unknown keys are ignored.
func (s *Store) SaveSettings(values map[string]string) error {
	return s.Tx(nil, func(tx *sql.Tx) error {
		for k, v := range values {
			if _, ok := Defaults[k]; !ok {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
				return err
			}
		}
		b, _ := json.Marshal(values)
		return audit(tx, "settings", 0, "update", string(b))
	})
}

// Company is the issuer identity printed on documents (snapshotted on issued invoices).
type Company struct {
	Name      string   `json:"name"`
	Trades    []string `json:"trades"`
	Address   []string `json:"address"`
	City      string   `json:"city"`
	Phones    []string `json:"phones"`
	Email     string   `json:"email"`
	Siret     string   `json:"siret"`
	APE       string   `json:"ape"`
	VatNumber string   `json:"vat_number"`
	Insurance string   `json:"insurance"`
	// VAT regime, frozen with the identity on issued invoices.
	VatFranchise   bool   `json:"vat_franchise"`
	FranchiseNote  string `json:"franchise_note"`
	ReducedVatNote string `json:"reduced_vat_note"`
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// CompanyFrom builds the issuer identity from settings.
func CompanyFrom(st map[string]string) Company {
	var phones []string
	for _, p := range []string{st["company_phone"], st["company_phone2"]} {
		if strings.TrimSpace(p) != "" {
			phones = append(phones, strings.TrimSpace(p))
		}
	}
	return Company{
		Name: st["company_name"], Trades: lines(st["company_trades"]), Address: lines(st["company_address"]),
		City: st["company_city"], Phones: phones, Email: st["company_email"], Siret: st["company_siret"],
		APE: st["company_ape"], VatNumber: st["company_vat_number"], Insurance: st["insurance"],
		VatFranchise: st["vat_regime"] == "franchise", FranchiseNote: strings.TrimSpace(st["vat_franchise_note"]),
		ReducedVatNote: strings.TrimSpace(st["reduced_vat_note"]),
	}
}

// IntList parses "550,1000,2000".
func IntList(s string) []int64 {
	var out []int64
	for _, p := range strings.Split(s, ",") {
		if v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}
