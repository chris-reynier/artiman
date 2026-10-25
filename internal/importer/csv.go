package importer

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/chris-reynier/artiman/internal/store"
)

// columnAliases maps normalised CSV headers (as exported by Excel or other software) to customer fields.
var columnAliases = map[string]string{
	"civilite": "civility", "titre": "civility", "civ": "civility",
	"nom": "last_name", "name": "last_name", "raison sociale": "last_name", "societe": "last_name", "client": "last_name", "nom client": "last_name",
	"prenom": "first_name", "first name": "first_name",
	"adresse": "address", "adresse 1": "address", "adresse1": "address", "rue": "address", "address": "address",
	"adresse 2": "address2", "adresse2": "address2", "complement": "address2", "complement d adresse": "address2",
	"code postal": "zip", "cp": "zip", "codepostal": "zip", "zip": "zip",
	"ville": "city", "commune": "city", "city": "city", "localite": "city",
	"telephone": "phone", "tel": "phone", "fixe": "phone", "telephone fixe": "phone", "phone": "phone",
	"portable": "mobile", "mobile": "mobile", "gsm": "mobile", "telephone portable": "mobile",
	"email": "email", "e mail": "email", "mail": "email", "courriel": "email", "adresse email": "email",
	"notes": "notes", "remarques": "notes", "commentaire": "notes", "commentaires": "notes",
}

func normHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(s, "\uFEFF")))
	r := strings.NewReplacer("é", "e", "è", "e", "ê", "e", "ë", "e", "à", "a", "â", "a", "ç", "c", "ô", "o", "î", "i", "ï", "i",
		"-", " ", "_", " ", ".", " ", "'", " ", "’", " ")
	return strings.Join(strings.Fields(r.Replace(s)), " ")
}

// toUTF8 accepts UTF-8 (with or without BOM) or Windows-1252, the encoding Excel uses for "CSV (séparateur : point-virgule)".
func toUTF8(b []byte) string {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(b) {
		return string(b)
	}
	return decodeLatin1(b)
}

// ParseCustomersCSV reads a customer list. The delimiter (; , or tab) is detected from the header line.
func ParseCustomersCSV(data []byte) ([]store.Customer, error) {
	text := toUTF8(data)
	first, _, _ := strings.Cut(text, "\n")
	delim := ';'
	best := strings.Count(first, ";")
	for _, d := range []rune{',', '\t'} {
		if n := strings.Count(first, string(d)); n > best {
			delim, best = d, n
		}
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = delim
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("fichier CSV illisible : %w", err)
	}
	cols := map[int]string{}
	hasName := false
	for i, h := range header {
		if f, ok := columnAliases[normHeader(h)]; ok {
			cols[i] = f
			hasName = hasName || f == "last_name"
		}
	}
	if !hasName {
		return nil, store.ValidationError{Msg: "Colonne « Nom » introuvable. La première ligne du fichier doit contenir les titres des colonnes (Nom, Prénom, Adresse, Code postal, Ville…)."}
	}
	var out []store.Customer
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("ligne %d illisible : %w", len(out)+2, err)
		}
		var c store.Customer
		var addr2 string
		for i, v := range rec {
			v = strings.TrimSpace(v)
			switch cols[i] {
			case "civility":
				c.Civility = normCivility(v)
			case "last_name":
				c.LastName = v
			case "first_name":
				c.FirstName = v
			case "address":
				c.Address = v
			case "address2":
				addr2 = v
			case "zip":
				c.Zip = v
			case "city":
				c.City = v
			case "phone":
				c.Phone = v
			case "mobile":
				c.Mobile = v
			case "email":
				c.Email = v
			case "notes":
				c.Notes = v
			}
		}
		if addr2 != "" {
			c.Address = strings.TrimSpace(c.Address + "\n" + addr2)
		}
		if c.LastName == "" {
			continue
		}
		c.IsBusiness = c.Civility == ""
		out = append(out, c)
	}
	return out, nil
}

func normCivility(v string) string {
	switch strings.ToLower(strings.TrimSuffix(strings.TrimSpace(v), ".")) {
	case "m", "mr", "monsieur":
		return "M."
	case "mme", "madame":
		return "Mme"
	case "mlle", "melle", "mademoiselle":
		return "Mlle"
	case "m et mme", "mr et mme", "m. et mme", "monsieur et madame":
		return "M. et Mme"
	}
	return ""
}

// ImportCustomers saves the parsed customers, skipping those already present (same name, first name and postcode).
func ImportCustomers(s *store.Store, customers []store.Customer) (created, skipped int, err error) {
	existing, err := s.ListCustomers(true)
	if err != nil {
		return 0, 0, err
	}
	key := func(c store.Customer) string {
		return strings.ToLower(strings.Join(strings.Fields(c.LastName+" "+c.FirstName+" "+strings.ReplaceAll(c.Zip, " ", "")), " "))
	}
	seen := map[string]bool{}
	for _, c := range existing {
		seen[key(c)] = true
	}
	for _, c := range customers {
		k := key(c)
		if seen[k] {
			skipped++
			continue
		}
		if _, err := s.SaveCustomer(c); err != nil {
			return created, skipped, err
		}
		seen[k] = true
		created++
	}
	return created, skipped, nil
}
