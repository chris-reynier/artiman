package server

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chris-reynier/artiman/internal/domain"
	"github.com/chris-reynier/artiman/internal/store"
	"github.com/chris-reynier/artiman/web"
)

// printLine is a line ready for the template.
type printLine struct {
	Kind, Label, Description, Qty, Unit, PU, Total string
}

type totalRow struct {
	Label, Value string
	Strong       bool
}

type printDoc struct {
	Kind        string // devis, facture, avoir
	Heading     string // "Devis Estimatif", "Facture n°0541"
	Reference   string // quote number or credited invoice
	PlaceDate   string
	Company     store.Company
	Party       store.Party
	Title       string
	Site        string
	Lines       []printLine
	Totals      []totalRow
	ValidUntil  string
	Deposit     []string
	Terms       string
	DueDate     string
	Mentions    []string
	Footer      string
	Legacy      bool
	LegacyNote  string
	Draft       bool
	Preview     bool
	LogoURL     string
	ShowSign    bool
	ReverseNote string
}

var printTmpl = template.Must(template.New("print.html").ParseFS(web.Templates, "templates/print.html"))

// currentPrintTmpl re-reads the template from disk in development (ARTIMAN_WEB_DIR set).
func currentPrintTmpl() *template.Template {
	if dir := os.Getenv("ARTIMAN_WEB_DIR"); dir != "" {
		if t, err := template.New("print.html").ParseFiles(filepath.Join(dir, "..", "templates", "print.html")); err == nil {
			return t
		}
	}
	return printTmpl
}

func frDate(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("02/01/2006")
}

func qtyString(c int64) string {
	return domain.FormatDecimal(c, 2)
}

func buildLines(lines []domain.Line) []printLine {
	out := make([]printLine, 0, len(lines))
	for _, l := range lines {
		pl := printLine{Kind: l.Kind, Label: l.Label, Description: l.Description}
		if l.Kind == domain.KindItem {
			pl.Qty, pl.Unit = qtyString(l.QtyC), l.Unit
			pl.PU = domain.FormatDecimal(l.UnitPriceCents, 2)
			pl.Total = domain.FormatDecimal(l.TotalCents(), 2)
		}
		out = append(out, pl)
	}
	return out
}

func totalsRows(t domain.Totals, legacyVat []domain.VatPart, advance int64, franchise bool) []totalRow {
	rows := []totalRow{}
	if t.DiscountCents != 0 {
		rows = append(rows, totalRow{Label: "Total brut HT", Value: domain.Euros(t.GrossCents)},
			totalRow{Label: "Remise", Value: "-" + domain.Euros(t.DiscountCents)})
	}
	if franchise {
		// No VAT at all: a single total, then the advance and the amount due.
		rows = append(rows, totalRow{Label: "Total", Value: domain.Euros(t.TTCCents), Strong: true})
		if advance != 0 {
			rows = append(rows, totalRow{Label: "Acompte", Value: domain.Euros(advance)})
		}
		return append(rows, totalRow{Label: "Net à payer", Value: domain.Euros(t.TTCCents - advance), Strong: true})
	}
	rows = append(rows, totalRow{Label: "Total HT", Value: domain.Euros(t.HTCents), Strong: true})
	vat := t.VAT
	if len(vat) == 0 {
		vat = legacyVat
	}
	for _, v := range vat {
		label := "TVA (" + domain.Rate(v.RateBP) + ")"
		if len(vat) > 1 {
			label += " sur " + domain.Euros(v.BaseCents)
		}
		rows = append(rows, totalRow{Label: label, Value: domain.Euros(v.VatCents)})
	}
	rows = append(rows, totalRow{Label: "Total TTC", Value: domain.Euros(t.TTCCents), Strong: true})
	if advance != 0 {
		rows = append(rows, totalRow{Label: "Acompte", Value: domain.Euros(advance)})
	}
	rows = append(rows, totalRow{Label: "Net à payer", Value: domain.Euros(t.TTCCents - advance), Strong: true})
	return rows
}

func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func footer(c store.Company) string {
	parts := []string{c.Name}
	if c.APE != "" {
		parts = append(parts, "APE "+c.APE)
	}
	if c.Siret != "" {
		parts = append(parts, "Siret n°"+c.Siret)
	}
	if c.VatNumber != "" {
		parts = append(parts, "TVA "+c.VatNumber)
	}
	return strings.Join(parts, " – ")
}

func hasZeroVat(lines []domain.Line) bool {
	for _, l := range lines {
		if l.Kind == domain.KindItem && l.VatBP == 0 && l.UnitPriceCents != 0 {
			return true
		}
	}
	return false
}

func (a *App) renderPrint(w http.ResponseWriter, d printDoc) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := currentPrintTmpl().Execute(w, d); err != nil {
		a.Log("print template: %v", err)
	}
}

