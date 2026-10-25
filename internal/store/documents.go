package store

import (
	"database/sql"
	"strconv"
	"strings"

	"github.com/chris-reynier/artiman/internal/domain"
)

// DocSummary is one row of the documents list (quotes and invoices together).
type DocSummary struct {
	Type         string `json:"type"` // "devis", "facture", "avoir"
	ID           int64  `json:"id"`
	Date         string `json:"date"`
	Label        string `json:"label"`
	CustomerID   int64  `json:"customer_id"`
	Customer     string `json:"customer"`
	City         string `json:"city"`
	Title        string `json:"title"`
	HTCents      int64  `json:"ht_cents"`
	TTCCents     int64  `json:"ttc_cents"`
	Status       string `json:"status"`
	Legacy       bool   `json:"legacy"`
	Flags        string `json:"flags"`
	LinkedID     *int64 `json:"linked_id"`
	LegacySource string `json:"legacy_source"`
}

// DocFilter narrows the documents list.
type DocFilter struct {
	Query      string
	Type       string // devis, facture, avoir or ""
	Year       string
	CustomerID int64
	Status     string // "unpaid", "open"
	Limit      int
}

// ListDocuments returns quotes and invoices matching the filter, newest first.
func (s *Store) ListDocuments(f DocFilter) ([]DocSummary, error) {
	var where []string
	var args []any
	words := strings.Fields(strings.ToLower(f.Query))
	quoteSQL := `SELECT 'devis', q.id, q.date, COALESCE(q.number,''), q.customer_id, c.last_name || ' ' || c.first_name, c.city,
		CASE WHEN q.title <> '' THEN q.title ELSE COALESCE((SELECT label FROM quote_lines WHERE quote_id=q.id ORDER BY position LIMIT 1),'') END,
		q.ht_cents, q.ttc_cents, q.status, q.legacy, q.legacy_flags,
		(SELECT id FROM invoices WHERE quote_id=q.id AND kind='invoice' LIMIT 1), q.legacy_source, q.discount_bp
		FROM quotes q JOIN customers c ON c.id = q.customer_id`
	invSQL := `SELECT CASE i.kind WHEN 'credit_note' THEN 'avoir' ELSE 'facture' END, i.id, i.date,
		CASE WHEN i.number IS NOT NULL THEN printf('%04d', i.number) WHEN i.legacy_number_text <> '' THEN i.legacy_number_text ELSE 'brouillon' END,
		i.customer_id, c.last_name || ' ' || c.first_name, c.city,
		CASE WHEN i.title <> '' THEN i.title ELSE COALESCE((SELECT label FROM invoice_lines WHERE invoice_id=i.id ORDER BY position LIMIT 1),'') END,
		CASE WHEN i.status='draft' THEN -1 ELSE i.total_ht_cents END, CASE WHEN i.status='draft' THEN -1 ELSE i.total_ttc_cents END,
		i.status, i.legacy, i.legacy_flags, i.quote_id, i.legacy_source, i.discount_bp
		FROM invoices i JOIN customers c ON c.id = i.customer_id`
	build := func(alias, linesTable, fk string) (string, []any) {
		var w []string
		var a []any
		for _, word := range words {
			like := "%" + word + "%"
			w = append(w, `(lower(c.last_name || ' ' || c.first_name || ' ' || c.city || ' ' || `+alias+`.title) LIKE ? OR EXISTS (SELECT 1 FROM `+
				linesTable+` l WHERE l.`+fk+` = `+alias+`.id AND lower(l.label || ' ' || l.description) LIKE ?))`)
			a = append(a, like, like)
		}
		if f.Year != "" {
			w = append(w, alias+`.date LIKE ?`)
			a = append(a, f.Year+"-%")
		}
		if f.CustomerID != 0 {
			w = append(w, alias+`.customer_id = ?`)
			a = append(a, f.CustomerID)
		}
		if len(w) == 0 {
			return "", nil
		}
		return " WHERE " + strings.Join(w, " AND "), a
	}
	var parts []string
	if f.Type == "" || f.Type == "devis" {
		w, a := build("q", "quote_lines", "quote_id")
		if f.Status == "open" {
			if w == "" {
				w = " WHERE "
			} else {
				w += " AND "
			}
			w += "q.legacy = 0 AND q.status IN ('draft','sent','accepted')"
		}
		if f.Status != "unpaid" {
			parts = append(parts, quoteSQL+w)
			args = append(args, a...)
		}
	}
	if f.Type != "devis" {
		w, a := build("i", "invoice_lines", "invoice_id")
		cond := []string{}
		switch f.Type {
		case "facture":
			cond = append(cond, "i.kind = 'invoice'")
		case "avoir":
			cond = append(cond, "i.kind = 'credit_note'")
		}
		if f.Status == "unpaid" {
			cond = append(cond, "i.kind = 'invoice' AND i.status IN ('issued','partially_paid')")
		}
		if f.Status == "open" {
			cond = append(cond, "i.status = 'draft'")
		}
		if len(cond) > 0 {
			if w == "" {
				w = " WHERE "
			} else {
				w += " AND "
			}
			w += strings.Join(cond, " AND ")
		}
		parts = append(parts, invSQL+w)
		args = append(args, a...)
	}
	_ = where
	q := strings.Join(parts, " UNION ALL ") + " ORDER BY 3 DESC, 2 DESC"
	if f.Limit > 0 {
		q += " LIMIT " + itoa(f.Limit)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocSummary{}
	for rows.Next() {
		var d DocSummary
		var linked sql.NullInt64
		var discount int64
		if err := rows.Scan(&d.Type, &d.ID, &d.Date, &d.Label, &d.CustomerID, &d.Customer, &d.City, &d.Title, &d.HTCents, &d.TTCCents,
			&d.Status, &d.Legacy, &d.Flags, &linked, &d.LegacySource, &discount); err != nil {
			return nil, err
		}
		d.Customer = strings.TrimSpace(d.Customer)
		if linked.Valid {
			d.LinkedID = &linked.Int64
		}
		if d.HTCents == -1 {
			// Drafts and non-historical quotes: compute from lines.
			table, fk := "quote_lines", "quote_id"
			if d.Type != "devis" {
				table, fk = "invoice_lines", "invoice_id"
			}
			ls, err := loadLines(s.DB, table, fk, d.ID)
			if err != nil {
				return nil, err
			}
			t := domain.ComputeTotals(ls, discount, 0)
			d.HTCents, d.TTCCents = t.HTCents, t.TTCCents
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func itoa(n int) string { return strconv.Itoa(n) }

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// YearStat aggregates activity per year.
type YearStat struct {
	Year          string `json:"year"`
	Quotes        int    `json:"quotes"`
	QuotedCents   int64  `json:"quoted_cents"`
	Invoices      int    `json:"invoices"`
	InvoicedCents int64  `json:"invoiced_cents"`
	Customers     int    `json:"customers"`
}

// Stats is the dashboard summary.
type Stats struct {
	Years        []YearStat   `json:"years"`
	Customers    int          `json:"customers"`
	Quotes       int          `json:"quotes"`
	Invoices     int          `json:"invoices"`
	TotalCents   int64        `json:"total_cents"`
	UnpaidCount  int          `json:"unpaid_count"`
	UnpaidCents  int64        `json:"unpaid_cents"`
	OpenQuotes   int          `json:"open_quotes"`
	OpenCents    int64        `json:"open_cents"`
	Drafts       int          `json:"drafts"`
	TopCustomers []DocSummary `json:"top_customers"`
}

// GetStats computes dashboard figures. Invoiced amounts include credit notes (negative).
func (s *Store) GetStats() (Stats, error) {
	var st Stats
	s.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM customers WHERE archived=0), (SELECT COUNT(*) FROM quotes),
		(SELECT COUNT(*) FROM invoices WHERE status<>'draft' AND kind='invoice'),
		(SELECT COALESCE(SUM(total_ht_cents),0) FROM invoices WHERE status<>'draft'),
		(SELECT COUNT(*) FROM invoices WHERE status='draft')`).Scan(&st.Customers, &st.Quotes, &st.Invoices, &st.TotalCents, &st.Drafts)
	rows, err := s.DB.Query(`SELECT y, SUM(nq), SUM(qc), SUM(ni), SUM(ic), MAX(nc) FROM (
		SELECT substr(date,1,4) y, COUNT(*) nq, SUM(ht_cents) qc, 0 ni, 0 ic, 0 nc FROM quotes GROUP BY y
		UNION ALL
		SELECT substr(date,1,4), 0, 0, SUM(kind='invoice'), SUM(total_ht_cents), COUNT(DISTINCT customer_id) FROM invoices WHERE status<>'draft' GROUP BY 1
	) GROUP BY y ORDER BY y`)
	if err != nil {
		return st, err
	}
	for rows.Next() {
		var y YearStat
		rows.Scan(&y.Year, &y.Quotes, &y.QuotedCents, &y.Invoices, &y.InvoicedCents, &y.Customers)
		st.Years = append(st.Years, y)
	}
	rows.Close()
	unpaid, err := s.ListDocuments(DocFilter{Status: "unpaid"})
	if err != nil {
		return st, err
	}
	for _, d := range unpaid {
		st.UnpaidCount++
		st.UnpaidCents += d.TTCCents
	}
	open, err := s.ListDocuments(DocFilter{Type: "devis", Status: "open"})
	if err != nil {
		return st, err
	}
	for _, d := range open {
		st.OpenQuotes++
		st.OpenCents += d.HTCents
	}
	trows, err := s.DB.Query(`SELECT i.customer_id, c.last_name || ' ' || c.first_name, c.city, COUNT(*), SUM(i.total_ht_cents)
		FROM invoices i JOIN customers c ON c.id=i.customer_id WHERE i.status<>'draft' GROUP BY i.customer_id ORDER BY 5 DESC LIMIT 15`)
	if err != nil {
		return st, err
	}
	defer trows.Close()
	for trows.Next() {
		var d DocSummary
		var n int
		trows.Scan(&d.CustomerID, &d.Customer, &d.City, &n, &d.HTCents)
		d.Label = fmtInt(n)
		d.Customer = strings.TrimSpace(d.Customer)
		st.TopCustomers = append(st.TopCustomers, d)
	}
	return st, nil
}

// Templates returns description templates matching q (most used first).
func (s *Store) Templates(q string, limit int) ([]string, error) {
	args := []any{}
	where := ""
	for _, w := range strings.Fields(strings.ToLower(q)) {
		where += " AND lower(text) LIKE ?"
		args = append(args, "%"+w+"%")
	}
	rows, err := s.DB.Query(`SELECT text FROM description_templates WHERE 1=1`+where+` ORDER BY usage_count DESC, text LIMIT `+fmtInt(limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		rows.Scan(&t)
		out = append(out, t)
	}
	return out, nil
}

// AddTemplate stores a description in the library.
func (s *Store) AddTemplate(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return ValidationError{"Description vide."}
	}
	_, err := s.DB.Exec(`INSERT INTO description_templates(text, usage_count) VALUES (?, 1) ON CONFLICT(text) DO UPDATE SET usage_count = usage_count + 1`, text)
	return err
}

// DeleteTemplate removes a description from the library.
func (s *Store) DeleteTemplate(text string) error {
	_, err := s.DB.Exec(`DELETE FROM description_templates WHERE text = ?`, text)
	return err
}

func bumpTemplates(tx *sql.Tx, lines []domain.Line) error {
	for _, l := range lines {
		if l.Kind == domain.KindItem && strings.TrimSpace(l.Description) != "" {
			if _, err := tx.Exec(`UPDATE description_templates SET usage_count = usage_count + 1 WHERE text = ?`, strings.TrimSpace(l.Description)); err != nil {
				return err
			}
		}
	}
	return nil
}
