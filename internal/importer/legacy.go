// Package importer loads the history rebuilt from the old ArtiMan Word documents
// (the data.json produced by the extraction tools) as read-only legacy records.
package importer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/chris-reynier/artiman/internal/domain"
	"github.com/chris-reynier/artiman/internal/store"
)

type item struct {
	Section string   `json:"section"`
	Label   string   `json:"label"`
	Qty     *float64 `json:"qty"`
	Unit    string   `json:"unit"`
	PU      *float64 `json:"pu"`
	Total   *float64 `json:"total"`
	Desc    string   `json:"desc"`
}

type doc struct {
	ID         int      `json:"id"`
	Kind       string   `json:"kind"`
	Date       string   `json:"date"`
	Client     string   `json:"client"`
	Number     string   `json:"number"`
	Title      string   `json:"title"`
	HT         *float64 `json:"ht"`
	TvaRate    *float64 `json:"tva_rate"`
	Tva        *float64 `json:"tva"`
	TTC        *float64 `json:"ttc"`
	Acompte    *float64 `json:"acompte"`
	Remise     *float64 `json:"remise"`
	ValidUntil string   `json:"valid_until"`
	Items      []item   `json:"items"`
	Path       string   `json:"path"`
	Flags      []string `json:"flags"`
	Address    []string `json:"address"`
	FromDevis  *int     `json:"from_devis"`
	Zip        string   `json:"zip"`
	City       string   `json:"city"`
}

type client struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Civility string `json:"civility"`
	City     string `json:"city"`
	Zip      string `json:"zip"`
	Docs     []int  `json:"docs"`
}

type history struct {
	Clients []client `json:"clients"`
	Docs    []doc    `json:"docs"`
}

// Result summarises an import.
type Result struct {
	Customers int `json:"customers"`
	Quotes    int `json:"quotes"`
	Invoices  int `json:"invoices"`
	Skipped   int `json:"skipped"`
	Templates int `json:"templates"`
}

var civilities = map[string]string{
	"M": "M.", "M.": "M.", "Mr": "M.", "Monsieur": "M.", "Mme": "Mme", "Madame": "Mme",
	"M. et Mme": "M. et Mme", "Mr et Mme": "M. et Mme", "M et Mme": "M. et Mme", "Mlle": "Mlle", "Melle": "Mlle",
}

var zipLine = regexp.MustCompile(`^\d{2}\s?\d{3}\b`)

// splitName turns "DUPONT Jean François" into ("DUPONT", "Jean François"); all-caps names stay whole.
func splitName(full string) (string, string) {
	words := strings.Fields(full)
	i := 0
	for i < len(words) && strings.ToUpper(words[i]) == words[i] && strings.ContainsAny(words[i], "ABCDEFGHIJKLMNOPQRSTUVWXYZÉÈ") {
		i++
	}
	if i == 0 || i == len(words) {
		return strings.Join(words, " "), ""
	}
	return strings.Join(words[:i], " "), strings.Join(words[i:], " ")
}

func cents(v *float64) int64 {
	if v == nil {
		return 0
	}
	return int64(math.Round(*v * 100))
}

func toLines(d doc) []domain.Line {
	vat := int64(0)
	if d.TvaRate != nil {
		vat = int64(math.Round(*d.TvaRate * 100))
	}
	var out []domain.Line
	section := ""
	for _, it := range d.Items {
		if it.Section != "" && it.Section != section {
			section = it.Section
			out = append(out, domain.Line{Kind: domain.KindSection, Label: section})
		}
		total := cents(it.Total)
		qty, pu := int64(100), total
		if it.Qty != nil && *it.Qty != 0 {
			qty = int64(math.Round(*it.Qty * 100))
			if it.PU != nil {
				pu = cents(it.PU)
			} else {
				pu = domain.RoundDiv(total*100, qty)
			}
		}
		unit := it.Unit
		if unit == "" {
			unit = "U"
		}
		out = append(out, domain.Line{Kind: domain.KindItem, Label: it.Label, Description: it.Desc, Unit: unit,
			QtyC: qty, UnitPriceCents: pu, VatBP: vat})
	}
	return out
}

