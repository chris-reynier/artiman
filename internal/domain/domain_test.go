package domain

import "testing"

func TestRoundDiv(t *testing.T) {
	cases := []struct{ a, b, want int64 }{
		{5, 10, 1}, {4, 10, 0}, {-5, 10, -1}, {-4, 10, 0}, {15, 10, 2}, {25, 10, 3}, {7, -2, -4},
	}
	for _, c := range cases {
		if got := RoundDiv(c.a, c.b); got != c.want {
			t.Errorf("RoundDiv(%d,%d)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseDecimal(t *testing.T) {
	cases := []struct {
		in   string
		dec  int
		want int64
	}{
		{"20,50", 2, 2050}, {"20.5", 2, 2050}, {"1 234,56", 2, 123456}, {"35", 2, 3500},
		{"0,005", 2, 1}, {"0,004", 2, 0}, {"-3,2", 2, -320}, {"", 2, 0}, {"5,5", 2, 550}, {"12,345 €", 2, 1235},
	}
	for _, c := range cases {
		got, err := ParseDecimal(c.in, c.dec)
		if err != nil || got != c.want {
			t.Errorf("ParseDecimal(%q)=%d,%v want %d", c.in, got, err, c.want)
		}
	}
	if _, err := ParseDecimal("12a", 2); err == nil {
		t.Error("expected error for 12a")
	}
}

func TestFormat(t *testing.T) {
	if got := Euros(123456789); got != "1\u202f234\u202f567,89 €" {
		t.Errorf("Euros=%q", got)
	}
	if got := Euros(-5); got != "-0,05 €" {
		t.Errorf("Euros neg=%q", got)
	}
	if got := Rate(550); got != "5,5 %" {
		t.Errorf("Rate=%q", got)
	}
	if got := Rate(2000); got != "20 %" {
		t.Errorf("Rate=%q", got)
	}
}

// Figures taken from documents produced by the old ArtiMan (customer details removed).
func TestComputeTotalsLegacyDocuments(t *testing.T) {
	// 2009 quote: 5 lines at 5.5 %, HT 2864.11. The old program printed TVA 157.52
	// because it truncated 157.526; we round to the nearest cent (157.53), the standard rule.
	lines := []Line{
		{Kind: KindItem, QtyC: 3000, UnitPriceCents: 5120, VatBP: 550},
		{Kind: KindItem, QtyC: 2073, UnitPriceCents: 990, VatBP: 550},
		{Kind: KindItem, QtyC: 3000, UnitPriceCents: 1630, VatBP: 550},
		{Kind: KindItem, QtyC: 2000, UnitPriceCents: 1625, VatBP: 550},
		{Kind: KindItem, QtyC: 2073, UnitPriceCents: 1490, VatBP: 550},
		{Kind: KindSection, Label: "SOL"},
	}
	tt := ComputeTotals(lines, 0, 0)
	if tt.HTCents != 286411 || tt.VatCents != 15753 || tt.TTCCents != 302164 {
		t.Errorf("2009 quote totals = %+v", tt)
	}
	// 2017 invoice: HT 718.20, TVA 10 % 71.82, TTC 790.02
	lines = []Line{
		{Kind: KindItem, QtyC: 2070, UnitPriceCents: 1700, VatBP: 1000},
		{Kind: KindItem, QtyC: 100, UnitPriceCents: 8000, VatBP: 1000},
		{Kind: KindItem, QtyC: 2070, UnitPriceCents: 900, VatBP: 1000},
		{Kind: KindItem, QtyC: 100, UnitPriceCents: 10000, VatBP: 1000},
	}
	tt = ComputeTotals(lines, 0, 0)
	if tt.HTCents != 71820 || tt.VatCents != 7182 || tt.TTCCents != 79002 || tt.NetCents != 79002 {
		t.Errorf("2017 invoice totals = %+v", tt)
	}
}

func TestComputeTotalsDiscountMixedRatesAdvance(t *testing.T) {
	lines := []Line{
		{Kind: KindItem, QtyC: 100, UnitPriceCents: 100000, VatBP: 1000},
		{Kind: KindItem, QtyC: 200, UnitPriceCents: 25000, VatBP: 2000},
		{Kind: KindText, Label: "note"},
	}
	tt := ComputeTotals(lines, 1000, 30000) // 10 % discount, 300 € advance
	if tt.GrossCents != 150000 || tt.HTCents != 135000 || tt.DiscountCents != 15000 {
		t.Fatalf("totals = %+v", tt)
	}
	if len(tt.VAT) != 2 || tt.VAT[0].VatCents != 9000 || tt.VAT[1].VatCents != 9000 {
		t.Fatalf("vat = %+v", tt.VAT)
	}
	if tt.TTCCents != 153000 || tt.NetCents != 123000 {
		t.Fatalf("ttc/net = %+v", tt)
	}
}

func TestCreditNoteNegative(t *testing.T) {
	tt := ComputeTotals([]Line{{Kind: KindItem, QtyC: -100, UnitPriceCents: 12345, VatBP: 2000}}, 0, 0)
	if tt.HTCents != -12345 || tt.VatCents != -2469 || tt.TTCCents != -14814 {
		t.Fatalf("credit = %+v", tt)
	}
}
