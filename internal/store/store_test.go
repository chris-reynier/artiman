package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/chris-reynier/artiman/internal/domain"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleQuote(t *testing.T, s *Store) (int64, int64) {
	t.Helper()
	cid, err := s.SaveCustomer(Customer{Civility: "M.", LastName: "DUPONT", FirstName: "Jean", Address: "1 rue A", Zip: "73000", City: "CHAMBERY"})
	if err != nil {
		t.Fatal(err)
	}
	qid, err := s.SaveQuote(Quote{CustomerID: cid, Date: "2026-10-25", Lines: []domain.Line{
		{Kind: domain.KindSection, Label: "SALON"},
		{Kind: domain.KindItem, Label: "MURS", Description: "2 couches", Unit: "m²", QtyC: 3500, UnitPriceCents: 2050, VatBP: 1000},
		{Kind: domain.KindItem, Label: "", Description: "", QtyC: 0}, // empty editor row, dropped
	}})
	if err != nil {
		t.Fatal(err)
	}
	return cid, qid
}

func TestQuoteNumberingAndTotals(t *testing.T) {
	s := newStore(t)
	_, qid := sampleQuote(t, s)
	q, err := s.GetQuote(qid)
	if err != nil {
		t.Fatal(err)
	}
	if q.Number != "D2026-001" || len(q.Lines) != 2 || q.Totals.HTCents != 71750 || q.HTCents != 71750 {
		t.Fatalf("quote = %+v", q)
	}
	id2, err := s.DuplicateQuote(qid, "2026-11-02", 30)
	if err != nil {
		t.Fatal(err)
	}
	q2, _ := s.GetQuote(id2)
	if q2.Number != "D2026-002" || q2.ValidUntil != "2026-12-02" {
		t.Fatalf("dup = %+v", q2)
	}
}

func TestInvoiceLifecycle(t *testing.T) {
	s := newStore(t)
	if err := s.SaveSettings(map[string]string{"company_name": "Exemple Peinture", "company_siret": "000 000 000 00000", "invoice_start": "541"}); err != nil {
		t.Fatal(err)
	}
	_, qid := sampleQuote(t, s)
	invID, err := s.CreateInvoiceFromQuote(qid, "2026-10-26")
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := s.GetInvoice(invID)
	if inv.Number != nil || inv.Status != "draft" || inv.Totals.TTCCents != 78925 {
		t.Fatalf("draft = %+v", inv)
	}
	// Advance on the draft
	inv.AdvanceCents = 10000
	if _, err := s.SaveDraftInvoice(inv); err != nil {
		t.Fatal(err)
	}
	inv, err = s.IssueInvoice(invID)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Number == nil || *inv.Number != 541 || inv.Label != "Facture n°0541" || inv.Totals.NetCents != 68925 {
		t.Fatalf("issued = %+v", inv)
	}
	if inv.Party.Display != "M. DUPONT Jean" || inv.Company == nil || inv.Company.Siret == "" {
		t.Fatalf("snapshots = %+v %+v", inv.Party, inv.Company)
	}
	q, _ := s.GetQuote(qid)
	if q.Status != "invoiced" || q.Editable() {
		t.Fatalf("quote after invoice = %s", q.Status)
	}
	// Immutability enforced by the database
	if _, err := s.DB.Exec(`UPDATE invoices SET total_ttc_cents = 1 WHERE id = ?`, invID); err == nil || !strings.Contains(err.Error(), "interdite") {
		t.Fatalf("update of issued invoice not blocked: %v", err)
	}
	if _, err := s.DB.Exec(`DELETE FROM invoices WHERE id = ?`, invID); err == nil {
		t.Fatal("delete of issued invoice not blocked")
	}
	if _, err := s.DB.Exec(`UPDATE invoice_lines SET qty_c = 1 WHERE invoice_id = ?`, invID); err == nil {
		t.Fatal("line update not blocked")
	}
	if _, err := s.SaveDraftInvoice(inv); err == nil {
		t.Fatal("saving an issued invoice as draft should fail")
	}
	// Payment status only (no amounts recorded)
	if inv.RemainingCents != 68925 {
		t.Fatalf("remaining before payment = %d", inv.RemainingCents)
	}
	if err := s.SetInvoicePaid(invID, true, "bad"); err == nil {
		t.Fatal("invalid date accepted")
	}
	if err := s.SetInvoicePaid(invID, true, "2026-11-05"); err != nil {
		t.Fatal(err)
	}
	inv, _ = s.GetInvoice(invID)
	if inv.Status != "paid" || inv.PaidAt != "2026-11-05" || inv.RemainingCents != 0 {
		t.Fatalf("paid = %s %s %d", inv.Status, inv.PaidAt, inv.RemainingCents)
	}
	if err := s.SetInvoicePaid(invID, false, ""); err != nil {
		t.Fatal(err)
	}
	inv, _ = s.GetInvoice(invID)
	if inv.Status != "issued" || inv.PaidAt != "" {
		t.Fatalf("unpaid = %s %s", inv.Status, inv.PaidAt)
	}
	s.SetInvoicePaid(invID, true, "2026-11-05")
	// Credit note takes the next number in the same sequence
	cnID, err := s.CreateCreditNote(invID, "2026-11-10")
	if err != nil {
		t.Fatal(err)
	}
	cn, err := s.IssueInvoice(cnID)
	if err != nil {
		t.Fatal(err)
	}
	if *cn.Number != 542 || cn.Totals.TTCCents != -78925 || cn.Label != "Avoir n°0542" || cn.CreditedLabel != "Facture n°0541" {
		t.Fatalf("credit note = %+v", cn)
	}
	inv, _ = s.GetInvoice(invID)
	if len(inv.CreditNotes) != 1 {
		t.Fatalf("credit notes on invoice = %v", inv.CreditNotes)
	}
	// Draft invoices can be deleted, issued ones cannot
	d, _ := s.SaveDraftInvoice(Invoice{CustomerID: inv.CustomerID, Date: "2026-11-11"})
	if err := s.DeleteDraftInvoice(d); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDraftInvoice(invID); err == nil {
		t.Fatal("issued invoice deleted")
	}
	// A draft cannot be given a number directly
	d, _ = s.SaveDraftInvoice(Invoice{CustomerID: inv.CustomerID, Date: "2026-11-11"})
	if _, err := s.DB.Exec(`UPDATE invoices SET number = 999 WHERE id = ?`, d); err == nil {
		t.Fatal("number on draft not blocked")
	}
	if _, err := s.IssueInvoice(d); err == nil {
		t.Fatal("empty invoice issued")
	}
}

