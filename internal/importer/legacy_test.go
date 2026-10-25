package importer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chris-reynier/artiman/internal/store"
)

func TestSeedCompany(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "t.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	imp := filepath.Join(dir, "import")
	os.MkdirAll(imp, 0o755)
	os.WriteFile(filepath.Join(imp, "settings.json"), []byte(`{"company_name":"Exemple Peinture","company_city":"Ville"}`), 0o644)
	os.WriteFile(filepath.Join(imp, "logo.png"), []byte("\x89PNG fake"), 0o644)
	ok, err := SeedCompany(s, imp, dir)
	if err != nil || !ok {
		t.Fatalf("seed: %v %v", ok, err)
	}
	if s.Setting("company_name") != "Exemple Peinture" {
		t.Fatal("company name not applied")
	}
	if _, err := os.Stat(filepath.Join(dir, "logo.png")); err != nil {
		t.Fatal("logo not installed")
	}
	if ok, _ := SeedCompany(s, imp, dir); ok {
		t.Fatal("seed must run only once")
	}
}

func TestSplitName(t *testing.T) {
	cases := map[string][2]string{
		"DUPONT Jean François":           {"DUPONT", "Jean François"},
		"DURAND IMMOBILIER":              {"DURAND IMMOBILIER", ""},
		"DE LA FONTAINE Suzanne":         {"DE LA FONTAINE", "Suzanne"},
		"PEINTURES DU LAC Paul MARTINEZ": {"PEINTURES DU LAC", "Paul MARTINEZ"},
	}
	for in, want := range cases {
		a, b := splitName(in)
		if a != want[0] || b != want[1] {
			t.Errorf("splitName(%q) = %q,%q", in, a, b)
		}
	}
}

const sample = `{"clients":[{"key":"DUPONT JEAN","name":"DUPONT Jean","civility":"M","city":"CHAMBERY","zip":"73000","docs":[0,1]}],
"docs":[
 {"id":0,"kind":"devis","date":"2015-03-01","client":"DUPONT JEAN","ht":100,"tva_rate":10,"tva":10,"ttc":110,"path":"Devis/a.doc",
  "items":[{"section":"SALON","label":"MURS","qty":10,"unit":"m²","pu":10,"total":100,"desc":"2 couches"}],"flags":[],"address":["1 rue A","73 000 CHAMBERY"],"to_facture":1},
 {"id":1,"kind":"facture","date":"2015-04-01","client":"DUPONT JEAN","number":"0201","ht":100,"tva_rate":10,"tva":10,"ttc":110,"acompte":30,
  "path":"Facture/F0201.doc","items":[{"section":"SALON","label":"MURS","qty":10,"unit":"m²","pu":10,"total":100,"desc":"2 couches"}],
  "flags":["x"],"address":["1 rue A","73 000 CHAMBERY"],"from_devis":0,"zip":"73000","city":"CHAMBERY"}]}`

func TestImport(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "t.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hp := filepath.Join(dir, "h.json")
	os.WriteFile(hp, []byte(sample), 0o644)
	dp := filepath.Join(dir, "description.txt")
	os.WriteFile(dp, []byte("Lessivage + 2 couches de peinture satin\xe9e\xa7Pon\xe7age\xa7"), 0o644)
	res, err := Import(s, hp, dp)
	if err != nil {
		t.Fatal(err)
	}
	if res.Customers != 1 || res.Quotes != 1 || res.Invoices != 1 || res.Templates != 3 {
		t.Fatalf("result = %+v", res)
	}
	docs, _ := s.ListDocuments(store.DocFilter{})
	if len(docs) != 2 {
		t.Fatalf("docs = %+v", docs)
	}
	var inv store.Invoice
	for _, d := range docs {
		if d.Type == "facture" {
			inv, _ = s.GetInvoice(d.ID)
		}
	}
	if inv.Label != "Facture n°0201" || inv.Status != "paid" || inv.Totals.TTCCents != 11000 || inv.AdvanceCents != 3000 || inv.QuoteID == nil {
		t.Fatalf("invoice = %+v", inv)
	}
	q, _ := s.GetQuote(*inv.QuoteID)
	if q.Status != "invoiced" || !q.Legacy || len(q.Lines) != 2 {
		t.Fatalf("quote = %+v", q)
	}
	c, _ := s.GetCustomer(q.CustomerID)
	if c.Civility != "M." || c.LastName != "DUPONT" || c.FirstName != "Jean" || c.Address != "1 rue A" {
		t.Fatalf("customer = %+v", c)
	}
	tpl, _ := s.Templates("satinée", 10)
	if len(tpl) != 1 {
		t.Fatalf("templates = %v", tpl)
	}
	if _, err := Import(s, hp, ""); err == nil {
		t.Fatal("second import should be refused")
	}
}
