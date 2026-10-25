package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chris-reynier/artiman/internal/domain"
)

// Quote is an estimate ("devis").
type Quote struct {
	ID           int64         `json:"id"`
	CustomerID   int64         `json:"customer_id"`
	Number       string        `json:"number"`
	Date         string        `json:"date"`
	ValidUntil   string        `json:"valid_until"`
	Title        string        `json:"title"`
	Site         string        `json:"site"`
	DiscountBP   int64         `json:"discount_bp"`
	Status       string        `json:"status"`
	DepositTerms string        `json:"deposit_terms"`
	Notes        string        `json:"notes"`
	PDFPath      string        `json:"pdf_path"`
	Legacy       bool          `json:"legacy"`
	LegacySource string        `json:"legacy_source"`
	LegacyFlags  string        `json:"legacy_flags"`
	HTCents      int64         `json:"ht_cents"`
	TTCCents     int64         `json:"ttc_cents"`
	CreatedAt    string        `json:"created_at"`
	Lines        []domain.Line `json:"lines"`
	Totals       domain.Totals `json:"totals"`
	Customer     *Customer     `json:"customer,omitempty"`
	InvoiceID    *int64        `json:"invoice_id"`
	InvoiceLabel string        `json:"invoice_label"`
}

// Editable tells whether the quote content can still be changed.
func (q Quote) Editable() bool { return !q.Legacy && q.Status != "invoiced" }

const quoteCols = `id, customer_id, COALESCE(number,''), date, valid_until, title, site, discount_bp, status, deposit_terms, notes,
	pdf_path, legacy, legacy_source, legacy_flags, ht_cents, ttc_cents, created_at`

func scanQuote(sc interface{ Scan(...any) error }) (Quote, error) {
	var q Quote
	err := sc.Scan(&q.ID, &q.CustomerID, &q.Number, &q.Date, &q.ValidUntil, &q.Title, &q.Site, &q.DiscountBP, &q.Status,
		&q.DepositTerms, &q.Notes, &q.PDFPath, &q.Legacy, &q.LegacySource, &q.LegacyFlags, &q.HTCents, &q.TTCCents, &q.CreatedAt)
	return q, err
}

func loadLines(q queryer, table, fk string, id int64) ([]domain.Line, error) {
	rows, err := q.Query(`SELECT id, position, kind, label, description, unit, qty_c, unit_price_cents, vat_bp FROM `+table+
		` WHERE `+fk+` = ? ORDER BY position, id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Line{}
	for rows.Next() {
		var l domain.Line
		if err := rows.Scan(&l.ID, &l.Position, &l.Kind, &l.Label, &l.Description, &l.Unit, &l.QtyC, &l.UnitPriceCents, &l.VatBP); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func saveLines(tx *sql.Tx, table, fk string, id int64, lines []domain.Line) error {
	if _, err := tx.Exec(`DELETE FROM `+table+` WHERE `+fk+` = ?`, id); err != nil {
		return err
	}
	for i, l := range lines {
		if _, err := tx.Exec(`INSERT INTO `+table+`(`+fk+`, position, kind, label, description, unit, qty_c, unit_price_cents, vat_bp)
			VALUES (?,?,?,?,?,?,?,?,?)`, id, i+1, l.Kind, l.Label, l.Description, l.Unit, l.QtyC, l.UnitPriceCents, l.VatBP); err != nil {
			return err
		}
	}
	return nil
}

func cleanLines(lines []domain.Line) ([]domain.Line, error) {
	out := make([]domain.Line, 0, len(lines))
	for i, l := range lines {
		l.Label = strings.TrimSpace(l.Label)
		l.Description = strings.TrimRight(strings.ReplaceAll(l.Description, "\r", ""), " \n")
		switch l.Kind {
		case domain.KindItem:
			if l.Label == "" && l.Description == "" && l.QtyC == 0 && l.UnitPriceCents == 0 {
				continue // empty row left by the editor
			}
			if l.Label == "" && l.Description == "" {
				return nil, ValidationError{fmt.Sprintf("Ligne %d : le titre est vide.", i+1)}
			}
		case domain.KindSection, domain.KindText:
			if l.Label == "" && l.Description == "" {
				continue
			}
			l.QtyC, l.UnitPriceCents = 0, 0
		default:
			return nil, ValidationError{fmt.Sprintf("Ligne %d : type inconnu.", i+1)}
		}
		out = append(out, l)
	}
	return out, nil
}

// GetQuote loads a quote with its lines, totals and customer.
func (s *Store) GetQuote(id int64) (Quote, error) {
	q, err := scanQuote(s.DB.QueryRow(`SELECT `+quoteCols+` FROM quotes WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return q, ErrNotFound
	}
	if err != nil {
		return q, err
	}
	if q.Lines, err = loadLines(s.DB, "quote_lines", "quote_id", id); err != nil {
		return q, err
	}
	q.Totals = domain.ComputeTotals(q.Lines, q.DiscountBP, 0)
	c, err := s.GetCustomer(q.CustomerID)
	if err != nil {
		return q, err
	}
	q.Customer = &c
	var invID int64
	var num sql.NullInt64
	var legacyNum string
	if err := s.DB.QueryRow(`SELECT id, number, legacy_number_text FROM invoices WHERE quote_id = ? AND kind = 'invoice' ORDER BY id DESC LIMIT 1`, id).
		Scan(&invID, &num, &legacyNum); err == nil {
		q.InvoiceID = &invID
		q.InvoiceLabel = invoiceLabel(num, legacyNum, "invoice")
	}
	return q, nil
}

