// Package demo fills an empty database with fictitious data, for trying the program and for screenshots.
// Every name, address and identifier below is invented.
package demo

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/chris-reynier/artiman/internal/domain"
	"github.com/chris-reynier/artiman/internal/store"
)

// Settings is the fictitious company used by the demo.
var Settings = map[string]string{
	"company_name":    "Dupont Peinture & Déco",
	"company_trades":  "Peinture intérieure et extérieure\nRevêtements muraux\nRavalement de façades",
	"company_address": "12 rue des Artisans\n73100 VILLENEUVE-SUR-LAC",
	"company_city":    "Villeneuve-sur-Lac",
	"company_phone":   "06 00 00 00 00",
	"company_email":   "contact@dupont-peinture.exemple",
	"company_siret":   "123 456 789 00012",
	"company_ape":     "4334 Z",
	"insurance":       "Exemple Assurances, contrat n° 000000 (fictif), France métropolitaine",
	"legal_todo":      "0",
	"invoice_start":   "1",
}

var customers = []store.Customer{
	{Civility: "M. et Mme", LastName: "MARTIN", FirstName: "Paul", Address: "4 allée des Tilleuls", Zip: "73100", City: "VILLENEUVE-SUR-LAC", Phone: "06 11 11 11 11"},
	{Civility: "Mme", LastName: "BERNARD", FirstName: "Claire", Address: "18 rue du Moulin", Zip: "73200", City: "SAINT-JEAN-DES-PRÉS"},
	{Civility: "M.", LastName: "PETIT", FirstName: "Lucas", Address: "2 impasse des Lilas", Zip: "73100", City: "VILLENEUVE-SUR-LAC", Email: "l.petit@exemple.fr"},
	{Civility: "Mme", LastName: "ROUX", FirstName: "Élodie", Address: "Le Hameau", Zip: "73300", City: "MONTCLAIR"},
	{Civility: "M. et Mme", LastName: "FOURNIER", FirstName: "Marc", Address: "35 avenue de la Gare", Zip: "73200", City: "SAINT-JEAN-DES-PRÉS"},
	{LastName: "SYNDIC DES ROCHERS", Address: "Résidence Les Rochers\n9 boulevard du Lac", Zip: "73100", City: "VILLENEUVE-SUR-LAC", IsBusiness: true},
	{LastName: "BOULANGERIE DU CENTRE", Address: "1 place de l'Église", Zip: "73300", City: "MONTCLAIR", IsBusiness: true},
	{Civility: "M.", LastName: "GIRARD", FirstName: "Thomas", Address: "7 chemin des Vignes", Zip: "73400", City: "BELLEVUE"},
	{Civility: "Mme", LastName: "LEFÈVRE", FirstName: "Anne", Address: "22 rue Victor Hugo", Zip: "73100", City: "VILLENEUVE-SUR-LAC"},
	{Civility: "M. et Mme", LastName: "MOREL", FirstName: "Julien", Address: "La Combe", Zip: "73400", City: "BELLEVUE"},
}

type tpl struct {
	label, desc, unit string
	minQty, maxQty    int64 // hundredths
	price             int64 // cents
}

var rooms = []string{"SALON", "CUISINE", "CHAMBRE 1", "CHAMBRE 2", "SALLE DE BAINS", "COULOIR", "ENTRÉE", "BUREAU"}

var works = []tpl{
	{"MURS", "Lessivage, rebouchage, ponçage + 2 couches de peinture velours blanc", "m²", 2000, 6000, 2150},
	{"PLAFOND", "Préparation + 2 couches de peinture mate blanche", "m²", 1000, 3000, 1980},
	{"BOISERIES", "Lessivage, ponçage + 2 couches de laque satinée", "m²", 300, 1200, 2650},
	{"PORTE", "Ponçage + 2 couches de peinture satinée", "U", 100, 300, 9500},
	{"PAPIER PEINT", "Arrachage, encollage et pose de papier peint (fourni par le client)", "m²", 1500, 3500, 1890},
	{"RADIATEUR", "Brossage + 1 couche antirouille + 2 couches de laque", "U", 100, 300, 7500},
	{"PLINTHES", "Ponçage + 2 couches de peinture satinée", "ml", 1000, 3000, 650},
}

var outdoor = []tpl{
	{"FAÇADE", "Nettoyage haute pression, traitement fongicide + 2 couches de peinture pliolite", "m²", 6000, 15000, 2800},
	{"VOLETS", "Ponçage + 3 couches de lasure", "U", 400, 1000, 9000},
	{"DESSOUS DE TOIT", "Brossage + 2 couches de lasure", "ml", 1000, 3000, 1800},
}

func between(r *rand.Rand, lo, hi int64) int64 { return lo + r.Int63n(hi-lo+1) }

