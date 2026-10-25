package domain

import "sort"

// Line kinds.
const (
	KindItem    = "item"
	KindSection = "section"
	KindText    = "text"
)

// Line is one row of a quote or invoice.
type Line struct {
	ID             int64  `json:"id,omitempty"`
	Position       int    `json:"position"`
	Kind           string `json:"kind"`
	Label          string `json:"label"`
	Description    string `json:"description"`
	Unit           string `json:"unit"`
	QtyC           int64  `json:"qty_c"`            // quantity in hundredths
	UnitPriceCents int64  `json:"unit_price_cents"` // unit price excluding VAT
	VatBP          int64  `json:"vat_bp"`           // VAT rate in basis points
}

// TotalCents is the line amount excluding VAT, rounded to the cent.
func (l Line) TotalCents() int64 {
	if l.Kind != KindItem {
		return 0
	}
	return RoundDiv(l.QtyC*l.UnitPriceCents, 100)
}

// VatPart is the VAT due for one rate.
type VatPart struct {
	RateBP    int64 `json:"rate_bp"`
	BaseCents int64 `json:"base_cents"`
	VatCents  int64 `json:"vat_cents"`
}

// Totals are the computed amounts of a document.
type Totals struct {
	GrossCents    int64     `json:"gross_cents"`    // sum of lines before discount
	DiscountCents int64     `json:"discount_cents"` // discount amount
	HTCents       int64     `json:"ht_cents"`
	VAT           []VatPart `json:"vat"`
	VatCents      int64     `json:"vat_cents"`
	TTCCents      int64     `json:"ttc_cents"`
	AdvanceCents  int64     `json:"advance_cents"`
	NetCents      int64     `json:"net_cents"` // TTC minus advance
}

// ComputeTotals applies the discount per VAT rate, then computes VAT once per rate.
func ComputeTotals(lines []Line, discountBP, advanceCents int64) Totals {
	gross := map[int64]int64{}
	var t Totals
	for _, l := range lines {
		if l.Kind != KindItem {
			continue
		}
		v := l.TotalCents()
		gross[l.VatBP] += v
		t.GrossCents += v
	}
	rates := make([]int64, 0, len(gross))
	for r := range gross {
		rates = append(rates, r)
	}
	sort.Slice(rates, func(i, j int) bool { return rates[i] < rates[j] })
	for _, r := range rates {
		g := gross[r]
		base := g - RoundDiv(g*discountBP, 10000)
		vat := RoundDiv(base*r, 10000)
		t.VAT = append(t.VAT, VatPart{RateBP: r, BaseCents: base, VatCents: vat})
		t.HTCents += base
		t.VatCents += vat
	}
	t.DiscountCents = t.GrossCents - t.HTCents
	t.TTCCents = t.HTCents + t.VatCents
	t.AdvanceCents = advanceCents
	t.NetCents = t.TTCCents - advanceCents
	return t
}
