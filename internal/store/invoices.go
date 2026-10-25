package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chris-reynier/artiman/internal/domain"
)

// Party is the customer identity as printed on an issued invoice.
type Party struct {
	Display    string   `json:"display"`
	Short      string   `json:"short"`
	Address    []string `json:"address"`
	Zip        string   `json:"zip"`
	City       string   `json:"city"`
	IsBusiness bool     `json:"is_business"`
}

// PartyFrom snapshots a customer.
func PartyFrom(c Customer) Party {
	return Party{Display: c.DisplayName(), Short: c.ShortName(), Address: lines(c.Address), Zip: c.Zip, City: c.City, IsBusiness: c.IsBusiness}
}

// Invoice is an invoice or a credit note ("avoir").
type Invoice struct {
	ID               int64         `json:"id"`
	Number           *int64        `json:"number"`
	Label            string        `json:"label"`
	Kind             string        `json:"kind"`
	QuoteID          *int64        `json:"quote_id"`
	CreditsInvoiceID *int64        `json:"credits_invoice_id"`
	CustomerID       int64         `json:"customer_id"`
	Party            Party         `json:"party"`
	Date             string        `json:"date"`
	DueDate          string        `json:"due_date"`
	Title            string        `json:"title"`
	Site             string        `json:"site"`
	DiscountBP       int64         `json:"discount_bp"`
	AdvanceCents     int64         `json:"advance_cents"`
	Status           string        `json:"status"`
	Totals           domain.Totals `json:"totals"`
	Terms            string        `json:"terms"`
	Mentions         string        `json:"mentions"`
	Company          *Company      `json:"company,omitempty"`
	PDFPath          string        `json:"pdf_path"`
	PDFSHA256        string        `json:"pdf_sha256"`
	Notes            string        `json:"notes"`
	Legacy           bool          `json:"legacy"`
	LegacyNumber     string        `json:"legacy_number_text"`
	LegacySource     string        `json:"legacy_source"`
	LegacyFlags      string        `json:"legacy_flags"`
	IssuedAt         string        `json:"issued_at"`
	Lines            []domain.Line `json:"lines"`
	PaidAt           string        `json:"paid_at"`
	RemainingCents   int64         `json:"remaining_cents"`
	Customer         *Customer     `json:"customer,omitempty"`
	CreditNotes      []string      `json:"credit_notes"`
	CreditedLabel    string        `json:"credited_label"`
}

const invoiceCols = `id, number, kind, quote_id, credits_invoice_id, customer_id, customer_snapshot, date, due_date, title, site, discount_bp,
	advance_cents, status, total_ht_cents, total_vat_cents, total_ttc_cents, vat_breakdown, terms, mentions, company_snapshot,
	pdf_path, pdf_sha256, notes, legacy, legacy_number_text, legacy_source, legacy_flags, issued_at, paid_at`

func scanInvoice(sc interface{ Scan(...any) error }) (Invoice, error) {
	var inv Invoice
	var num, quoteID, credits sql.NullInt64
	var party, vat, company string
	var ht, tva, ttc int64
	err := sc.Scan(&inv.ID, &num, &inv.Kind, &quoteID, &credits, &inv.CustomerID, &party, &inv.Date, &inv.DueDate, &inv.Title,
		&inv.Site, &inv.DiscountBP, &inv.AdvanceCents, &inv.Status, &ht, &tva, &ttc, &vat, &inv.Terms, &inv.Mentions, &company,
		&inv.PDFPath, &inv.PDFSHA256, &inv.Notes, &inv.Legacy, &inv.LegacyNumber, &inv.LegacySource, &inv.LegacyFlags, &inv.IssuedAt, &inv.PaidAt)
	if err != nil {
		return inv, err
	}
	if num.Valid {
		inv.Number = &num.Int64
	}
	if quoteID.Valid {
		inv.QuoteID = &quoteID.Int64
	}
	if credits.Valid {
		inv.CreditsInvoiceID = &credits.Int64
	}
	json.Unmarshal([]byte(party), &inv.Party)
	inv.Totals = domain.Totals{HTCents: ht, VatCents: tva, TTCCents: ttc, AdvanceCents: inv.AdvanceCents, NetCents: ttc - inv.AdvanceCents}
	json.Unmarshal([]byte(vat), &inv.Totals.VAT)
	if company != "{}" && company != "" {
		var c Company
		if json.Unmarshal([]byte(company), &c) == nil {
			inv.Company = &c
		}
	}
	inv.Label = invoiceLabel(num, inv.LegacyNumber, inv.Kind)
	return inv, nil
}

