package enricher

import (
	"testing"
)

const testDataDir = "../data"

func TestLoadCO2EqCategories(t *testing.T) {
	entries, err := LoadCO2EqCategories(testDataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("co2eq_categories.csv is empty")
	}
	for _, e := range entries {
		if e.Name == "" {
			t.Error("entry with empty name")
		}
		if e.Category == "" {
			t.Error("entry with empty category")
		}
		if e.CO2PerKg <= 0 {
			t.Errorf("entry %q has non-positive co2eq_per_kg: %v", e.Name, e.CO2PerKg)
		}
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name] = true
	}
	for _, want := range []string{"Kipfilet", "Zalm", "Halfvolle melk"} {
		if !names[want] {
			t.Errorf("expected entry %q not found", want)
		}
	}
}

func TestLoadDefaultWeights(t *testing.T) {
	weights, err := loadDefaultWeights(testDataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(weights) == 0 {
		t.Fatal("default_weights.csv is empty")
	}
}

func TestSaveAndLoadCorrections(t *testing.T) {
	dir := t.TempDir()

	original := []Correction{
		{WebID: 42, Action: "set_category", CO2Category: "Vlees", CO2Name: "Kipfilet", CO2PerKg: f64Ptr(4.0)},
		{WebID: 7, Action: "ignore"},
	}
	if err := SaveCorrections(dir, original); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadCorrections(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != len(original) {
		t.Fatalf("expected %d corrections, got %d", len(original), len(loaded))
	}
	// SaveCorrections sorts by web_id ascending, so WebID 7 comes first.
	if loaded[0].WebID != 7 || loaded[1].WebID != 42 {
		t.Errorf("unexpected order: %v %v", loaded[0].WebID, loaded[1].WebID)
	}
	if loaded[1].CO2Category != "Vlees" {
		t.Errorf("CO2Category = %q, want Vlees", loaded[1].CO2Category)
	}
	if loaded[1].CO2PerKg == nil || !approxEq(*loaded[1].CO2PerKg, 4.0, 1e-6) {
		t.Errorf("unexpected CO2PerKg: %v", loaded[1].CO2PerKg)
	}
}

func TestLoadCorrectionsReturnsNilWhenMissing(t *testing.T) {
	corrections, err := LoadCorrections(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if corrections != nil {
		t.Errorf("expected nil for missing file, got %v", corrections)
	}
}