func (a *App) printQuote(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	q, err := a.store.GetQuote(id)
	if err != nil {
		a.fail(w, err)
		return
	}
	st, _ := a.store.Settings()
	company := store.CompanyFrom(st)
	t := q.Totals
	d := printDoc{
		Kind: "devis", Heading: "Devis Estimatif", Reference: q.Number, Company: company, Party: store.PartyFrom(*q.Customer),
		PlaceDate: placeDate(company.City, q.Date), Title: q.Title, Site: q.Site, Lines: buildLines(q.Lines),
		Totals: totalsRows(t, nil, 0, company.VatFranchise), Deposit: splitLines(q.DepositTerms), Footer: footer(company), Legacy: q.Legacy,
		Preview: r.URL.Query().Get("preview") == "1", LogoURL: a.logoURL(), ShowSign: true,
	}
	if q.ValidUntil != "" {
		d.ValidUntil = frDate(q.ValidUntil)
	}
	if strings.TrimSpace(company.Insurance) != "" {
		d.Mentions = append(d.Mentions, "Assurance décennale : "+company.Insurance)
	}
	d.ReverseNote = vatNote(company, q.Lines)
	if q.Legacy {
		d.LegacyNote = "Document reconstitué à partir de l'ancien ArtiMan (" + q.LegacySource + "). Montants imprimés à l'époque : " +
			domain.Euros(q.HTCents) + " HT / " + domain.Euros(q.TTCCents) + " TTC."
		d.Mentions = nil
	}
	a.renderPrint(w, d)
}

func (a *App) printInvoice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	inv, err := a.store.GetInvoice(id)
	if err != nil {
		a.fail(w, err)
		return
	}
	st, _ := a.store.Settings()
	company := store.CompanyFrom(st)
	if inv.Company != nil {
		company = *inv.Company // issued: identity frozen at issue time
	}
	heading := inv.Label
	kind := "facture"
	if inv.Kind == "credit_note" {
		kind = "avoir"
	}
	d := printDoc{
		Kind: kind, Heading: heading, Company: company, Party: inv.Party, PlaceDate: placeDate(company.City, inv.Date),
		Title: inv.Title, Site: inv.Site, Lines: buildLines(inv.Lines), Totals: totalsRows(inv.Totals, inv.Totals.VAT, inv.AdvanceCents, company.VatFranchise),
		Terms: inv.Terms, Footer: footer(company), Legacy: inv.Legacy, Draft: inv.Status == "draft",
		Preview: r.URL.Query().Get("preview") == "1", LogoURL: a.logoURL(), Mentions: splitLines(inv.Mentions),
	}
	if inv.CreditedLabel != "" {
		d.Reference = "Avoir sur " + inv.CreditedLabel
	}
	if d.Draft {
		d.Heading = "Facture (brouillon – non émise)"
		if inv.Kind == "credit_note" {
			d.Heading = "Avoir (brouillon – non émis)"
		}
		d.Terms = st["payment_terms"]
		d.Mentions = splitLines(store.Mentions(st, inv.Party.IsBusiness))
	}
	if inv.DueDate != "" && inv.Kind == "invoice" {
		d.DueDate = frDate(inv.DueDate)
	}
	d.ReverseNote = vatNote(company, inv.Lines)
	if inv.Legacy {
		d.LegacyNote = "Document reconstitué à partir de l'ancien ArtiMan (" + inv.LegacySource + ")."
		d.Mentions = nil
		d.Terms = "Règlement à la réception de la facture"
	}
	a.renderPrint(w, d)
}

// logoURL is empty when no logo was installed (the template then prints the company name as text).
func (a *App) logoURL() string {
	if fileExists(filepath.Join(a.DataDir, "logo.png")) {
		return "/logo"
	}
	return ""
}

// placeDate renders "Ville le 25/10/2026", or "Le 25/10/2026" when no city is configured.
func placeDate(city, iso string) string {
	if strings.TrimSpace(city) == "" {
		return "Le " + frDate(iso)
	}
	return fmt.Sprintf("%s le %s", city, frDate(iso))
}

// vatNote returns the VAT wording printed under the lines: VAT franchise (micro-entreprise), reverse charge
// for 0 % lines, or the configured reduced-rate wording when 5.5 % or 10 % is used.
func vatNote(c store.Company, lines []domain.Line) string {
	if c.VatFranchise {
		return c.FranchiseNote
	}
	var notes []string
	if hasZeroVat(lines) {
		notes = append(notes, "Autoliquidation : TVA due par le preneur assujetti (art. 283-2 nonies du CGI).")
	}
	if c.ReducedVatNote != "" {
		for _, l := range lines {
			if l.Kind == domain.KindItem && (l.VatBP == 550 || l.VatBP == 1000) {
				notes = append(notes, c.ReducedVatNote)
				break
			}
		}
	}
	return strings.Join(notes, "\n")
}
