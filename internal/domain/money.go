// Package domain holds the pure business rules: money, quantities, VAT and document totals.
// Amounts are integer cents, quantities integer hundredths, VAT rates basis points (10 % = 1000).
package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// RoundDiv returns a/b rounded half away from zero (the usual French commercial rounding).
func RoundDiv(a, b int64) int64 {
	if b < 0 {
		a, b = -a, -b
	}
	if a >= 0 {
		return (a + b/2) / b
	}
	return -((-a + b/2) / b)
}

// ParseDecimal parses "1 234,56", "1234.56" or "-3" into an integer scaled by 10^decimals,
// rounding any extra digits half away from zero.
func ParseDecimal(s string, decimals int) (int64, error) {
	s = strings.NewReplacer(" ", "", "\u00a0", "", "\u202f", "", "€", "").Replace(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" {
		return 0, nil
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "-"), "+")
	intPart, frac, _ := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	for _, r := range intPart + frac {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("nombre invalide : %q", s)
		}
	}
	extra := ""
	if len(frac) > decimals {
		extra, frac = frac[decimals:], frac[:decimals]
	}
	frac += strings.Repeat("0", decimals-len(frac))
	v, err := strconv.ParseInt(intPart+frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("nombre invalide : %q", s)
	}
	if extra != "" && extra[0] >= '5' {
		v++
	}
	if neg {
		v = -v
	}
	return v, nil
}

// FormatDecimal formats a scaled integer with a comma and thin-space thousands ("1 234,56").
func FormatDecimal(v int64, decimals int) string {
	neg := v < 0
	if neg {
		v = -v
	}
	pow := int64(1)
	for i := 0; i < decimals; i++ {
		pow *= 10
	}
	ip := strconv.FormatInt(v/pow, 10)
	var b strings.Builder
	for i, r := range ip {
		if i > 0 && (len(ip)-i)%3 == 0 {
			b.WriteRune('\u202f')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if decimals > 0 {
		out += "," + fmt.Sprintf("%0*d", decimals, v%pow)
	}
	if neg {
		out = "-" + out
	}
	return out
}

// Euros formats cents as "1 234,56 €".
func Euros(c int64) string { return FormatDecimal(c, 2) + " €" }

// Rate formats basis points as a percentage ("5,5 %", "20 %").
func Rate(bp int64) string {
	s := FormatDecimal(bp, 2)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ",")
	return s + " %"
}
