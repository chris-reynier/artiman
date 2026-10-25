package demo

import (
	"path/filepath"
	"testing"

	"github.com/chris-reynier/artiman/internal/store"
)

func TestSeed(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "d.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := Seed(s, "2026-10-25"); err != nil {
		t.Fatal(err)
	}
	c, q, i, _ := s.Counts()
	if c != len(customers) || q != 26 || i < 10 {
		t.Fatalf("counts: %d customers, %d quotes, %d invoices", c, q, i)
	}
	st, err := s.GetStats()
	if err != nil || st.Invoices == 0 || st.TotalCents <= 0 || st.OpenQuotes == 0 {
		t.Fatalf("stats = %+v %v", st, err)
	}
	if s.Setting("company_name") == "" {
		t.Fatal("demo company not set")
	}
	// Seeding twice does nothing.
	if err := Seed(s, "2026-10-25"); err != nil {
		t.Fatal(err)
	}
	if c2, _, _, _ := s.Counts(); c2 != c {
		t.Fatal("second seed added data")
	}
}