// GetInvoice loads an invoice with lines and customer.
func (s *Store) GetInvoice(id int64) (Invoice, error) {
	inv, err := scanInvoice(s.DB.QueryRow(`SELECT `+invoiceCols+` FROM invoices WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return inv, ErrNotFound
	}
	if err != nil {
		return inv, err
	}
	if inv.Lines, err = loadLines(s.DB, "invoice_lines", "invoice_id", id); err != nil {
		return inv, err
	}
	if inv.Status == "draft" {
		inv.Totals = domain.ComputeTotals(inv.Lines, inv.DiscountBP, inv.AdvanceCents)
	} else if !inv.Legacy {
		// Gross and discount are not stored; recompute them for display only.
		t := domain.ComputeTotals(inv.Lines, inv.DiscountBP, inv.AdvanceCents)
		inv.Totals.GrossCents, inv.Totals.DiscountCents = t.GrossCents, t.DiscountCents
	}
	c, err := s.GetCustomer(inv.CustomerID)
	if err != nil {
		return inv, err
	}
	inv.Customer = &c
	if inv.Status == "draft" {
		inv.Party = PartyFrom(c)
	}
	if inv.Status != "paid" && inv.Status != "draft" && inv.Kind == "invoice" {
		inv.RemainingCents = inv.Totals.NetCents
	}
	cn, _ := s.DB.Query(`SELECT number, legacy_number_text FROM invoices WHERE credits_invoice_id = ? AND status <> 'draft'`, id)
	if cn != nil {
		for cn.Next() {
			var n sql.NullInt64
			var l string
			cn.Scan(&n, &l)
			inv.CreditNotes = append(inv.CreditNotes, invoiceLabel(n, l, "credit_note"))
		}
		cn.Close()
	}
	if inv.CreditsInvoiceID != nil {
		var n sql.NullInt64
		var l string
		if s.DB.QueryRow(`SELECT number, legacy_number_text FROM invoices WHERE id = ?`, *inv.CreditsInvoiceID).Scan(&n, &l) == nil {
			inv.CreditedLabel = invoiceLabel(n, l, "invoice")
		}
	}
	return inv, nil
}

func addDays(date string, days int64) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, int(days)).Format("2006-01-02")
}

// CreateInvoiceFromQuote creates a draft invoice copying the quote lines.
func (s *Store) CreateInvoiceFromQuote(quoteID int64, today string) (int64, error) {
	q, err := s.GetQuote(quoteID)
	if err != nil {
		return 0, err
	}
	if q.InvoiceID != nil {
		return *q.InvoiceID, nil
	}
	if q.Legacy {
		return 0, ValidationError{"Devis issu de l'historique : dupliquez-le pour le facturer."}
	}
	inv := Invoice{Kind: "invoice", QuoteID: &q.ID, CustomerID: q.CustomerID, Date: today, Title: q.Title, Site: q.Site,
		DiscountBP: q.DiscountBP, Lines: q.Lines}
	return s.SaveDraftInvoice(inv)
}

// CreateCreditNote creates a draft credit note that cancels the given issued invoice (lines negated).
func (s *Store) CreateCreditNote(invoiceID int64, today string) (int64, error) {
	src, err := s.GetInvoice(invoiceID)
	if err != nil {
		return 0, err
	}
	if src.Status == "draft" || src.Kind != "invoice" {
		return 0, ValidationError{"Un avoir ne peut être fait que sur une facture émise."}
	}
	lines := make([]domain.Line, len(src.Lines))
	for i, l := range src.Lines {
		l.QtyC = -l.QtyC
		lines[i] = l
	}
	if len(lines) == 0 && src.Legacy {
		vat := int64(0)
		if len(src.Totals.VAT) > 0 {
			vat = src.Totals.VAT[0].RateBP
		}
		lines = []domain.Line{{Kind: domain.KindItem, Label: "Annulation " + src.Label, Unit: "U", QtyC: -100, UnitPriceCents: src.Totals.HTCents, VatBP: vat}}
	}
	inv := Invoice{Kind: "credit_note", CreditsInvoiceID: &src.ID, CustomerID: src.CustomerID, Date: today,
		Title: "Avoir sur " + src.Label, DiscountBP: src.DiscountBP, Lines: lines}
	return s.SaveDraftInvoice(inv)
}

// SaveDraftInvoice creates (ID 0) or updates a draft invoice or credit note.
func (s *Store) SaveDraftInvoice(inv Invoice) (int64, error) {
	if inv.CustomerID == 0 {
		return 0, ValidationError{"Choisissez un client."}
	}
	if _, err := time.Parse("2006-01-02", inv.Date); err != nil {
		return 0, ValidationError{"Date de facture invalide."}
	}
	lines, err := cleanLines(inv.Lines)
	if err != nil {
		return 0, err
	}
	if inv.Kind == "" {
		inv.Kind = "invoice"
	}
	err = s.Tx(nil, func(tx *sql.Tx) error {
		if inv.ID == 0 {
			res, err := tx.Exec(`INSERT INTO invoices(kind, quote_id, credits_invoice_id, customer_id, date, title, site, discount_bp, advance_cents, notes)
				VALUES (?,?,?,?,?,?,?,?,?,?)`, inv.Kind, inv.QuoteID, inv.CreditsInvoiceID, inv.CustomerID, inv.Date, inv.Title, inv.Site,
				inv.DiscountBP, inv.AdvanceCents, inv.Notes)
			if err != nil {
				return err
			}
			inv.ID, _ = res.LastInsertId()
			if err := audit(tx, "invoice", inv.ID, "create", inv.Kind); err != nil {
				return err
			}
		} else {
			res, err := tx.Exec(`UPDATE invoices SET customer_id=?, date=?, title=?, site=?, discount_bp=?, advance_cents=?, notes=?,
				updated_at=datetime('now','localtime') WHERE id=? AND status='draft'`, inv.CustomerID, inv.Date, inv.Title, inv.Site,
				inv.DiscountBP, inv.AdvanceCents, inv.Notes, inv.ID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return ValidationError{"Cette facture est déjà émise : elle n'est plus modifiable."}
			}
		}
		return saveLines(tx, "invoice_lines", "invoice_id", inv.ID, lines)
	})
	return inv.ID, err
}

// DeleteDraftInvoice removes a draft (issued invoices are protected by triggers).
func (s *Store) DeleteDraftInvoice(id int64) error {
	return s.Tx(nil, func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM invoices WHERE id=? AND status='draft'`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ValidationError{"Seul un brouillon peut être supprimé."}
		}
		return audit(tx, "invoice", id, "delete", "draft")
	})
}

