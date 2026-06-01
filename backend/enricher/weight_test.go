package enricher

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// Quantity-string parsing lives in the weight package and is tested there
// (weight.ParseKg). The tests below cover enricher-specific weight resolution.

// ---------------------------------------------------------------------------
// applyDefaultWeight
// ---------------------------------------------------------------------------

func TestApplyDefaultWeight(t *testing.T) {
	weights := mustLoadDefaultWeights(t)

	cases := []struct {
		title    string
		unitSize string
		expected float64
	}{
		{"AH Zwarte thee earl grey", "20 stuks", 0.040},
		{"AH Ananas eetrijp", "per stuk", 1.2},
		{"AH Biologisch Prei", "per stuk", 0.25},
		{"AH Jalapeño peper groen", "per stuk", 0.030},
	}
	for _, tc := range cases {
		got, ok := applyDefaultWeight(tc.title, tc.unitSize, weights)
		if !ok {
			t.Errorf("applyDefaultWeight(%q, %q): expected ok=true", tc.title, tc.unitSize)
			continue
		}
		if !approxEq(got, tc.expected, 1e-3) {
			t.Errorf("applyDefaultWeight(%q, %q) = %v, want %v", tc.title, tc.unitSize, got, tc.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// fillDefaultWeights
// ---------------------------------------------------------------------------

func TestFillDefaultWeightsUpdatesNull(t *testing.T) {
	db := weightTestDB(t)
	insertProduct(t, db, 1, "AH Zwarte thee earl grey", "20 stuks")
	insertEnrichment(t, db, 1, nil, "keyword")

	n, err := fillDefaultWeights(db, mustLoadDefaultWeights(t))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 updated, got %d", n)
	}
	var w sql.NullFloat64
	db.QueryRow("SELECT weight_kg FROM product_enrichment WHERE web_id = 1").Scan(&w)
	if !approxEq(w.Float64, 0.040, 1e-3) {
		t.Errorf("weight_kg = %v, want ~0.040", w.Float64)
	}
}

func TestFillDefaultWeightsSkipsAlreadyWeighted(t *testing.T) {
	db := weightTestDB(t)
	insertProduct(t, db, 1, "AH Zwarte thee earl grey", "20 stuks")
	w := 0.1
	insertEnrichment(t, db, 1, &w, "keyword")

	n, err := fillDefaultWeights(db, mustLoadDefaultWeights(t))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 updated, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// fillUnitSizeWeights
// ---------------------------------------------------------------------------

func TestFillUnitSizeWeightsVolume(t *testing.T) {
	db := weightTestDB(t)
	insertProduct(t, db, 1, "AA Drink High energy", "0,5 l")
	insertEnrichment(t, db, 1, nil, "subcategory_direct")

	n, err := fillUnitSizeWeights(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 updated, got %d", n)
	}
	var w sql.NullFloat64
	db.QueryRow("SELECT weight_kg FROM product_enrichment WHERE web_id = 1").Scan(&w)
	if !approxEq(w.Float64, 0.5, 1e-3) {
		t.Errorf("weight_kg = %v, want 0.5", w.Float64)
	}
}

func TestFillUnitSizeWeightsGram(t *testing.T) {
	db := weightTestDB(t)
	insertProduct(t, db, 1, "AH Broccoli", "200 g")
	insertEnrichment(t, db, 1, nil, "subcategory_direct")

	n, err := fillUnitSizeWeights(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 updated, got %d", n)
	}
	var w sql.NullFloat64
	db.QueryRow("SELECT weight_kg FROM product_enrichment WHERE web_id = 1").Scan(&w)
	if !approxEq(w.Float64, 0.2, 1e-3) {
		t.Errorf("weight_kg = %v, want 0.2", w.Float64)
	}
}

func TestFillUnitSizeWeightsSkipsPieceBased(t *testing.T) {
	db := weightTestDB(t)
	insertProduct(t, db, 1, "AH Kaiserbroodje", "per stuk")
	insertProduct(t, db, 2, "AH Eieren", "6 stuks")
	insertEnrichment(t, db, 1, nil, "subcategory_direct")
	insertEnrichment(t, db, 2, nil, "subcategory_direct")

	n, _ := fillUnitSizeWeights(db)
	if n != 0 {
		t.Errorf("expected 0 updated for piece-based products, got %d", n)
	}
}

func TestFillUnitSizeWeightsSkipsAlreadyWeighted(t *testing.T) {
	db := weightTestDB(t)
	insertProduct(t, db, 1, "AH Melk", "1 l")
	w := 1.0
	insertEnrichment(t, db, 1, &w, "subcategory_direct")

	n, _ := fillUnitSizeWeights(db)
	if n != 0 {
		t.Errorf("expected 0 updated for already-weighted product, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// fillMultipackWeights
// ---------------------------------------------------------------------------

func TestFillMultipackWeightsBasic(t *testing.T) {
	db := weightTestDB(t)
	insertProduct(t, db, 10, "AH Stamppot zuurkool rookworst", "500 g")
	insertEnrichment(t, db, 10, nil, "keyword")
	insertProduct(t, db, 11, "AH Stamppot zuurkool rookworst 2-pack", "2 stuks")
	insertEnrichment(t, db, 11, nil, "keyword")

	n, err := fillMultipackWeights(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 updated, got %d", n)
	}
	var w sql.NullFloat64
	db.QueryRow("SELECT weight_kg FROM product_enrichment WHERE web_id = 11").Scan(&w)
	if !approxEq(w.Float64, 1.0, 1e-3) {
		t.Errorf("weight_kg = %v, want 1.0", w.Float64)
	}
}

func TestFillMultipackWeightsNoSingleProduct(t *testing.T) {
	db := weightTestDB(t)
	insertProduct(t, db, 11, "AH Mystery product 3-pack", "3 stuks")
	insertEnrichment(t, db, 11, nil, "keyword")

	n, _ := fillMultipackWeights(db)
	if n != 0 {
		t.Errorf("expected 0 updated, got %d", n)
	}
	var w sql.NullFloat64
	db.QueryRow("SELECT weight_kg FROM product_enrichment WHERE web_id = 11").Scan(&w)
	if w.Valid {
		t.Errorf("weight_kg should still be NULL")
	}
}

func TestFillMultipackWeightsSingleIsCountBased(t *testing.T) {
	db := weightTestDB(t)
	insertProduct(t, db, 10, "AH Scharreleieren M", "6 stuks")
	insertEnrichment(t, db, 10, nil, "keyword")
	insertProduct(t, db, 11, "AH Scharreleieren M 2-pack", "2 stuks")
	insertEnrichment(t, db, 11, nil, "keyword")

	n, _ := fillMultipackWeights(db)
	if n != 0 {
		t.Errorf("expected 0 updated (single is count-based), got %d", n)
	}
}

// ---------------------------------------------------------------------------
// fillServingSizeWeights
// ---------------------------------------------------------------------------

func TestFillServingSizeWeightsSkipsPerStuk(t *testing.T) {
	// A single "per stuk" unit may hold many servings (a loaf is not one slice),
	// so serving size must not be used as the product weight. Such products are
	// left weightless and surface in the missing-weight list instead.
	db := weightTestDB(t)
	db.Exec("INSERT INTO products VALUES (1, 'AH Volkorenbrood', 'per stuk', '35 gram')")
	insertEnrichment(t, db, 1, nil, "keyword")

	n, err := fillServingSizeWeights(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 updated (per stuk skipped), got %d", n)
	}
	var w sql.NullFloat64
	db.QueryRow("SELECT weight_kg FROM product_enrichment WHERE web_id = 1").Scan(&w)
	if w.Valid {
		t.Errorf("weight_kg = %v, want NULL (not derived from serving size)", w.Float64)
	}
}

func TestFillServingSizeWeightsMultiPiece(t *testing.T) {
	db := weightTestDB(t)
	db.Exec("INSERT INTO products VALUES (1, 'AH Broodjes 6 stuks', '6 stuks', '70 gram')")
	insertEnrichment(t, db, 1, nil, "keyword")

	n, err := fillServingSizeWeights(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 updated, got %d", n)
	}
	var w sql.NullFloat64
	db.QueryRow("SELECT weight_kg FROM product_enrichment WHERE web_id = 1").Scan(&w)
	if !approxEq(w.Float64, 0.420, 1e-3) {
		t.Errorf("weight_kg = %v, want ~0.420", w.Float64)
	}
}

func TestFillServingSizeWeightsSkipsAlreadyWeighted(t *testing.T) {
	db := weightTestDB(t)
	db.Exec("INSERT INTO products VALUES (1, 'AH Kaiserbroodje', 'per stuk', '70 gram')")
	existing := 0.1
	insertEnrichment(t, db, 1, &existing, "keyword")

	n, _ := fillServingSizeWeights(db)
	if n != 0 {
		t.Errorf("expected 0 updated, got %d", n)
	}
}

func TestFillServingSizeWeightsSkipsUnparseable(t *testing.T) {
	db := weightTestDB(t)
	db.Exec("INSERT INTO products VALUES (1, 'AH Broodje', 'per stuk', 'onbekend')")
	insertEnrichment(t, db, 1, nil, "keyword")

	n, _ := fillServingSizeWeights(db)
	if n != 0 {
		t.Errorf("expected 0 updated for unparseable serving_size, got %d", n)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func weightTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	db.Exec(`CREATE TABLE products (web_id INTEGER PRIMARY KEY, title TEXT, unit_size TEXT, serving_size TEXT)`)
	db.Exec(`CREATE TABLE product_enrichment (
		web_id INTEGER PRIMARY KEY, co2eq_category TEXT, co2eq_name TEXT,
		co2eq_per_kg REAL, match_method TEXT, weight_kg REAL, weight_source TEXT)`)
	return db
}

func insertProduct(t *testing.T, db *sql.DB, webID int, title, unitSize string) {
	t.Helper()
	if _, err := db.Exec("INSERT INTO products (web_id, title, unit_size) VALUES (?, ?, ?)", webID, title, unitSize); err != nil {
		t.Fatal(err)
	}
}

func insertEnrichment(t *testing.T, db *sql.DB, webID int, weightKg *float64, method string) {
	t.Helper()
	if _, err := db.Exec(
		"INSERT INTO product_enrichment (web_id, co2eq_per_kg, match_method, weight_kg) VALUES (?, 1.0, ?, ?)",
		webID, method, weightKg,
	); err != nil {
		t.Fatal(err)
	}
}

func mustLoadDefaultWeights(t *testing.T) []defaultWeight {
	t.Helper()
	weights, err := loadDefaultWeights(testDataDir)
	if err != nil {
		t.Fatalf("loadDefaultWeights: %v", err)
	}
	return weights
}

func approxEq(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol*b+1e-9
}
