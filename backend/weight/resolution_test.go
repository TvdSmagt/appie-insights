package weight

import (
	"math"
	"testing"
)

func TestEffectiveKgStoredWins(t *testing.T) {
	stored := 0.3
	got := EffectiveKg(&stored, "500 g")
	if got == nil || *got != 0.3 {
		t.Errorf("EffectiveKg(&0.3, %q) = %v, want &0.3", "500 g", got)
	}
}

func TestEffectiveKgFallsBackToUnitSize(t *testing.T) {
	cases := map[string]float64{
		"500 g":         0.5,
		"2 kg":          2.0,
		"2 x 250 ml":    0.5,
		"ca. 200 g":     0.2,
		"los per 500 g": 0.5,
	}
	for input, want := range cases {
		got := EffectiveKg(nil, input)
		if got == nil {
			t.Errorf("EffectiveKg(nil, %q) = nil, want %v", input, want)
			continue
		}
		if math.Abs(*got-want) > 1e-3 {
			t.Errorf("EffectiveKg(nil, %q) = %v, want %v", input, *got, want)
		}
	}
}

func TestEffectiveKgReturnsNil(t *testing.T) {
	// No 1 kg/unit fallback: a weightless product yields nil so it surfaces in the
	// missing-weight list rather than being counted at a guess.
	for _, input := range []string{"", "per stuk", "6 stuks", "per bosje", "onbekend", "stuks"} {
		if got := EffectiveKg(nil, input); got != nil {
			t.Errorf("EffectiveKg(nil, %q) = %v, want nil", input, *got)
		}
	}
}

func TestServingSizeKg(t *testing.T) {
	cases := []struct {
		unitSize, servingSize string
		want                  float64
	}{
		{"6 stuks", "50 g", 0.3}, // explicit multi-count pack
		{"4 stuks", "125 g", 0.5},
	}
	for _, tc := range cases {
		got, ok := ServingSizeKg(tc.unitSize, tc.servingSize)
		if !ok {
			t.Errorf("ServingSizeKg(%q, %q): expected ok=true", tc.unitSize, tc.servingSize)
			continue
		}
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("ServingSizeKg(%q, %q) = %v, want %v", tc.unitSize, tc.servingSize, got, tc.want)
		}
	}
}

func TestServingSizeKgNotOk(t *testing.T) {
	cases := []struct{ unitSize, servingSize string }{
		{"per stuk", "50 g"},    // ambiguous single unit — rejected
		{"per bosje", "50 g"},   // ambiguous single unit — rejected
		{"per pakket", "50 g"},  // ambiguous single unit — rejected
		{"500 g", "50 g"},       // measured unit, not a piece count
		{"6 stuks", ""},         // no serving size
		{"6 stuks", "per stuk"}, // unparseable serving size
	}
	for _, tc := range cases {
		if _, ok := ServingSizeKg(tc.unitSize, tc.servingSize); ok {
			t.Errorf("ServingSizeKg(%q, %q): expected ok=false", tc.unitSize, tc.servingSize)
		}
	}
}
