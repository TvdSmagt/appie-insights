package analytics

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"appie-insights/backend/config"
	"appie-insights/backend/store"
)

// newTestDBFile creates a file-based SQLite DB with the full schema in a temp directory.
// The seeding connection is returned for the caller to populate and close before use.
func newTestDBFile(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE receipts (
			transaction_id TEXT PRIMARY KEY,
			date           TEXT NOT NULL,
			total_amount   REAL NOT NULL
		);
		CREATE TABLE items (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			receipt_id  TEXT NOT NULL REFERENCES receipts(transaction_id),
			description TEXT NOT NULL,
			quantity    INTEGER NOT NULL,
			amount      REAL NOT NULL,
			web_id      INTEGER,
			product_id  INTEGER
		);
		CREATE TABLE products (
			web_id                 INTEGER PRIMARY KEY,
			pos_id                 INTEGER,
			title                  TEXT,
			brand                  TEXT,
			ah_category            TEXT,
			ah_subcategory         TEXT,
			unit_size              TEXT,
			nutriscore             TEXT,
			unit_price_description TEXT,
			property_icons         TEXT,
			thumbnail_url          TEXT,
			net_content            TEXT,
			serving_size           TEXT
		);
		CREATE TABLE product_enrichment (
			web_id         INTEGER PRIMARY KEY,
			co2eq_category TEXT,
			co2eq_name     TEXT,
			co2eq_per_kg   REAL,
			match_method   TEXT,
			weight_kg      REAL,
			weight_source  TEXT
		);
		CREATE TABLE product_not_found (
			web_id INTEGER PRIMARY KEY
		);
		CREATE TABLE orders (
			order_id         INTEGER PRIMARY KEY,
			delivery_date    TEXT NOT NULL,
			delivery_method  TEXT,
			delivery_status  TEXT,
			total_price      REAL,
			invoice_id       TEXT,
			address_street   TEXT,
			address_number   TEXT,
			address_extra    TEXT,
			address_postcode TEXT,
			address_city     TEXT
		);
		CREATE TABLE order_items (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id        INTEGER NOT NULL REFERENCES orders(order_id),
			web_id          INTEGER NOT NULL,
			title           TEXT NOT NULL,
			brand           TEXT,
			category        TEXT,
			sales_unit_size TEXT,
			quantity        INTEGER NOT NULL,
			allocated_qty   INTEGER NOT NULL,
			unit_price      REAL,
			was_price       REAL,
			image_url       TEXT,
			UNIQUE(order_id, web_id)
		);
		CREATE TABLE receipt_discounts (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			receipt_id TEXT NOT NULL REFERENCES receipts(transaction_id),
			name       TEXT,
			amount     REAL NOT NULL
		);`); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func seedFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	exec(`INSERT INTO receipts VALUES ('TRX-001','2026-05-01T10:00:00Z',25.50)`)
	exec(`INSERT INTO items (receipt_id,description,quantity,amount,web_id,product_id) VALUES ('TRX-001','Melk',2,2.18,456,789)`)
	// "Brood" has product_id but no web_id → appears in products/issues no_web_id
	exec(`INSERT INTO items (receipt_id,description,quantity,amount,web_id,product_id) VALUES ('TRX-001','Brood',1,1.50,NULL,111)`)
	exec(`INSERT INTO products (web_id,pos_id,title,brand,ah_category,ah_subcategory,unit_size,nutriscore,unit_price_description,property_icons,thumbnail_url) VALUES (456,789,'Melk Halfvol','AH','Zuivel','Melk','1L','B','€1.09/L','','https://img.example.com/melk.jpg')`)
	exec(`INSERT INTO product_enrichment VALUES (456,'dairy','Melk',3.2,'exact',1.0, NULL)`)
	exec(`INSERT INTO orders (order_id,delivery_date,delivery_method,delivery_status,total_price,invoice_id,address_street,address_number,address_postcode,address_city) VALUES (1001,'2026-05-10','home','delivered',50.00,'INV-1','Hoofdstraat','1','1234AB','Amsterdam')`)
	exec(`INSERT INTO order_items (order_id,web_id,title,brand,category,sales_unit_size,quantity,allocated_qty,unit_price) VALUES (1001,456,'Melk Halfvol','AH','Zuivel','1L',3,3,1.09)`)
	exec(`INSERT INTO receipt_discounts (receipt_id,name,amount) VALUES ('TRX-001','AH Bonus',2.50)`)
}

// newTestMux seeds a fresh DB, then returns a mux with all analytics handlers registered.
func newTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	db, path := newTestDBFile(t)
	seedFixtures(t, db)
	db.Close()
	mux := http.NewServeMux()
	RegisterHandlers(mux, config.Server{DBPath: path})
	return mux
}

func doRequest(mux *http.ServeMux, method, path string, body []byte) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status %d, want %d; body: %s", rec.Code, want, rec.Body.String())
	}
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v; body: %s", err, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GET /receipts
// ---------------------------------------------------------------------------

func TestHandlerGetReceipts(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/receipts", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	if len(body) != 1 {
		t.Fatalf("want 1 receipt, got %d", len(body))
	}
	if body[0]["transaction_id"] != "TRX-001" {
		t.Errorf("unexpected transaction_id: %v", body[0]["transaction_id"])
	}
	if body[0]["co2eq_total"] == nil {
		t.Error("expected non-nil co2eq_total")
	}
}

// TestReceiptItemCountExcludesNonFood verifies that item_count and matched_count
// in /receipts exclude items enriched as non_food or ignored.
func TestReceiptItemCountExcludesNonFood(t *testing.T) {
	db, path := newTestDBFile(t)

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	exec(`INSERT INTO receipts VALUES ('TRX-NF','2026-05-02T10:00:00Z',10.00)`)
	// food item with CO2 data
	exec(`INSERT INTO items (receipt_id,description,quantity,amount,web_id) VALUES ('TRX-NF','Melk',1,1.09,100)`)
	exec(`INSERT INTO products (web_id,title,unit_size) VALUES (100,'Melk Halfvol','1L')`)
	exec(`INSERT INTO product_enrichment (web_id,co2eq_category,co2eq_name,co2eq_per_kg,match_method,weight_kg) VALUES (100,'dairy','Melk',3.2,'subcategory_direct',1.0)`)
	// unidentifiable item (no products metadata)
	exec(`INSERT INTO items (receipt_id,description,quantity,amount,web_id) VALUES ('TRX-NF','Deodorant',1,2.50,101)`)
	exec(`INSERT INTO products (web_id,title,unit_size) VALUES (101,'Deodorant Roll-On','150ml')`)
	exec(`INSERT INTO product_enrichment (web_id,co2eq_category,co2eq_name,co2eq_per_kg,match_method,weight_kg) VALUES (101,NULL,NULL,NULL,'no_product',NULL)`)
	// ignored item (manual correction)
	exec(`INSERT INTO items (receipt_id,description,quantity,amount,web_id) VALUES ('TRX-NF','Statiegeld',1,0.25,102)`)
	exec(`INSERT INTO products (web_id,title,unit_size) VALUES (102,'Statiegeld fles','')`)
	exec(`INSERT INTO product_enrichment (web_id,co2eq_category,co2eq_name,co2eq_per_kg,match_method,weight_kg) VALUES (102,NULL,NULL,NULL,'ignored',NULL)`)
	// enriched non_food item
	exec(`INSERT INTO items (receipt_id,description,quantity,amount,web_id) VALUES ('TRX-NF','Shampoo',1,3.00,103)`)
	exec(`INSERT INTO products (web_id,title,unit_size) VALUES (103,'Shampoo','250ml')`)
	exec(`INSERT INTO product_enrichment (web_id,co2eq_category,co2eq_name,co2eq_per_kg,match_method,weight_kg) VALUES (103,NULL,NULL,NULL,'non_food',NULL)`)
	// receipt line with no web_id (e.g. bag fee, unrecognised barcode)
	exec(`INSERT INTO items (receipt_id,description,quantity,amount,web_id) VALUES ('TRX-NF','Tas',1,0.10,NULL)`)

	db.Close()
	mux := http.NewServeMux()
	RegisterHandlers(mux, config.Server{DBPath: path})

	rec := doRequest(mux, "GET", "/receipts", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)

	var r map[string]any
	for _, item := range body {
		if item["transaction_id"] == "TRX-NF" {
			r = item
			break
		}
	}
	if r == nil {
		t.Fatal("TRX-NF not found in response")
	}
	// Only the food item (Melk) counts; non_food, no_product, ignored, and NULL web_id items are excluded
	if got := r["item_count"]; got != float64(1) {
		t.Errorf("item_count = %v, want 1", got)
	}
	if got := r["matched_count"]; got != float64(1) {
		t.Errorf("matched_count = %v, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// GET /receipts/{id}
// ---------------------------------------------------------------------------

func TestHandlerGetReceiptDetail(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/receipts/TRX-001", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)
	if body["transaction_id"] != "TRX-001" {
		t.Errorf("unexpected transaction_id: %v", body["transaction_id"])
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
}

func TestHandlerGetReceiptDetailNotFound(t *testing.T) {
	assertStatus(t, doRequest(newTestMux(t), "GET", "/receipts/TRX-NOPE", nil), http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// GET /orders
// ---------------------------------------------------------------------------

func TestHandlerGetOrders(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/orders", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	if len(body) != 1 || body[0]["order_id"] != float64(1001) {
		t.Errorf("unexpected orders: %v", body)
	}
}

// ---------------------------------------------------------------------------
// GET /orders/{id}
// ---------------------------------------------------------------------------

func TestHandlerGetOrderDetail(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/orders/1001", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)
	if body["order_id"] != float64(1001) {
		t.Errorf("unexpected order_id: %v", body["order_id"])
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("want 1 order item, got %d", len(items))
	}
}

func TestHandlerGetOrderDetailNotFound(t *testing.T) {
	assertStatus(t, doRequest(newTestMux(t), "GET", "/orders/9999", nil), http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// GET /items
// ---------------------------------------------------------------------------

func TestHandlerGetItems(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/items", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	if len(body) == 0 {
		t.Fatal("expected items")
	}
	for _, it := range body {
		if it["source_type"] == nil {
			t.Error("item missing source_type")
		}
	}
}

// ---------------------------------------------------------------------------
// GET /products
// ---------------------------------------------------------------------------

func TestHandlerGetProducts(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/products", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	if len(body) != 1 {
		t.Fatalf("want 1 product, got %d", len(body))
	}
	if body[0]["web_id"] != float64(456) {
		t.Errorf("unexpected web_id: %v", body[0]["web_id"])
	}
}

// ---------------------------------------------------------------------------
// GET /products/stats
// ---------------------------------------------------------------------------

func TestHandlerGetProductStats(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/products/stats", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	if len(body) == 0 {
		t.Fatal("expected product stats")
	}
	if body[0]["times_bought"] == nil {
		t.Error("missing times_bought")
	}
}

func TestHandlerGetProductStatsSinceFilter(t *testing.T) {
	// Seed: receipt on 2026-05-01 (ISO 8601 with Z), order on 2026-05-10.
	// since=2026-05-05 should include the order but exclude the receipt.
	mux := newTestMux(t)

	// Without filter: both receipt and order purchases appear.
	rec := doRequest(mux, "GET", "/products/stats", nil)
	assertStatus(t, rec, http.StatusOK)
	var all []map[string]any
	decodeJSON(t, rec, &all)
	if len(all) == 0 {
		t.Fatal("expected stats without filter")
	}
	var totalAll float64
	for _, row := range all {
		if tb, ok := row["times_bought"].(float64); ok {
			totalAll += tb
		}
	}
	if totalAll == 0 {
		t.Error("expected non-zero times_bought without filter")
	}

	// With since=2026-05-05: only the order (2026-05-10) should be included.
	rec = doRequest(mux, "GET", "/products/stats?since=2026-05-05", nil)
	assertStatus(t, rec, http.StatusOK)
	var filtered []map[string]any
	decodeJSON(t, rec, &filtered)
	var totalFiltered float64
	for _, row := range filtered {
		if tb, ok := row["times_bought"].(float64); ok {
			totalFiltered += tb
		}
	}
	if totalFiltered >= totalAll {
		t.Errorf("filtered times_bought (%v) should be less than unfiltered (%v)", totalFiltered, totalAll)
	}
}

func TestHandlerGetProductStatsSinceInvalidDate(t *testing.T) {
	assertStatus(t, doRequest(newTestMux(t), "GET", "/products/stats?since=not-a-date", nil), http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// GET /products/nutriscores
// ---------------------------------------------------------------------------

func TestHandlerGetNutriscoreDistribution(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/products/nutriscores", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	// Seeded product 456 has nutriscore "B", purchased in both receipt and order.
	found := false
	for _, row := range body {
		if row["score"] == "B" {
			found = true
			if row["times_bought"] == nil {
				t.Error("missing times_bought for score B")
			}
		}
	}
	if !found {
		t.Error("expected nutriscore B in distribution")
	}
}

func TestHandlerGetNutriscoreDistributionSinceFilter(t *testing.T) {
	// Seed: receipt on 2026-05-01, order on 2026-05-10; both buy web_id=456 (nutriscore B).
	// since=2026-05-05 → only the order purchase should count.
	mux := newTestMux(t)

	rec := doRequest(mux, "GET", "/products/nutriscores", nil)
	assertStatus(t, rec, http.StatusOK)
	var all []map[string]any
	decodeJSON(t, rec, &all)
	var allTotal float64
	for _, row := range all {
		if tb, ok := row["times_bought"].(float64); ok {
			allTotal += tb
		}
	}

	rec = doRequest(mux, "GET", "/products/nutriscores?since=2026-05-05", nil)
	assertStatus(t, rec, http.StatusOK)
	var filtered []map[string]any
	decodeJSON(t, rec, &filtered)
	var filteredTotal float64
	for _, row := range filtered {
		if tb, ok := row["times_bought"].(float64); ok {
			filteredTotal += tb
		}
	}

	if filteredTotal >= allTotal {
		t.Errorf("filtered times_bought (%v) should be less than unfiltered (%v)", filteredTotal, allTotal)
	}
}

func TestHandlerGetNutriscoreDistributionSinceInvalidDate(t *testing.T) {
	assertStatus(t, doRequest(newTestMux(t), "GET", "/products/nutriscores?since=bad", nil), http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// GET /products/issues
// ---------------------------------------------------------------------------

func TestHandlerGetProductIssues(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/products/issues", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)
	summary, ok := body["summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing summary; body: %v", body)
	}
	if _, ok := summary["total_food_products"]; !ok {
		t.Error("missing total_food_products in summary")
	}
	// "Brood" has product_id but no web_id → should appear in no_web_id
	noWebID, _ := body["no_web_id"].([]any)
	if len(noWebID) != 1 {
		t.Errorf("want 1 no_web_id item, got %d", len(noWebID))
	}
}

// TestUnmatchedSubcategories verifies that /products/issues groups food products
// with an unmapped subcategory by that subcategory, counts affected products, and
// excludes products that are non-food, have no subcategory, or did match.
func TestUnmatchedSubcategories(t *testing.T) {
	db, path := newTestDBFile(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	exec(`INSERT INTO receipts VALUES ('TRX-U','2026-05-01T10:00:00Z',10.00)`)
	// Two unmatched products sharing one subcategory → one decision row, count 2.
	exec(`INSERT INTO products (web_id,title,ah_category,ah_subcategory) VALUES (201,'Tahin','Wereldkeuken','Midden-Oosterse pasta')`)
	exec(`INSERT INTO product_enrichment (web_id,match_method) VALUES (201,'unmatched')`)
	exec(`INSERT INTO products (web_id,title,ah_category,ah_subcategory) VALUES (202,'Sesampasta','Wereldkeuken','Midden-Oosterse pasta')`)
	exec(`INSERT INTO product_enrichment (web_id,match_method) VALUES (202,'unmatched')`)
	// A different unmatched subcategory → second decision row, count 1.
	exec(`INSERT INTO products (web_id,title,ah_category,ah_subcategory) VALUES (203,'Yuzu','Groente','Exotisch fruit')`)
	exec(`INSERT INTO product_enrichment (web_id,match_method) VALUES (203,'unmatched')`)
	// Excluded: matched, non_food, and unmatched-without-subcategory.
	exec(`INSERT INTO products (web_id,title,ah_category,ah_subcategory) VALUES (204,'Melk','Zuivel','Melk')`)
	exec(`INSERT INTO product_enrichment (web_id,match_method) VALUES (204,'subcategory_direct')`)
	exec(`INSERT INTO products (web_id,title,ah_category,ah_subcategory) VALUES (205,'Shampoo','Drogisterij','Shampoo')`)
	exec(`INSERT INTO product_enrichment (web_id,match_method) VALUES (205,'non_food')`)
	exec(`INSERT INTO products (web_id,title,ah_category,ah_subcategory) VALUES (206,'Onbekend','Diversen','')`)
	exec(`INSERT INTO product_enrichment (web_id,match_method) VALUES (206,'unmatched')`)
	db.Close()

	mux := http.NewServeMux()
	RegisterHandlers(mux, config.Server{DBPath: path})
	rec := doRequest(mux, "GET", "/products/issues", nil)
	assertStatus(t, rec, http.StatusOK)

	var body struct {
		Summary struct {
			UnmatchedSubcategories int `json:"unmatched_subcategories"`
			UnmatchedNoSubcategory int `json:"unmatched_no_subcategory"`
		} `json:"summary"`
		UnmatchedSubcategories []struct {
			AHCategory    string   `json:"ah_category"`
			AHSubcategory string   `json:"ah_subcategory"`
			ProductCount  int      `json:"product_count"`
			ExampleTitles []string `json:"example_titles"`
		} `json:"unmatched_subcategories"`
		UnmatchedNoSubcategory []struct {
			WebID int    `json:"web_id"`
			Title string `json:"title"`
		} `json:"unmatched_no_subcategory"`
	}
	decodeJSON(t, rec, &body)

	if len(body.UnmatchedSubcategories) != 2 {
		t.Fatalf("want 2 unmatched subcategories, got %d: %+v", len(body.UnmatchedSubcategories), body.UnmatchedSubcategories)
	}
	if body.Summary.UnmatchedSubcategories != 2 {
		t.Errorf("summary count = %d, want 2", body.Summary.UnmatchedSubcategories)
	}
	// Ordered by product_count DESC: the shared subcategory comes first.
	first := body.UnmatchedSubcategories[0]
	if first.AHSubcategory != "Midden-Oosterse pasta" || first.ProductCount != 2 {
		t.Errorf("first row = %q/%d, want Midden-Oosterse pasta/2", first.AHSubcategory, first.ProductCount)
	}
	if len(first.ExampleTitles) != 2 {
		t.Errorf("want 2 example titles, got %v", first.ExampleTitles)
	}
	// web_id 206 is unmatched with an empty subcategory → its own list, not grouped above.
	if body.Summary.UnmatchedNoSubcategory != 1 {
		t.Errorf("unmatched_no_subcategory count = %d, want 1", body.Summary.UnmatchedNoSubcategory)
	}
	if len(body.UnmatchedNoSubcategory) != 1 || body.UnmatchedNoSubcategory[0].WebID != 206 {
		t.Errorf("unmatched_no_subcategory list = %+v, want [web_id 206]", body.UnmatchedNoSubcategory)
	}
}

// ---------------------------------------------------------------------------
// GET /products/{web_id}
// ---------------------------------------------------------------------------

func TestHandlerGetProductDetail(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/products/456", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)
	if body["web_id"] != float64(456) {
		t.Errorf("unexpected web_id: %v", body["web_id"])
	}
	if body["title"] != "Melk Halfvol" {
		t.Errorf("unexpected title: %v", body["title"])
	}
	if body["co2eq_per_kg"] == nil {
		t.Error("missing co2eq_per_kg")
	}
}

func TestHandlerGetProductDetailNotFound(t *testing.T) {
	assertStatus(t, doRequest(newTestMux(t), "GET", "/products/9999", nil), http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// GET /products/{web_id}/purchases
// ---------------------------------------------------------------------------

func TestHandlerGetProductPurchases(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/products/456/purchases", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	// One receipt row + one order row for web_id 456
	if len(body) != 2 {
		t.Fatalf("want 2 purchases (1 receipt + 1 order), got %d", len(body))
	}
	sources := map[string]bool{}
	for _, p := range body {
		sources[p["source"].(string)] = true
	}
	if !sources["receipt"] || !sources["order"] {
		t.Errorf("expected both receipt and order purchases, got sources: %v", sources)
	}
}

// ---------------------------------------------------------------------------
// GET /corrections/missing-category
// ---------------------------------------------------------------------------

func TestHandlerGetMissingCategory(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/corrections/missing-category", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []any
	decodeJSON(t, rec, &body)
	// Product 456 has exact match with co2eq_per_kg set — not missing
	if len(body) != 0 {
		t.Errorf("want 0 items, got %d", len(body))
	}
}

// ---------------------------------------------------------------------------
// GET /corrections/missing-weight
// ---------------------------------------------------------------------------

func TestHandlerGetMissingWeight(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/corrections/missing-weight", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []any
	decodeJSON(t, rec, &body)
	// Product 456 has weight_kg=1.0 — not missing
	if len(body) != 0 {
		t.Errorf("want 0 items, got %d", len(body))
	}
}

// ---------------------------------------------------------------------------
// GET /pos/{pos_id}
// ---------------------------------------------------------------------------

func TestHandlerGetPosByID(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/pos/789", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)
	if body["pos_id"] != float64(789) {
		t.Errorf("unexpected pos_id: %v", body["pos_id"])
	}
	if body["web_id"] != float64(456) {
		t.Errorf("unexpected web_id: %v", body["web_id"])
	}
}

func TestHandlerGetPosByIDNotFound(t *testing.T) {
	assertStatus(t, doRequest(newTestMux(t), "GET", "/pos/9999", nil), http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// GET /search
// ---------------------------------------------------------------------------

func TestHandlerSearch(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/search?q=Melk", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)
	products, _ := body["products"].([]any)
	if len(products) == 0 {
		t.Error("expected products in search results")
	}
	if body["receipt_items"] == nil {
		t.Error("missing receipt_items key")
	}
	if body["order_items"] == nil {
		t.Error("missing order_items key")
	}
}

func TestHandlerSearchQueryTooShort(t *testing.T) {
	assertStatus(t, doRequest(newTestMux(t), "GET", "/search?q=M", nil), http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// GET /enrichment/count
// ---------------------------------------------------------------------------

func TestHandlerGetEnrichmentCount(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/enrichment/count", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)
	if body["count"] != float64(1) {
		t.Errorf("want count=1, got %v", body["count"])
	}
}

// ---------------------------------------------------------------------------
// GET /enrichment/pending
// ---------------------------------------------------------------------------

func TestHandlerGetEnrichmentPending(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/enrichment/pending", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)
	// All web_ids are enriched, so pending = 0
	if body["count"] != float64(0) {
		t.Errorf("want count=0, got %v", body["count"])
	}
}

// ---------------------------------------------------------------------------
// DELETE /enrichment
// ---------------------------------------------------------------------------

func TestHandlerDeleteAllEnrichment(t *testing.T) {
	mux := newTestMux(t)
	assertStatus(t, doRequest(mux, "DELETE", "/enrichment", nil), http.StatusNoContent)

	rec := doRequest(mux, "GET", "/enrichment/count", nil)
	var body map[string]any
	decodeJSON(t, rec, &body)
	if body["count"] != float64(0) {
		t.Errorf("want count=0 after delete, got %v", body["count"])
	}
}

// ---------------------------------------------------------------------------
// DELETE /enrichment/{web_id}
// ---------------------------------------------------------------------------

func TestHandlerDeleteProductEnrichment(t *testing.T) {
	rec := doRequest(newTestMux(t), "DELETE", "/enrichment/456", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)
	if body["deleted"] != float64(1) {
		t.Errorf("want deleted=1, got %v", body["deleted"])
	}
}

func TestHandlerDeleteProductEnrichmentMissing(t *testing.T) {
	rec := doRequest(newTestMux(t), "DELETE", "/enrichment/9999", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)
	if body["deleted"] != float64(0) {
		t.Errorf("want deleted=0, got %v", body["deleted"])
	}
}

// ---------------------------------------------------------------------------
// POST /database/reset
// ---------------------------------------------------------------------------

func TestHandlerDatabaseReset(t *testing.T) {
	mux := newTestMux(t)
	assertStatus(t, doRequest(mux, "POST", "/database/reset", nil), http.StatusNoContent)

	rec := doRequest(mux, "GET", "/receipts", nil)
	var body []any
	decodeJSON(t, rec, &body)
	if len(body) != 0 {
		t.Errorf("want empty receipts after reset, got %d", len(body))
	}
}

// ---------------------------------------------------------------------------
// GET /finances/summary
// ---------------------------------------------------------------------------

func TestHandlerGetFinancialSummary(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/finances/summary", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)

	for _, field := range []string{"total_spent", "avg_per_year", "avg_per_month", "avg_per_week", "total_discount", "discount_avg_per_year", "discount_avg_per_month", "discount_avg_per_week", "first_date", "last_date"} {
		if _, ok := body[field]; !ok {
			t.Errorf("missing field %q in response", field)
		}
	}
	// Seeded: receipt €25.50 + order €50.00 = €75.50
	totalSpent, _ := body["total_spent"].(float64)
	if totalSpent <= 0 {
		t.Errorf("total_spent = %v, want > 0", totalSpent)
	}
	// Seeded discount: €2.50
	totalDiscount, _ := body["total_discount"].(float64)
	if totalDiscount != 2.50 {
		t.Errorf("total_discount = %v, want 2.50", totalDiscount)
	}
}

func TestHandlerGetFinancialSummaryAveragesNonZero(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/finances/summary", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)

	for _, avg := range []string{"avg_per_year", "avg_per_month", "avg_per_week"} {
		v, _ := body[avg].(float64)
		if v <= 0 {
			t.Errorf("%s = %v, want > 0 (dates span multiple days)", avg, v)
		}
	}
}

func TestHandlerGetFinancialSummarySinceFilter(t *testing.T) {
	// Seed: receipt on 2026-05-01 (€25.50, discount €2.50), order on 2026-05-10 (€50.00).
	// since=2026-05-05 should include only the order.
	mux := newTestMux(t)

	rec := doRequest(mux, "GET", "/finances/summary?since=2026-05-05", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)

	// Only the order should be included (€50.00), receipt (€25.50) excluded.
	totalSpent, _ := body["total_spent"].(float64)
	if totalSpent <= 0 || totalSpent >= 75.0 {
		t.Errorf("total_spent with since filter = %v, want ~50.00 (order only)", totalSpent)
	}
	// Discount belongs to the receipt, so should be excluded.
	totalDiscount, _ := body["total_discount"].(float64)
	if totalDiscount != 0 {
		t.Errorf("total_discount with since filter = %v, want 0 (receipt excluded)", totalDiscount)
	}
}

func TestHandlerGetFinancialSummarySinceInvalidDate(t *testing.T) {
	assertStatus(t, doRequest(newTestMux(t), "GET", "/finances/summary?since=not-a-date", nil), http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// GET /finances/by-category
// ---------------------------------------------------------------------------

func TestHandlerGetSpendingByCategory(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/finances/by-category", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)

	if len(body) == 0 {
		t.Fatal("expected at least one category row")
	}
	for _, row := range body {
		if _, ok := row["category"]; !ok {
			t.Error("row missing 'category'")
		}
		if _, ok := row["subcategory"]; !ok {
			t.Error("row missing 'subcategory'")
		}
		if _, ok := row["total_spent"]; !ok {
			t.Error("row missing 'total_spent'")
		}
	}
	// Product 456 is in category "Zuivel" — should appear.
	found := false
	for _, row := range body {
		if row["category"] == "Zuivel" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'Zuivel' category in results")
	}
}

func TestHandlerGetSpendingByCategorySortedDesc(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/finances/by-category", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)

	for i := 1; i < len(body); i++ {
		prev, _ := body[i-1]["total_spent"].(float64)
		curr, _ := body[i]["total_spent"].(float64)
		if prev < curr {
			t.Errorf("results not sorted descending at index %d: %v < %v", i, prev, curr)
		}
	}
}

// ---------------------------------------------------------------------------
// GET /finances/over-time
// ---------------------------------------------------------------------------

func TestHandlerGetSpendingOverTime(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/finances/over-time", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)

	if len(body) == 0 {
		t.Fatal("expected at least one period row")
	}
	for _, row := range body {
		if _, ok := row["period"]; !ok {
			t.Error("row missing 'period'")
		}
		if _, ok := row["amount"]; !ok {
			t.Error("row missing 'amount'")
		}
	}
}

func TestHandlerGetSpendingOverTimeYear(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/finances/over-time?period=year", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)

	if len(body) == 0 {
		t.Fatal("expected at least one year row")
	}
	// Year format should be 4-digit year only.
	period, _ := body[0]["period"].(string)
	if len(period) != 4 {
		t.Errorf("year period = %q, expected 4-char year (e.g. '2026')", period)
	}
}

func TestHandlerGetSpendingOverTimeDay(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/finances/over-time?period=day", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)

	if len(body) == 0 {
		t.Fatal("expected at least one day row")
	}
	// Day format should be YYYY-MM-DD (10 chars).
	period, _ := body[0]["period"].(string)
	if len(period) != 10 {
		t.Errorf("day period = %q, expected YYYY-MM-DD (10 chars)", period)
	}
}

func TestHandlerGetSpendingOverTimeInvalidPeriod(t *testing.T) {
	// Unknown period falls back to month format — should still return 200.
	rec := doRequest(newTestMux(t), "GET", "/finances/over-time?period=banana", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	if len(body) == 0 {
		t.Fatal("expected rows even for invalid period (falls back to month)")
	}
	// Month fallback: YYYY-MM (7 chars).
	period, _ := body[0]["period"].(string)
	if len(period) != 7 {
		t.Errorf("fallback period = %q, expected YYYY-MM (7 chars)", period)
	}
}

// ---------------------------------------------------------------------------
// GET /sustainability/summary
// ---------------------------------------------------------------------------

func TestHandlerSustainabilitySummary(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/sustainability/summary?household_ae=1.0", nil)
	assertStatus(t, rec, http.StatusOK)
	var body map[string]any
	decodeJSON(t, rec, &body)
	// Seeded: web_id 456, co2eq_per_kg=3.2, weight_kg=1.0
	// receipt: qty=2 → co2=6.4; order: qty=3 → co2=9.6 → monthly avg depends on months
	if _, ok := body["grade"]; !ok {
		t.Error("missing grade field")
	}
	if _, ok := body["pct_above_sustainable"]; !ok {
		t.Error("missing pct_above_sustainable field")
	}
	if _, ok := body["top_category"]; !ok {
		t.Error("missing top_category field")
	}
}

func TestHandlerSustainabilitySummaryInvalidSince(t *testing.T) {
	assertStatus(t, doRequest(newTestMux(t), "GET", "/sustainability/summary?since=not-a-date", nil), http.StatusBadRequest)
}

func TestHandlerSustainabilitySummaryInvalidAE(t *testing.T) {
	assertStatus(t, doRequest(newTestMux(t), "GET", "/sustainability/summary?household_ae=notanumber", nil), http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// GET /sustainability/trend
// ---------------------------------------------------------------------------

func TestHandlerSustainabilityTrend(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/sustainability/trend?period=month", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	// Seeded data should produce at least one trend entry
	if len(body) == 0 {
		t.Fatal("expected at least one trend entry")
	}
	for _, e := range body {
		if _, ok := e["period"]; !ok {
			t.Error("missing period field")
		}
		if _, ok := e["category"]; !ok {
			t.Error("missing category field")
		}
		if _, ok := e["co2eq"]; !ok {
			t.Error("missing co2eq field")
		}
	}
}

func TestHandlerSustainabilityTrendDefaultPeriodIsMonth(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/sustainability/trend", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	if len(body) == 0 {
		t.Fatal("expected trend entries with default period")
	}
	period, _ := body[0]["period"].(string)
	if len(period) != 7 {
		t.Errorf("default period = %q, expected YYYY-MM (7 chars)", period)
	}
}

func TestHandlerSustainabilityTrendYearPeriod(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/sustainability/trend?period=year", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	if len(body) == 0 {
		t.Fatal("expected trend entries for year period")
	}
	period, _ := body[0]["period"].(string)
	if len(period) != 4 {
		t.Errorf("year period = %q, expected 4-char year", period)
	}
}

// ---------------------------------------------------------------------------
// GET /sustainability/categories
// ---------------------------------------------------------------------------

func TestHandlerSustainabilityCategories(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/sustainability/categories", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	if len(body) == 0 {
		t.Fatal("expected at least one category")
	}
	for _, e := range body {
		if _, ok := e["category"]; !ok {
			t.Error("missing category field")
		}
		if _, ok := e["co2eq"]; !ok {
			t.Error("missing co2eq field")
		}
	}
	// Sorted descending by co2eq
	for i := 1; i < len(body); i++ {
		prev, _ := body[i-1]["co2eq"].(float64)
		curr, _ := body[i]["co2eq"].(float64)
		if prev < curr {
			t.Errorf("categories not sorted descending at index %d", i)
		}
	}
}

// ---------------------------------------------------------------------------
// GET /sustainability/categories/{category}/products
// ---------------------------------------------------------------------------

func TestHandlerCategoryProducts(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/sustainability/categories/dairy/products", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	// Seeded: co2eq_category='dairy' for web_id 456
	if len(body) == 0 {
		t.Fatal("expected at least one product")
	}
	for _, p := range body {
		for _, field := range []string{"description", "co2eq_total", "percentage_of_category"} {
			if _, ok := p[field]; !ok {
				t.Errorf("missing field %q", field)
			}
		}
	}
}

func TestHandlerCategoryProductsWithPeriodFilter(t *testing.T) {
	mux := newTestMux(t)
	// Seeded receipt on 2026-05 and order on 2026-05 — both match
	rec := doRequest(mux, "GET", "/sustainability/categories/dairy/products?period_type=month&period_label=2026-05", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	if len(body) == 0 {
		t.Fatal("expected products for 2026-05")
	}
}

func TestHandlerCategoryProductsPeriodNoMatch(t *testing.T) {
	rec := doRequest(newTestMux(t), "GET", "/sustainability/categories/dairy/products?period_type=month&period_label=2099-01", nil)
	assertStatus(t, rec, http.StatusOK)
	var body []map[string]any
	decodeJSON(t, rec, &body)
	if len(body) != 0 {
		t.Errorf("expected 0 products for future period, got %d", len(body))
	}
}