// Mentions builds the legal mentions printed on an invoice from settings.
func Mentions(st map[string]string, business bool) string {
	parts := []string{}
	for _, k := range []string{"late_penalty", "no_discount_terms"} {
		if v := strings.TrimSpace(st[k]); v != "" {
			parts = append(parts, v)
		}
	}
	if business {
		if v := strings.TrimSpace(st["recovery_fee"]); v != "" {
			parts = append(parts, v)
		}
	}
	if v := strings.TrimSpace(st["insurance"]); v != "" {
		parts = append(parts, "Assurance décennale : "+v)
	}
	return strings.Join(parts, "\n")
}

// IssueInvoice gives the next number, freezes totals and identities, and marks the quote invoiced.
// Everything happens in one transaction so the sequence never has gaps.
func (s *Store) IssueInvoice(id int64) (Invoice, error) {
	st, err := s.Settings()
	if err != nil {
		return Invoice{}, err
	}
	err = s.Tx(nil, func(tx *sql.Tx) error {
		inv, err := scanInvoice(tx.QueryRow(`SELECT `+invoiceCols+` FROM invoices WHERE id = ?`, id))
		if err != nil {
			return ErrNotFound
		}
		if inv.Status != "draft" {
			return ValidationError{"Cette facture est déjà émise."}
		}
		lines, err := loadLines(tx, "invoice_lines", "invoice_id", id)
		if err != nil {
			return err
		}
		hasItem := false
		for _, l := range lines {
			hasItem = hasItem || l.Kind == domain.KindItem
		}
		if !hasItem {
			return ValidationError{"La facture ne contient aucune ligne chiffrée."}
		}
		c, err := getCustomer(tx, inv.CustomerID)
		if err != nil {
			return err
		}
		t := domain.ComputeTotals(lines, inv.DiscountBP, inv.AdvanceCents)
		var max sql.NullInt64
		if err := tx.QueryRow(`SELECT MAX(number) FROM invoices`).Scan(&max); err != nil {
			return err
		}
		next := max.Int64 + 1
		if start, _ := parseInt(st["invoice_start"]); next < start {
			next = start
		}
		party, _ := json.Marshal(PartyFrom(c))
		company, _ := json.Marshal(CompanyFrom(st))
		vat, _ := json.Marshal(t.VAT)
		days, _ := parseInt(st["payment_days"])
		terms := st["payment_terms"]
		if inv.Kind == "credit_note" {
			terms = ""
		}
		_, err = tx.Exec(`UPDATE invoices SET status='issued', number=?, customer_snapshot=?, company_snapshot=?, total_ht_cents=?,
			total_vat_cents=?, total_ttc_cents=?, vat_breakdown=?, due_date=?, terms=?, mentions=?, issued_at=datetime('now','localtime'),
			updated_at=datetime('now','localtime') WHERE id=?`,
			next, string(party), string(company), t.HTCents, t.VatCents, t.TTCCents, string(vat), addDays(inv.Date, days), terms,
			Mentions(st, c.IsBusiness), id)
		if err != nil {
			return err
		}
		if inv.QuoteID != nil {
			if _, err := tx.Exec(`UPDATE quotes SET status='invoiced', updated_at=datetime('now','localtime') WHERE id=?`, *inv.QuoteID); err != nil {
				return err
			}
		}
		if inv.Kind == "invoice" && t.NetCents <= 0 {
			if _, err := tx.Exec(`UPDATE invoices SET status='paid', paid_at=date WHERE id=?`, id); err != nil {
				return err
			}
		}
		return audit(tx, "invoice", id, "issue", fmt.Sprintf("n°%04d %s TTC", next, domain.Euros(t.TTCCents)))
	})
	if err != nil {
		return Invoice{}, err
	}
	return s.GetInvoice(id)
}

