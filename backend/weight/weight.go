// Package weight parses AH quantity strings (unit_size, serving_size,
// net_content) into kilograms and defines the weight-resolution policy. It is
// the single source of truth for both: the parsers (ParseKg, ParsePieceCount)
// and the policy (resolution.go: the SourcePriority ordering, the per-source
// ServingSizeKg rule, and the query-time EffectiveKg fallback). The enricher
// drives enrichment-time resolution from this policy; the analytics layer uses
// it for the query-time fallback and the product-page breakdown.
package weight

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	approxRE   = regexp.MustCompile(`^ca\.?\s*`)
	compoundRE = regexp.MustCompile(`^(\d+(?:[.,]\d+)?)\s*[x×]\s*(\d+(?:[.,]\d+)?)\s*(g|gram|kg|kilo|l|liter|litre|cl|ml|cc)\b`)
	simpleRE   = regexp.MustCompile(`^(\d+(?:[.,]\d+)?)\s*(g|gram|kg|kilo|l|liter|litre|cl|ml|cc)\b`)
	stuksRE    = regexp.MustCompile(`^(\d+)\s+stuks?$`)
)

// ParsePieceCount interprets a count-based unit_size such as "per stuk",
// "6 stuks", or "per bosje" as a number of pieces. It returns ok=false for
// measured units ("500 g") or empty strings.
func ParsePieceCount(unitSize string) (int, bool) {
	s := strings.TrimSpace(strings.ToLower(unitSize))
	if s == "" {
		return 0, false
	}
	switch s {
	case "per stuk", "per bosje", "per pakket":
		return 1, true
	}
	if m := stuksRE.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

// ParseKg parses a quantity string like "500 g", "0,5 l", "ca. 200 g",
// "2 x 250 ml" or "los per 500 g" into kilograms (liquids are treated as
// 1 l = 1 kg). It returns ok=false when the string is empty or carries no
// parseable quantity (e.g. "per stuk", "6 stuks").
func ParseKg(unitSize string) (float64, bool) {
	s := strings.ToLower(strings.TrimSpace(unitSize))
	if s == "" {
		return 0, false
	}
	s = approxRE.ReplaceAllString(s, "")

	if kg, ok := parseQuantity(s); ok {
		return kg, true
	}
	// By-weight products use a "per" descriptor, e.g. "los per 500 g" or
	// "per 500 g" — the quantity follows "per ". ("per stuk"/"per bosje" have
	// no quantity after "per" and correctly fall through to ok=false.)
	if i := strings.Index(s, "per "); i != -1 {
		if kg, ok := parseQuantity(s[i+len("per "):]); ok {
			return kg, true
		}
	}
	return 0, false
}

// parseQuantity matches a leading quantity ("500 g", "2 x 250 ml") at the start
// of s and converts it to kilograms.
func parseQuantity(s string) (float64, bool) {
	var total float64
	var unit string
	if m := compoundRE.FindStringSubmatch(s); m != nil {
		total = parseCommaFloat(m[1]) * parseCommaFloat(m[2])
		unit = m[3]
	} else if m := simpleRE.FindStringSubmatch(s); m != nil {
		total = parseCommaFloat(m[1])
		unit = m[2]
	} else {
		return 0, false
	}

	switch unit {
	case "g", "gram":
		return total / 1000, true
	case "kg", "kilo":
		return total, true
	case "l", "liter", "litre":
		return total, true
	case "cl":
		return total / 100, true
	case "ml", "cc":
		return total / 1000, true
	}
	return 0, false
}

func parseCommaFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	return f
}