func insertLines(tx *sql.Tx, table, fk string, id int64, lines []domain.Line) error {
	for i, l := range lines {
		if _, err := tx.Exec(`INSERT INTO `+table+`(`+fk+`, position, kind, label, description, unit, qty_c, unit_price_cents, vat_bp)
			VALUES (?,?,?,?,?,?,?,?,?)`, id, i+1, l.Kind, l.Label, l.Description, l.Unit, l.QtyC, l.UnitPriceCents, l.VatBP); err != nil {
			return err
		}
	}
	return nil
}

// Import loads history.json into an empty database. descriptionFile is the old description.txt (optional).
func Import(s *store.Store, historyPath, descriptionFile string) (Result, error) {
	var res Result
	b, err := os.ReadFile(historyPath)
	if err != nil {
		return res, err
	}
	var h history
	if err := json.Unmarshal(b, &h); err != nil {
		return res, fmt.Errorf("historique illisible : %w", err)
	}
	c, _, _, err := s.Counts()
	if err != nil {
		return res, err
	}
	if c > 0 {
		return res, fmt.Errorf("la base contient déjà des clients : import annulé")
	}
	docs := make(map[int]doc, len(h.Docs))
	for _, d := range h.Docs {
		docs[d.ID] = d
	}
	err = s.Tx(nil, func(tx *sql.Tx) error {
		quoteIDs := map[int]int64{}
		descCount := map[string]int{}
		for _, cl := range h.Clients {
			if len(cl.Docs) == 0 {
				continue
			}
			ds := make([]doc, 0, len(cl.Docs))
			for _, id := range cl.Docs {
				ds = append(ds, docs[id])
			}
			sort.Slice(ds, func(i, j int) bool { return ds[i].Date < ds[j].Date })
			last := ds[len(ds)-1]
			var addr []string
			for _, l := range last.Address {
				if !zipLine.MatchString(l) {
					addr = append(addr, l)
				}
			}
			civ := civilities[cl.Civility]
			lastName, first := splitName(cl.Name)
			r, err := tx.Exec(`INSERT INTO customers(civility, last_name, first_name, address, zip, city, is_business, legacy_key, notes)
				VALUES (?,?,?,?,?,?,?,?,?)`, civ, lastName, first, strings.Join(addr, "\n"), cl.Zip, cl.City, civ == "", cl.Key,
				"Importé de l'ancien ArtiMan")
			if err != nil {
				return err
			}
			cid, _ := r.LastInsertId()
			res.Customers++
			// Quotes first so invoices can reference them.
			for _, d := range ds {
				if d.Kind != "devis" {
					continue
				}
				lines := toLines(d)
				ht, ttc := cents(d.HT), cents(d.TTC)
				if d.HT == nil {
					t := domain.ComputeTotals(lines, 0, 0)
					ht, ttc = t.HTCents, t.TTCCents
				}
				status := "sent"
				r, err := tx.Exec(`INSERT INTO quotes(customer_id, date, valid_until, title, status, legacy, legacy_source, legacy_flags, ht_cents, ttc_cents)
					VALUES (?,?,?,?,?,1,?,?,?,?)`, cid, d.Date, d.ValidUntil, d.Title, status, d.Path, strings.Join(d.Flags, " · "), ht, ttc)
				if err != nil {
					return err
				}
				qid, _ := r.LastInsertId()
				if err := insertLines(tx, "quote_lines", "quote_id", qid, lines); err != nil {
					return err
				}
				quoteIDs[d.ID] = qid
				res.Quotes++
				for _, l := range lines {
					if l.Description != "" && len(l.Description) < 400 {
						descCount[l.Description]++
					}
				}
			}
			for _, d := range ds {
				switch d.Kind {
				case "devis":
					continue
				case "facture":
				default:
					res.Skipped++
					continue
				}
				lines := toLines(d)
				var quoteID any
				if d.FromDevis != nil {
					if q, ok := quoteIDs[*d.FromDevis]; ok {
						quoteID = q
						tx.Exec(`UPDATE quotes SET status='invoiced' WHERE id=?`, q)
					}
				}
				ht, tva, ttc := cents(d.HT), cents(d.Tva), cents(d.TTC)
				if d.HT == nil {
					t := domain.ComputeTotals(lines, 0, 0)
					ht, tva, ttc = t.HTCents, t.VatCents, t.TTCCents
				}
				rate := int64(0)
				if d.TvaRate != nil {
					rate = int64(math.Round(*d.TvaRate * 100))
				}
				vat, _ := json.Marshal([]domain.VatPart{{RateBP: rate, BaseCents: ht, VatCents: tva}})
				zip := d.Zip
				if len(zip) == 5 {
					zip = zip[:2] + " " + zip[2:]
				}
				party, _ := json.Marshal(store.Party{Display: strings.TrimSpace(civ + " " + cl.Name), Short: cl.Name,
					Address: func() []string {
						var a []string
						for _, l := range d.Address {
							if !zipLine.MatchString(l) {
								a = append(a, l)
							}
						}
						return a
					}(), Zip: d.Zip, City: d.City, IsBusiness: civ == ""})
				date := d.Date
				if date == "" {
					date = "2000-01-01"
				}
				r, err := tx.Exec(`INSERT INTO invoices(kind, quote_id, customer_id, customer_snapshot, date, title, advance_cents, legacy,
					legacy_number_text, legacy_source, legacy_flags) VALUES ('invoice',?,?,?,?,?,?,1,?,?,?)`,
					quoteID, cid, string(party), date, d.Title, cents(d.Acompte), d.Number, d.Path, strings.Join(d.Flags, " · "))
				if err != nil {
					return err
				}
				iid, _ := r.LastInsertId()
				if err := insertLines(tx, "invoice_lines", "invoice_id", iid, lines); err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE invoices SET status='paid', total_ht_cents=?, total_vat_cents=?, total_ttc_cents=?, vat_breakdown=?,
					terms='Règlement à la réception de la facture', issued_at=date WHERE id=?`, ht, tva, ttc, string(vat), iid); err != nil {
					return err
				}
				res.Invoices++
			}
		}
		// Description library: the old description.txt plus the most frequent historical descriptions.
		var texts []string
		if descriptionFile != "" {
			if b, err := os.ReadFile(descriptionFile); err == nil {
				for _, t := range strings.Split(decodeLatin1(b), "§") {
					if t = strings.TrimSpace(t); t != "" {
						texts = append(texts, t)
						descCount[t] += 5
					}
				}
			}
		}
		type kv struct {
			t string
			n int
		}
		var all []kv
		for t, n := range descCount {
			all = append(all, kv{t, n})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].n > all[j].n || (all[i].n == all[j].n && all[i].t < all[j].t) })
		for i, e := range all {
			if i >= 300 {
				break
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO description_templates(text, usage_count) VALUES (?,?)`, strings.TrimSpace(e.t), e.n); err != nil {
				return err
			}
			res.Templates++
		}
		_, err := tx.Exec(`INSERT INTO audit_log(entity, entity_id, action, details) VALUES ('import', 0, 'legacy', ?)`,
			fmt.Sprintf("%d clients, %d devis, %d factures", res.Customers, res.Quotes, res.Invoices))
		return err
	})
	return res, err
}

// decodeLatin1 converts the Windows-1252 text written by the old program (ASCII-compatible part + Latin-1).
func decodeLatin1(b []byte) string {
	if isUTF8(b) {
		return string(b)
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
		if c == 0x80 {
			r[i] = '€'
		}
	}
	return string(r)
}

func isUTF8(b []byte) bool { return utf8.Valid(b) }
