package enricher

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// buildAHSubcategoryLookup
// ---------------------------------------------------------------------------

func TestBuildAHSubcategoryLookup(t *testing.T) {
	lookup := buildAHSubcategoryLookup([]ahSubcategoryEntry{
		{ahSubcategory: "Yoghurt", co2Category: "Zuivel", co2Subcategory: "Yoghurt"},
		{ahSubcategory: "Eieren", co2Category: "Zuivel", co2Subcategory: "Ei"},
	})
	if e, ok := lookup["yoghurt"]; !ok || e.co2Subcategory != "Yoghurt" {
		t.Errorf("expected Yoghurt subcategory, got %+v", e)
	}
	if e, ok := lookup["eieren"]; !ok || e.co2Subcategory != "Ei" {
		t.Errorf("expected Ei subcategory, got %+v", e)
	}
	if _, ok := lookup["Yoghurt"]; ok {
		t.Error("lookup should use lowercase keys")
	}
}

// ---------------------------------------------------------------------------
// matchCO2Category — early exits
// ---------------------------------------------------------------------------

func TestMatchMissingProduct(t *testing.T) {
	db := matchTestDB(t)
	result := matchCO2Category(999, db, nil)
	if result.method != "no_product" || result.name != nil {
		t.Errorf("expected no_product, got method=%q matched=%v", result.method, result.name != nil)
	}
}

func TestMatchNoMetadata(t *testing.T) {
	db := matchTestDB(t)
	insertMatchProduct(t, db, 1, "", "", "")
	result := matchCO2Category(1, db, nil)
	if result.method != "no_metadata" {
		t.Errorf("expected no_metadata, got %q", result.method)
	}
}

// ---------------------------------------------------------------------------
// matchCO2Category — subcategory direct path
// ---------------------------------------------------------------------------

func TestMatchSubcategoryDirect(t *testing.T) {
	db := matchTestDB(t)
	insertMatchProduct(t, db, 1, "AH Griekse Yoghurt", "Zuivel, eieren", "Griekse yoghurt")

	co2 := 3.5
	subcatLookup := buildAHSubcategoryLookup([]ahSubcategoryEntry{
		{ahSubcategory: "Griekse yoghurt", co2Category: "Zuivel", co2Subcategory: "Yoghurt", co2PerKg: &co2},
	})

	result := matchCO2Category(1, db, subcatLookup)
	if result.method != "subcategory_direct" {
		t.Fatalf("expected subcategory_direct, got %q", result.method)
	}
	if result.co2PerKg == nil || !approxEq(*result.co2PerKg, 3.5, 1e-3) {
		t.Errorf("expected 3.5, got %v", result.co2PerKg)
	}
	if result.category == nil || *result.category != "Zuivel" {
		t.Errorf("expected Zuivel category, got %v", result.category)
	}
}

func TestMatchSubcategoryOverridesCategory(t *testing.T) {
	db := matchTestDB(t)
	insertMatchProduct(t, db, 1, "AH Haverdrink", "Vegetarisch, vegan en plantaardig", "Haverdrink")

	co2 := 0.6
	subcatLookup := buildAHSubcategoryLookup([]ahSubcategoryEntry{
		{ahSubcategory: "Haverdrink", co2Category: "Dranken", co2Subcategory: "Zuiveldrank", co2PerKg: &co2},
	})

	result := matchCO2Category(1, db, subcatLookup)
	if result.method != "subcategory_direct" {
		t.Fatalf("expected subcategory_direct, got %q", result.method)
	}
	if result.category == nil || *result.category != "Dranken" {
		t.Errorf("expected Dranken category, got %v", result.category)
	}
	if result.co2PerKg == nil || !approxEq(*result.co2PerKg, 0.6, 1e-3) {
		t.Errorf("expected 0.6, got %v", result.co2PerKg)
	}
}

func TestMatchSubcategoryUnmapped(t *testing.T) {
	db := matchTestDB(t)
	insertMatchProduct(t, db, 1, "AH Stoommaaltijd", "Diepvries", "Stoommaaltijden")

	subcatLookup := buildAHSubcategoryLookup([]ahSubcategoryEntry{
		{ahSubcategory: "Stoommaaltijden", co2Category: "", co2Subcategory: "", co2PerKg: nil},
	})

	result := matchCO2Category(1, db, subcatLookup)
	if result.method != "non_food" || result.name != nil {
		t.Errorf("expected non_food unmatched, got method=%q matched=%v", result.method, result.name != nil)
	}
}

