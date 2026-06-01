package weight

import (
	"math"
	"testing"
)

func TestParseKg(t *testing.T) {
	cases := []struct {
		input    string
		expected float64
	}{
		{"500g", 0.5},
		{"500 g", 0.5},
		{"ca. 500g", 0.5},
		{"ca. 200 g", 0.2},
		{"2 kg", 2.0},
		{"1.5 liter", 1.5},
		{"1,5 l", 1.5},
		{"0,5 l", 0.5},
		{"330 ml", 0.33},
		{"2 x 250 ml", 0.5},
		{"4 x 125 g", 0.5},
		{"50 cl", 0.5},
		{"los per 500 g", 0.5}, // by-weight: quantity after "per"
		{"per 250 g", 0.25},    // by-weight, "per"-prefixed
		{"los per 1 kg", 1.0},
	}
	for _, tc := range cases {
		got, ok := ParseKg(tc.input)
		if !ok {
			t.Errorf("ParseKg(%q): expected ok=true", tc.input)
			continue
		}
		if math.Abs(got-tc.expected) > 1e-9 {
			t.Errorf("ParseKg(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestParseKgNotOk(t *testing.T) {
	for _, input := range []string{"", "per stuk", "6 stuks", "per bosje", "onbekend"} {
		if _, ok := ParseKg(input); ok {
			t.Errorf("ParseKg(%q): expected ok=false", input)
		}
	}
}

func TestParsePieceCount(t *testing.T) {
	ok := map[string]int{"per stuk": 1, "per bosje": 1, "per pakket": 1, "6 stuks": 6, "1 stuk": 1}
	for in, want := range ok {
		if n, got := ParsePieceCount(in); !got || n != want {
			t.Errorf("ParsePieceCount(%q) = (%d, %v), want (%d, true)", in, n, got, want)
		}
	}
	for _, in := range []string{"", "500 g", "los per 500 g"} {
		if _, got := ParsePieceCount(in); got {
			t.Errorf("ParsePieceCount(%q): expected ok=false", in)
		}
	}
}