func TestCustomerDeleteProtection(t *testing.T) {
	s := newStore(t)
	cid, _ := sampleQuote(t, s)
	if err := s.DeleteCustomer(cid); err == nil {
		t.Fatal("customer with documents deleted")
	}
	free, _ := s.SaveCustomer(Customer{LastName: "SEUL"})
	if err := s.DeleteCustomer(free); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveCustomer(Customer{LastName: "  "}); err == nil {
		t.Fatal("empty name accepted")
	}
}

func TestDocumentsAndStats(t *testing.T) {
	s := newStore(t)
	_, qid := sampleQuote(t, s)
	id, _ := s.CreateInvoiceFromQuote(qid, "2026-10-26")
	s.IssueInvoice(id)
	docs, err := s.ListDocuments(DocFilter{Query: "couches"})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("docs = %+v", docs)
	}
	unpaid, _ := s.ListDocuments(DocFilter{Status: "unpaid"})
	if len(unpaid) != 1 || unpaid[0].TTCCents != 78925 {
		t.Fatalf("unpaid = %+v", unpaid)
	}
	st, err := s.GetStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Invoices != 1 || st.TotalCents != 71750 || len(st.Years) != 1 || st.UnpaidCount != 1 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestSettingsAndBackup(t *testing.T) {
	s := newStore(t)
	if err := s.SaveSettings(map[string]string{"insurance": "AXA, France", "unknown": "x"}); err != nil {
		t.Fatal(err)
	}
	st, _ := s.Settings()
	if st["insurance"] != "AXA, France" || st["quote_validity_days"] != "30" {
		t.Fatalf("settings = %v", st["insurance"])
	}
	if _, ok := st["unknown"]; ok {
		t.Fatal("unknown key stored")
	}
	dst := filepath.Join(t.TempDir(), "b.db")
	if err := s.BackupTo(dst); err != nil {
		t.Fatal(err)
	}
	b, err := Open(dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Setting("insurance") != "AXA, France" {
		t.Fatal("backup content mismatch")
	}
}