func invoiceLabel(num sql.NullInt64, legacy, kind string) string {
	prefix := "Facture"
	if kind == "credit_note" {
		prefix = "Avoir"
	}
	switch {
	case num.Valid:
		return fmt.Sprintf("%s n°%04d", prefix, num.Int64)
	case legacy != "":
		return prefix + " n°" + legacy
	default:
		return prefix + " (brouillon)"
	}
}

func nextQuoteNumber(tx *sql.Tx, date string) (string, error) {
	year := date[:4]
	var max sql.NullString
	if err := tx.QueryRow(`SELECT MAX(number) FROM quotes WHERE number LIKE ?`, "D"+year+"-%").Scan(&max); err != nil {
		return "", err
	}
	n := 0
	if max.Valid {
		fmt.Sscanf(strings.TrimPrefix(max.String, "D"+year+"-"), "%d", &n)
	}
	return fmt.Sprintf("D%s-%03d", year, n+1), nil
}

// SaveQuote creates (ID 0) or updates a quote with its lines.
func (s *Store) SaveQuote(q Quote) (int64, error) {
	if q.CustomerID == 0 {
		return 0, ValidationError{"Choisissez un client."}
	}
	if _, err := time.Parse("2006-01-02", q.Date); err != nil {
		return 0, ValidationError{"Date du devis invalide."}
	}
	lines, err := cleanLines(q.Lines)
	if err != nil {
		return 0, err
	}
	if q.DiscountBP < 0 || q.DiscountBP > 10000 {
		return 0, ValidationError{"Remise invalide."}
	}
	if q.Status == "" || q.Status == "invoiced" {
		q.Status = "draft"
	}
	err = s.Tx(nil, func(tx *sql.Tx) error {
		if q.ID == 0 {
			num, err := nextQuoteNumber(tx, q.Date)
			if err != nil {
				return err
			}
			res, err := tx.Exec(`INSERT INTO quotes(customer_id, number, date, valid_until, title, site, discount_bp, status, deposit_terms, notes)
				VALUES (?,?,?,?,?,?,?,?,?,?)`, q.CustomerID, num, q.Date, q.ValidUntil, q.Title, q.Site, q.DiscountBP, q.Status, q.DepositTerms, q.Notes)
			if err != nil {
				return err
			}
			q.ID, _ = res.LastInsertId()
			if err := audit(tx, "quote", q.ID, "create", num); err != nil {
				return err
			}
		} else {
			cur, err := scanQuote(tx.QueryRow(`SELECT `+quoteCols+` FROM quotes WHERE id = ?`, q.ID))
			if err != nil {
				return ErrNotFound
			}
			if !cur.Editable() {
				return ValidationError{"Ce devis n'est plus modifiable (déjà facturé ou issu de l'historique). Dupliquez-le."}
			}
			if _, err := tx.Exec(`UPDATE quotes SET customer_id=?, date=?, valid_until=?, title=?, site=?, discount_bp=?, status=?, deposit_terms=?,
				notes=?, updated_at=datetime('now','localtime') WHERE id=?`, q.CustomerID, q.Date, q.ValidUntil, q.Title, q.Site, q.DiscountBP,
				q.Status, q.DepositTerms, q.Notes, q.ID); err != nil {
				return err
			}
			if err := audit(tx, "quote", q.ID, "update", ""); err != nil {
				return err
			}
		}
		if err := saveLines(tx, "quote_lines", "quote_id", q.ID, lines); err != nil {
			return err
		}
		t := domain.ComputeTotals(lines, q.DiscountBP, 0)
		if _, err := tx.Exec(`UPDATE quotes SET ht_cents=?, ttc_cents=? WHERE id=?`, t.HTCents, t.TTCCents, q.ID); err != nil {
			return err
		}
		return bumpTemplates(tx, lines)
	})
	return q.ID, err
}

// SetQuoteStatus changes the commercial status (sent, accepted, refused, draft).
func (s *Store) SetQuoteStatus(id int64, status string) error {
	switch status {
	case "draft", "sent", "accepted", "refused":
	default:
		return ValidationError{"Statut invalide."}
	}
	return s.Tx(nil, func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE quotes SET status=?, updated_at=datetime('now','localtime') WHERE id=? AND legacy=0 AND status<>'invoiced'`, status, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ValidationError{"Statut non modifiable pour ce devis."}
		}
		return audit(tx, "quote", id, "status", status)
	})
}

// SetQuotePDF records the archived PDF path.
func (s *Store) SetQuotePDF(id int64, path string) error {
	_, err := s.DB.Exec(`UPDATE quotes SET pdf_path=? WHERE id=?`, path, id)
	return err
}

// DeleteQuote deletes a quote that is neither invoiced nor historical.
func (s *Store) DeleteQuote(id int64) error {
	return s.Tx(nil, func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM quotes WHERE id=? AND legacy=0 AND status<>'invoiced' AND NOT EXISTS (SELECT 1 FROM invoices WHERE quote_id=quotes.id)`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ValidationError{"Ce devis ne peut pas être supprimé (facturé ou historique)."}
		}
		return audit(tx, "quote", id, "delete", "")
	})
}

// DuplicateQuote copies any quote (including historical ones) into a new draft dated today.
func (s *Store) DuplicateQuote(id int64, today string, validityDays int) (int64, error) {
	src, err := s.GetQuote(id)
	if err != nil {
		return 0, err
	}
	t, _ := time.Parse("2006-01-02", today)
	cp := Quote{CustomerID: src.CustomerID, Date: today, ValidUntil: t.AddDate(0, 0, validityDays).Format("2006-01-02"),
		Title: src.Title, Site: src.Site, DiscountBP: src.DiscountBP, Status: "draft", DepositTerms: src.DepositTerms, Lines: src.Lines}
	if cp.DepositTerms == "" {
		cp.DepositTerms = s.Setting("deposit_terms")
	}
	return s.SaveQuote(cp)
}