// qty draws a quantity in hundredths: whole numbers for units, one decimal for surfaces and lengths.
func qty(r *rand.Rand, w tpl) int64 {
	q := between(r, w.minQty, w.maxQty)
	if w.unit == "U" {
		return q / 100 * 100
	}
	return q / 10 * 10
}

func quoteLines(r *rand.Rand, business bool) []domain.Line {
	vat := int64(1000)
	if business {
		vat = 2000
	}
	var lines []domain.Line
	if r.Intn(4) == 0 {
		lines = append(lines, domain.Line{Kind: domain.KindSection, Label: "EXTÉRIEUR"})
		for _, w := range outdoor[:1+r.Intn(len(outdoor))] {
			lines = append(lines, domain.Line{Kind: domain.KindItem, Label: w.label, Description: w.desc, Unit: w.unit,
				QtyC: qty(r, w), UnitPriceCents: w.price, VatBP: vat})
		}
	} else {
		for _, room := range r.Perm(len(rooms))[:1+r.Intn(3)] {
			lines = append(lines, domain.Line{Kind: domain.KindSection, Label: rooms[room]})
			for _, wi := range r.Perm(len(works))[:2+r.Intn(2)] {
				w := works[wi]
				lines = append(lines, domain.Line{Kind: domain.KindItem, Label: w.label, Description: w.desc, Unit: w.unit,
					QtyC: qty(r, w), UnitPriceCents: w.price, VatBP: vat})
			}
		}
	}
	lines = append(lines, domain.Line{Kind: domain.KindItem, Label: "PROTECTION", Description: "Protection des sols et du mobilier, nettoyage de fin de chantier",
		Unit: "forfait", QtyC: 100, UnitPriceCents: between(r, 60, 150) * 100, VatBP: vat})
	return lines
}

var titles = []string{"Rafraîchissement appartement", "Rénovation peinture", "Travaux de peinture", "Remise en état après dégât des eaux",
	"Ravalement", "Peinture parties communes", "Pose de papier peint"}

// Seed fills an empty database. today is the reference date ("2006-01-02").
func Seed(s *store.Store, today string) error {
	if c, _, _, err := s.Counts(); err != nil || c > 0 {
		return err
	}
	if err := s.SaveSettings(Settings); err != nil {
		return err
	}
	ref, err := time.Parse("2006-01-02", today)
	if err != nil {
		return err
	}
	r := rand.New(rand.NewSource(42))
	var ids []int64
	for _, c := range customers {
		id, err := s.SaveCustomer(c)
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}
	// 26 quotes spread over the last 18 months, most of them invoiced.
	type plan struct {
		days int
		cust int
	}
	var plans []plan
	for i := 0; i < 26; i++ {
		plans = append(plans, plan{days: 575 - i*22 - r.Intn(6), cust: (i*7 + r.Intn(2)) % len(ids)})
	}
	for i, p := range plans {
		date := ref.AddDate(0, 0, -p.days)
		business := customers[p.cust].IsBusiness
		q := store.Quote{CustomerID: ids[p.cust], Date: date.Format("2006-01-02"), ValidUntil: date.AddDate(0, 0, 30).Format("2006-01-02"),
			Title: titles[r.Intn(len(titles))], Status: "sent", DepositTerms: "*30% versés à la signature du devis\n*30% versés au début des travaux",
			Lines: quoteLines(r, business)}
		qid, err := s.SaveQuote(q)
		if err != nil {
			return fmt.Errorf("demo quote %d: %w", i, err)
		}
		age := p.days
		switch {
		case age < 25:
			continue // recent quotes stay open
		case i%7 == 3:
			s.SetQuoteStatus(qid, "refused")
			continue
		}
		invDate := date.AddDate(0, 0, 20+r.Intn(15))
		if invDate.After(ref) {
			invDate = ref
		}
		invID, err := s.CreateInvoiceFromQuote(qid, invDate.Format("2006-01-02"))
		if err != nil {
			return err
		}
		inv, err := s.GetInvoice(invID)
		if err != nil {
			return err
		}
		if r.Intn(3) == 0 {
			inv.AdvanceCents = domain.RoundDiv(inv.Totals.TTCCents*30, 100) / 100 * 100
			if _, err := s.SaveDraftInvoice(inv); err != nil {
				return err
			}
		}
		if _, err := s.IssueInvoice(invID); err != nil {
			return err
		}
		if age > 75 || r.Intn(3) == 0 {
			paid := invDate.AddDate(0, 0, 5+r.Intn(25))
			if paid.After(ref) {
				paid = ref
			}
			if err := s.SetInvoicePaid(invID, true, paid.Format("2006-01-02")); err != nil {
				return err
			}
		}
	}
	return nil
}
