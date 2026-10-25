package importer

import (
	"path/filepath"
	"testing"

	"github.com/chris-reynier/artiman/internal/store"
)

func TestParseCustomersCSVExcelLatin1(t *testing.T) {
	// Excel "CSV (séparateur : point-virgule)" export: Windows-1252, semicolons, accented headers.
	data := []byte("Civilit\xe9;Nom;Pr\xe9nom;Adresse;Adresse 2;Code postal;Ville;T\xe9l\xe9phone;E-mail\r\n" +
		"Mme;DURAND;H\xe9l\xe8ne;3 rue des Pins;B\xe2t. A;73000;CHAMBERY;04 00 00 00 00;h.durand@exemple.fr\r\n" +
		";PEINTURES DU LAC;;ZA Nord;;74000;ANNECY;;\r\n" +
		";;;ligne sans nom;;;;;\r\n")
	cs, err := ParseCustomersCSV(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("customers = %+v", cs)
	}
	c := cs[0]
	if c.Civility != "Mme" || c.FirstName != "Hélène" || c.Address != "3 rue des Pins\nBât. A" || c.Zip != "73000" || c.Email != "h.durand@exemple.fr" || c.IsBusiness {
		t.Fatalf("first = %+v", c)
	}
	if !cs[1].IsBusiness || cs[1].LastName != "PEINTURES DU LAC" {
		t.Fatalf("second = %+v", cs[1])
	}
}

func TestParseCustomersCSVCommaUTF8AndErrors(t *testing.T) {
	cs, err := ParseCustomersCSV([]byte("\xef\xbb\xbfname,first name,city\n\"MARTIN, SARL\",,Lyon\n"))
	if err != nil || len(cs) != 1 || cs[0].LastName != "MARTIN, SARL" || cs[0].City != "Lyon" {
		t.Fatalf("comma utf8: %+v %v", cs, err)
	}
	if _, err := ParseCustomersCSV([]byte("a;b;c\n1;2;3\n")); err == nil {
		t.Fatal("missing name column accepted")
	}
}

func TestImportCustomersSkipsDuplicates(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SaveCustomer(store.Customer{LastName: "DURAND", FirstName: "Hélène", Zip: "73 000"})
	created, skipped, err := ImportCustomers(s, []store.Customer{
		{LastName: "DURAND", FirstName: "Hélène", Zip: "73000"},
		{LastName: "NOUVEAU"}, {LastName: "NOUVEAU"},
	})
	if err != nil || created != 1 || skipped != 2 {
		t.Fatalf("created=%d skipped=%d err=%v", created, skipped, err)
	}
}