// ---------------------------------------------------------------------------
// matchCO2Category — vegan icon override
// ---------------------------------------------------------------------------

func TestMatchVeganIconOverride(t *testing.T) {
	db := matchTestDB(t)
	insertMatchProductWithIcons(t, db, 1, "Flower Farm Bakboter", "Zuivel, eieren", "Bak- en braadboter", `["vegan"]`)

	co2Dairy := 14.0
	co2Vegan := 1.5
	subcatLookup := buildAHSubcategoryLookup([]ahSubcategoryEntry{
		{ahSubcategory: "Bak- en braadboter", co2Category: "Zuivel", co2Subcategory: "Boter", co2PerKg: &co2Dairy},
		{ahSubcategory: "Bak- en braadboter (vegan)", co2Category: "Plantaardig", co2Subcategory: "Plantaardige alternatieven", co2PerKg: &co2Vegan},
	})

	result := matchCO2Category(1, db, subcatLookup)
	if result.method != "subcategory_vegan" {
		t.Fatalf("expected subcategory_vegan, got %q", result.method)
	}
	if result.category == nil || *result.category != "Plantaardig" {
		t.Errorf("expected Plantaardig, got %v", result.category)
	}
	if result.co2PerKg == nil || !approxEq(*result.co2PerKg, 1.5, 1e-3) {
		t.Errorf("expected 1.5, got %v", result.co2PerKg)
	}
}

func TestMatchVeganIconFallsBackWithoutEntry(t *testing.T) {
	db := matchTestDB(t)
	insertMatchProductWithIcons(t, db, 1, "Flower Farm Bakboter", "Zuivel, eieren", "Bak- en braadboter", `["vegan"]`)

	co2Dairy := 14.0
	subcatLookup := buildAHSubcategoryLookup([]ahSubcategoryEntry{
		{ahSubcategory: "Bak- en braadboter", co2Category: "Zuivel", co2Subcategory: "Boter", co2PerKg: &co2Dairy},
	})

	result := matchCO2Category(1, db, subcatLookup)
	if result.method != "subcategory_direct" {
		t.Fatalf("expected subcategory_direct fallback, got %q", result.method)
	}
}

// ---------------------------------------------------------------------------
// matchCO2Category — non-food AH category fallback
// ---------------------------------------------------------------------------

func TestMatchNonFoodAHCategory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ahCat    string
		ahSubcat string
	}{
		{"drogisterij unmatched subcat", "Drogisterij", "Tandpasta gevoelig tandvlees"},
		{"huishouden unmatched subcat", "Huishouden", "Schoonmaakdoekjes"},
		{"koken vrije tijd unmatched subcat", "Koken, tafelen, vrije tijd", "Aanmaakblokjes"},
		{"drogisterij no subcat", "Drogisterij", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := matchTestDB(t)
			insertMatchProduct(t, db, 1, "Some Product", tc.ahCat, tc.ahSubcat)
			result := matchCO2Category(1, db, buildAHSubcategoryLookup(nil))
			if result.method != "non_food" || result.name != nil {
				t.Errorf("expected non_food unmatched, got method=%q matched=%v", result.method, result.name != nil)
			}
		})
	}
}

func TestMatchFoodCategoryStaysUnmatched(t *testing.T) {
	db := matchTestDB(t)
	insertMatchProduct(t, db, 1, "Some Bread", "Bakkerij", "Volkoren brood")
	result := matchCO2Category(1, db, buildAHSubcategoryLookup(nil))
	if result.method != "unmatched" {
		t.Errorf("expected unmatched for food category without map entry, got %q", result.method)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func matchTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	db.Exec(`CREATE TABLE products (
		web_id INTEGER PRIMARY KEY, title TEXT, ah_category TEXT, ah_subcategory TEXT, property_icons TEXT)`)
	return db
}

func insertMatchProduct(t *testing.T, db *sql.DB, webID int, title, ahCategory, ahSubcategory string) {
	t.Helper()
	insertMatchProductWithIcons(t, db, webID, title, ahCategory, ahSubcategory, "")
}

func insertMatchProductWithIcons(t *testing.T, db *sql.DB, webID int, title, ahCategory, ahSubcategory, propertyIcons string) {
	t.Helper()
	if _, err := db.Exec(
		"INSERT INTO products VALUES (?, ?, ?, ?, ?)", webID, title, ahCategory, ahSubcategory, propertyIcons,
	); err != nil {
		t.Fatal(err)
	}
}