func parseInt(s string) (int64, error) {
	var v int64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &v)
	return v, err
}

// SetInvoicePDF records the archived PDF of an issued invoice.
func (s *Store) SetInvoicePDF(id int64, path, sha string) error {
	_, err := s.DB.Exec(`UPDATE invoices SET pdf_path=?, pdf_sha256=? WHERE id=?`, path, sha, id)
	return err
}

// SetInvoicePaid marks an issued invoice as paid (with the payment date) or back to unpaid.
// Only the status is kept: amounts and payment methods are deliberately not recorded.
func (s *Store) SetInvoicePaid(id int64, paid bool, date string) error {
	if paid {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return ValidationError{"Date de règlement invalide."}
		}
	} else {
		date = ""
	}
	return s.Tx(nil, func(tx *sql.Tx) error {
		var status, kind string
		if err := tx.QueryRow(`SELECT status, kind FROM invoices WHERE id=?`, id).Scan(&status, &kind); err != nil {
			return ErrNotFound
		}
		if status == "draft" || kind != "invoice" {
			return ValidationError{"Seule une facture émise peut être marquée payée."}
		}
		newStatus := "issued"
		if paid {
			newStatus = "paid"
		}
		if _, err := tx.Exec(`UPDATE invoices SET status=?, paid_at=? WHERE id=?`, newStatus, date, id); err != nil {
			return err
		}
		action := "unpaid"
		if paid {
			action = "paid " + date
		}
		return audit(tx, "invoice", id, action, "")
	})
}
