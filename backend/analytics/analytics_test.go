package analytics

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestGetProductDetailWeightBreakdown(t *testing.T) {
	db, _ := newTestDBFile(t) // full schema (pos_id, net_content, serving_size, weight_source)
	db.Exec(`INSERT INTO products (web_id, title, unit_size, net_content, serving_size)
	         VALUES (1, 'Broodjes', '6 stuks', '300 g', '50 g')`)
	db.Exec(`INSERT INTO product_enrichment (web_id, co2eq_per_kg, match_method, weight_kg, weight_source)
	         VALUES (1, 1.0, 'subcategory_direct', 0.3, 'net_content')`)

	p, err := getProductDetail(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("product not found")
	}
	if p.WeightSource != "net_content" {
		t.Errorf("WeightSource = %q, want net_content", p.WeightSource)
	}

	fptr := func(f float64) *float64 { return &f }
	want := []struct {
		source string
		val    *float64
		active bool
	}{
		{"correction", nil, false},
		{"unit_size", nil, false},        // "6 stuks" → not parseable
		{"net_content", fptr(0.3), true}, // "300 g"
		{"multipack", nil, false},
		{"default", nil, false},
		{"serving_size", fptr(0.3), false}, // 6 × "50 g" (explicit count, shown but not used here)
	}
	if len(p.WeightBreakdown) != len(want) {
		t.Fatalf("breakdown len = %d, want %d", len(p.WeightBreakdown), len(want))
	}
	for i, w := range want {
		got := p.WeightBreakdown[i]
		if got.Source != w.source || got.Active != w.active {
			t.Errorf("row %d = (%s, active=%v), want (%s, active=%v)", i, got.Source, got.Active, w.source, w.active)
		}
		if (w.val == nil) != (got.ValueKg == nil) {
			t.Errorf("row %s ValueKg = %v, want nil=%v", w.source, got.ValueKg, w.val == nil)
		} else if w.val != nil && !approxEq(*w.val, *got.ValueKg, 1e-9) {
			t.Errorf("row %s ValueKg = %v, want %v", w.source, *got.ValueKg, *w.val)
		}
	}
}

// TestGetMissingWeightUnparseableUnitSize verifies that with no 1 kg fallback, a
// product whose unit_size can't be parsed (and isn't stuk/per) still surfaces in
// the missing-weight list, while parseable ones (incl. "los per 500 g") do not.
func TestGetMissingWeightUnparseableUnitSize(t *testing.T) {
	db := newTestDB(t)
	seed := func(webID int, unitSize string) {
		db.Exec(`INSERT INTO products (web_id, title, unit_size) VALUES (?, ?, ?)`, webID, "P", unitSize)
		db.Exec(`INSERT INTO product_enrichment (web_id, co2eq_per_kg, match_method, weight_kg) VALUES (?, 3.0, 'subcategory_direct', NULL)`, webID)
	}
	seed(1, "1 rol")         // unparseable, non-stuk/per → missing
	seed(2, "500 g")         // parseable → not missing
	seed(3, "los per 500 g") // parseable via "per" → not missing

	items, err := getMissingWeight(db)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]bool{}
	for _, it := range items {
		got[it.WebID] = true
	}
	if !got[1] {
		t.Error("web_id 1 (unparseable unit_size) should be in missing-weight list")
	}
	if got[2] || got[3] {
		t.Errorf("parseable unit_sizes should not be listed: got %v", got)
	}
}

// ---------------------------------------------------------------------------
// computeCO2Total
// ---------------------------------------------------------------------------

func TestComputeCO2TotalNilInputs(t *testing.T) {
	co2 := 10.0
	w := 0.5
	if computeCO2Total(nil, 2, &w) != nil {
		t.Error("expected nil when co2PerKg is nil")
	}
	if computeCO2Total(&co2, 2, nil) != nil {
		t.Error("expected nil when weightPerUnitKg is nil")
	}
}

func TestComputeCO2Total(t *testing.T) {
	co2 := 27.0 // kg CO2 per kg product
	w := 0.5    // kg per unit
	qty := 2
	got := computeCO2Total(&co2, qty, &w)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	// 27 * 2 * 0.5 = 27.0
	if !approxEq(*got, 27.0, 1e-6) {
		t.Errorf("computeCO2Total = %v, want 27.0", *got)
	}
}

// ---------------------------------------------------------------------------
// DB query helpers
// ---------------------------------------------------------------------------

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
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
		CREATE TABLE orders (
			order_id        INTEGER PRIMARY KEY,
			delivery_date   TEXT NOT NULL,
			delivery_method TEXT,
			delivery_status TEXT,
			total_price     REAL,
			invoice_id      TEXT,
			address_street  TEXT,
			address_number  TEXT,
			address_extra   TEXT,
			address_postcode TEXT,
			address_city    TEXT
		);
		CREATE TABLE order_items (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id      INTEGER NOT NULL REFERENCES orders(order_id),
			web_id        INTEGER NOT NULL,
			title         TEXT NOT NULL,
			brand         TEXT,
			category      TEXT,
			sales_unit_size TEXT,
			quantity      INTEGER NOT NULL,
			allocated_qty INTEGER NOT NULL,
			unit_price    REAL,
			was_price     REAL,
			image_url     TEXT
		);
		CREATE TABLE receipt_discounts (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			receipt_id TEXT NOT NULL REFERENCES receipts(transaction_id),
			name       TEXT,
			amount     REAL NOT NULL
		);`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// ---------------------------------------------------------------------------
// getReceipts
// ---------------------------------------------------------------------------

func TestGetReceiptsEmpty(t *testing.T) {
	db := newTestDB(t)
	receipts, err := getReceipts(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 0 {
		t.Errorf("expected 0 receipts, got %d", len(receipts))
	}
}

func TestGetReceiptsWithCO2(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-15', 45.00)`)
	db.Exec(`INSERT INTO products VALUES (100, 'Kipfilet', 'AH', 'Vlees', NULL, '500g', 'A', NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Kipfilet', 2, 6.00, 100)`)
	// co2eq_per_kg=4.0, weight from unit_size "500g" = 0.5 kg, qty=2 → 4*2*0.5 = 4.0
	db.Exec(`INSERT INTO product_enrichment VALUES (100, 'Vlees', 'Kipfilet', 4.0, 'keyword', NULL, NULL)`)

	receipts, err := getReceipts(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}
	r := receipts[0]
	if r.TransactionID != "r1" {
		t.Errorf("TransactionID = %q, want r1", r.TransactionID)
	}
	if r.CO2EqTotal == nil {
		t.Fatal("CO2EqTotal should not be nil")
	}
	if !approxEq(*r.CO2EqTotal, 4.0, 1e-3) {
		t.Errorf("CO2EqTotal = %v, want 4.0", *r.CO2EqTotal)
	}
}

func TestGetReceiptsCO2WithWeightOverride(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-15', 10.00)`)
	db.Exec(`INSERT INTO products VALUES (200, 'Biefstuk', 'AH', 'Vlees', NULL, '200g', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Biefstuk', 1, 8.00, 200)`)
	// weight_kg override = 0.3 (overrides unit_size "200g" = 0.2); co2=27 * 1 * 0.3 = 8.1
	db.Exec(`INSERT INTO product_enrichment VALUES (200, 'Vlees', 'Rund', 27.0, 'keyword', 0.3, NULL)`)

	receipts, err := getReceipts(db)
	if err != nil {
		t.Fatal(err)
	}
	if receipts[0].CO2EqTotal == nil || !approxEq(*receipts[0].CO2EqTotal, 8.1, 1e-3) {
		t.Errorf("CO2EqTotal = %v, want 8.1 (weight override applied)", receipts[0].CO2EqTotal)
	}
}

func TestGetReceiptsNoCO2WhenNotEnriched(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-15', 5.00)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount) VALUES ('r1', 'Groente', 1, 2.00)`)

	receipts, err := getReceipts(db)
	if err != nil {
		t.Fatal(err)
	}
	if receipts[0].CO2EqTotal != nil {
		t.Errorf("CO2EqTotal should be nil when item has no enrichment, got %v", *receipts[0].CO2EqTotal)
	}
}

func TestGetReceiptsOrderedByDateDesc(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-01', 10.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2024-03-15', 20.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r3', '2024-02-10', 15.00)`)

	receipts, err := getReceipts(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 3 {
		t.Fatalf("expected 3 receipts, got %d", len(receipts))
	}
	if receipts[0].TransactionID != "r2" || receipts[1].TransactionID != "r3" || receipts[2].TransactionID != "r1" {
		t.Errorf("wrong order: %v %v %v", receipts[0].TransactionID, receipts[1].TransactionID, receipts[2].TransactionID)
	}
}

// ---------------------------------------------------------------------------
// getReceiptDetail
// ---------------------------------------------------------------------------

func TestGetReceiptDetailNotFound(t *testing.T) {
	db := newTestDB(t)
	detail, err := getReceiptDetail(db, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if detail != nil {
		t.Error("expected nil for missing receipt")
	}
}

func TestGetReceiptDetailWithItems(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-06-01', 12.00)`)
	db.Exec(`INSERT INTO products VALUES (10, 'Zalm', 'AH', 'Vis', NULL, '300g', NULL, NULL, NULL, 'https://img/zalm.jpg', NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Zalm', 1, 5.00, 10)`)
	// co2=6.0 * 1 * 0.3 (from 300g) = 1.8
	db.Exec(`INSERT INTO product_enrichment VALUES (10, 'Vis', 'Zalm', 6.0, 'keyword', NULL, NULL)`)

	detail, err := getReceiptDetail(db, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if detail == nil {
		t.Fatal("expected non-nil detail")
	}
	if detail.TransactionID != "r1" {
		t.Errorf("TransactionID = %q", detail.TransactionID)
	}
	if len(detail.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(detail.Items))
	}
	item := detail.Items[0]
	if item.Description != "Zalm" {
		t.Errorf("Description = %q", item.Description)
	}
	if item.CO2EqTotal == nil || !approxEq(*item.CO2EqTotal, 1.8, 1e-3) {
		t.Errorf("CO2EqTotal = %v, want 1.8", item.CO2EqTotal)
	}
	if item.WeightPerUnitKg == nil || !approxEq(*item.WeightPerUnitKg, 0.3, 1e-3) {
		t.Errorf("WeightPerUnitKg = %v, want 0.3", item.WeightPerUnitKg)
	}
	if item.ThumbnailURL != "https://img/zalm.jpg" {
		t.Errorf("ThumbnailURL = %q", item.ThumbnailURL)
	}
}

// ---------------------------------------------------------------------------
// getItems (combined receipt + order)
// ---------------------------------------------------------------------------

func TestGetItemsEmpty(t *testing.T) {
	db := newTestDB(t)
	items, err := getItems(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestGetItemsCombinesReceiptsAndOrders(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-10', 20.00)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount) VALUES ('r1', 'Appels', 3, 4.50)`)

	db.Exec(`INSERT INTO orders (order_id, delivery_date, delivery_method, delivery_status, total_price) VALUES (42, '2024-02-20', 'delivery', 'delivered', 35.00)`)
	db.Exec(`INSERT INTO order_items (order_id, web_id, title, quantity, allocated_qty, unit_price) VALUES (42, 99, 'Biologische Appels', 2, 2, 3.50)`)

	items, err := getItems(db)
	if err != nil {
		t.Fatal(err)
	}

	var receiptCount, orderCount int
	for _, it := range items {
		switch it.SourceType {
		case "receipt":
			receiptCount++
		case "order":
			orderCount++
		}
	}
	if receiptCount != 1 {
		t.Errorf("receipt items = %d, want 1", receiptCount)
	}
	if orderCount != 1 {
		t.Errorf("order items = %d, want 1", orderCount)
	}
}

func TestGetItemsComputesCO2(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-10', 10.00)`)
	db.Exec(`INSERT INTO products VALUES (5, 'Volle melk', 'AH', 'Zuivel', NULL, '1 liter', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Melk', 2, 3.00, 5)`)
	// co2=3.2 * 2 * 1.0 (1 liter ≈ 1 kg) = 6.4
	db.Exec(`INSERT INTO product_enrichment VALUES (5, 'Zuivel', 'Volle melk', 3.2, 'keyword', NULL, NULL)`)

	items, err := getItems(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("expected items")
	}
	it := items[0]
	if it.CO2EqTotal == nil || !approxEq(*it.CO2EqTotal, 6.4, 1e-3) {
		t.Errorf("CO2EqTotal = %v, want 6.4", it.CO2EqTotal)
	}
}

// ---------------------------------------------------------------------------
// getProducts
// ---------------------------------------------------------------------------

func TestGetProductsFiltersEmptyTitles(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO products VALUES (1, 'Kipfilet', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO products VALUES (2, '', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO products VALUES (3, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO products VALUES (4, '   ', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)

	products, err := getProducts(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Errorf("expected 1 product (with title), got %d", len(products))
	}
	if products[0].WebID != 1 {
		t.Errorf("WebID = %d, want 1", products[0].WebID)
	}
}

func TestGetProductsIncludesEnrichment(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO products VALUES (10, 'Zalm', 'AH', 'Vis', 'Zeevis', '200g', 'B', NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO product_enrichment VALUES (10, 'Vis', 'Zalm', 6.0, 'keyword', NULL, NULL)`)

	products, err := getProducts(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	p := products[0]
	if p.CO2EqPerKg == nil || *p.CO2EqPerKg != 6.0 {
		t.Errorf("CO2EqPerKg = %v, want 6.0", p.CO2EqPerKg)
	}
	if p.CO2EqCategory != "Vis" {
		t.Errorf("CO2EqCategory = %q, want Vis", p.CO2EqCategory)
	}
	if p.AHSubcategory != "Zeevis" {
		t.Errorf("AHSubcategory = %q, want Zeevis", p.AHSubcategory)
	}
}

func TestGetProductsOrderedByTitle(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO products VALUES (1, 'Zalm', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO products VALUES (2, 'Appel', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO products VALUES (3, 'Melk', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)

	products, err := getProducts(db)
	if err != nil {
		t.Fatal(err)
	}
	if products[0].Title != "Appel" || products[1].Title != "Melk" || products[2].Title != "Zalm" {
		t.Errorf("wrong order: %v %v %v", products[0].Title, products[1].Title, products[2].Title)
	}
}

// ---------------------------------------------------------------------------
// getOrders
// ---------------------------------------------------------------------------

func TestGetOrdersEmpty(t *testing.T) {
	db := newTestDB(t)
	orders, err := getOrders(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 0 {
		t.Errorf("expected 0 orders, got %d", len(orders))
	}
}

func TestGetOrdersOrderedByDateDesc(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO orders (order_id, delivery_date, delivery_method, delivery_status, total_price) VALUES (1, '2024-01-01', 'delivery', 'delivered', 30.00)`)
	db.Exec(`INSERT INTO orders (order_id, delivery_date, delivery_method, delivery_status, total_price) VALUES (2, '2024-03-15', 'pickup', 'delivered', 45.00)`)

	orders, err := getOrders(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}
	if orders[0].OrderID != 2 || orders[1].OrderID != 1 {
		t.Errorf("wrong order: %d %d", orders[0].OrderID, orders[1].OrderID)
	}
}

func TestGetOrdersNullableFields(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO orders (order_id, delivery_date) VALUES (1, '2024-01-01')`)

	orders, err := getOrders(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orders))
	}
	o := orders[0]
	if o.DeliveryMethod != "" || o.DeliveryStatus != "" || o.TotalPrice != 0 {
		t.Errorf("nullable fields should default to zero values: method=%q status=%q price=%v",
			o.DeliveryMethod, o.DeliveryStatus, o.TotalPrice)
	}
}

// ---------------------------------------------------------------------------
// getNutriscoreDistribution
// ---------------------------------------------------------------------------

func TestGetNutriscoreDistributionEmpty(t *testing.T) {
	db := newTestDB(t)
	entries, err := getNutriscoreDistribution(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestGetNutriscoreDistribution(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-15', 20.00)`)
	db.Exec(`INSERT INTO products VALUES (1, 'Melk', 'AH', NULL, NULL, '1L', 'A', NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO products VALUES (2, 'Chips', 'AH', NULL, NULL, '200g', 'D', NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO products VALUES (3, 'Yoghurt', 'AH', NULL, NULL, '500g', 'A', NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Melk', 2, 4.00, 1)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Chips', 1, 2.00, 2)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Yoghurt', 3, 6.00, 3)`)

	entries, err := getNutriscoreDistribution(db, nil)
	if err != nil {
		t.Fatal(err)
	}

	byScore := make(map[string]NutriscoreEntry)
	for _, e := range entries {
		byScore[e.Score] = e
	}

	if a, ok := byScore["A"]; !ok {
		t.Error("expected entry for score A")
	} else {
		if a.Count != 2 {
			t.Errorf("score A count = %d, want 2", a.Count)
		}
		if a.TimesBought != 5 {
			t.Errorf("score A times_bought = %d, want 5 (2+3)", a.TimesBought)
		}
	}

	if d, ok := byScore["D"]; !ok {
		t.Error("expected entry for score D")
	} else {
		if d.Count != 1 {
			t.Errorf("score D count = %d, want 1", d.Count)
		}
		if d.TimesBought != 1 {
			t.Errorf("score D times_bought = %d, want 1", d.TimesBought)
		}
	}
}

func TestGetNutriscoreDistributionUnknownScore(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-15', 10.00)`)
	db.Exec(`INSERT INTO products VALUES (1, 'Brood', 'AH', NULL, NULL, '800g', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Brood', 1, 2.00, 1)`)

	entries, err := getNutriscoreDistribution(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Score != "" {
		t.Errorf("expected one entry with empty score, got %v", entries)
	}
}

// ---------------------------------------------------------------------------
// getNutriscoreProducts
// ---------------------------------------------------------------------------

func TestGetNutriscoreProductsEmpty(t *testing.T) {
	db := newTestDB(t)
	products, err := getNutriscoreProducts(db, "A", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 0 {
		t.Errorf("expected 0 products, got %d", len(products))
	}
}

func TestGetNutriscoreProductsFiltersScore(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-15', 20.00)`)
	db.Exec(`INSERT INTO products VALUES (1, 'Melk', 'AH', NULL, NULL, '1L', 'A', NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO products VALUES (2, 'Chips', 'AH', NULL, NULL, '200g', 'D', NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Melk', 2, 4.00, 1)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Chips', 1, 2.00, 2)`)

	products, err := getNutriscoreProducts(db, "A", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product with score A, got %d", len(products))
	}
	p := products[0]
	if p.WebID != 1 {
		t.Errorf("WebID = %d, want 1", p.WebID)
	}
	if p.Nutriscore != "A" {
		t.Errorf("Nutriscore = %q, want A", p.Nutriscore)
	}
	if p.TotalSpent != 4.00 {
		t.Errorf("TotalSpent = %v, want 4.00", p.TotalSpent)
	}
}

func TestGetNutriscoreProductsIncludesOrderItems(t *testing.T) {
	db := newTestDB(t)

	db.Exec(`INSERT INTO products VALUES (10, 'Yoghurt', 'AH', NULL, NULL, '500g', 'B', NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO orders (order_id, delivery_date, delivery_method, delivery_status, total_price) VALUES (1, '2024-02-01', 'delivery', 'delivered', 5.00)`)
	db.Exec(`INSERT INTO order_items (order_id, web_id, title, quantity, allocated_qty, unit_price) VALUES (1, 10, 'Yoghurt', 2, 2, 2.50)`)

	products, err := getNutriscoreProducts(db, "B", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product with score B, got %d", len(products))
	}
	if products[0].WebID != 10 {
		t.Errorf("WebID = %d, want 10", products[0].WebID)
	}
}

// ---------------------------------------------------------------------------
// getFinancialSummary
// ---------------------------------------------------------------------------

func TestGetFinancialSummaryEmpty(t *testing.T) {
	db := newTestDB(t)
	s, err := getFinancialSummary(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalSpent != 0 || s.TotalDiscount != 0 {
		t.Errorf("expected zeros on empty DB, got spent=%v discount=%v", s.TotalSpent, s.TotalDiscount)
	}
	if s.AvgPerYear != 0 || s.AvgPerMonth != 0 || s.AvgPerWeek != 0 {
		t.Error("expected zero averages on empty DB")
	}
}

func TestGetFinancialSummaryTotalSpent(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-15', 30.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2024-07-15', 20.00)`)
	db.Exec(`INSERT INTO orders (order_id, delivery_date, delivery_method, delivery_status, total_price) VALUES (1, '2024-04-01', 'delivery', 'delivered', 50.00)`)

	s, err := getFinancialSummary(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !approxEq(s.TotalSpent, 100.0, 1e-6) {
		t.Errorf("TotalSpent = %v, want 100.0", s.TotalSpent)
	}
}

func TestGetFinancialSummaryDiscount(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-15', 20.00)`)
	db.Exec(`INSERT INTO receipt_discounts (receipt_id, name, amount) VALUES ('r1', 'AH Bonus', 3.50)`)
	db.Exec(`INSERT INTO receipt_discounts (receipt_id, name, amount) VALUES ('r1', 'Wekelijks Korting', 1.00)`)

	s, err := getFinancialSummary(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !approxEq(s.TotalDiscount, 4.50, 1e-6) {
		t.Errorf("TotalDiscount = %v, want 4.50", s.TotalDiscount)
	}
}

func TestGetFinancialSummaryAveragesComputed(t *testing.T) {
	db := newTestDB(t)
	// Two receipts ~365 days apart; total = 365.25 → avg/year ≈ 365.25.
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-01', 182.625)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2025-01-01', 182.625)`)

	s, err := getFinancialSummary(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.AvgPerYear == 0 {
		t.Error("AvgPerYear should be non-zero when date range spans multiple dates")
	}
	if s.AvgPerMonth == 0 || s.AvgPerWeek == 0 {
		t.Error("AvgPerMonth and AvgPerWeek should be non-zero")
	}
	// AvgPerMonth should be about AvgPerYear / 12.
	ratio := s.AvgPerYear / s.AvgPerMonth
	if ratio < 10 || ratio > 14 {
		t.Errorf("AvgPerYear/AvgPerMonth ratio = %v, expected ~12", ratio)
	}
}

func TestGetFinancialSummaryDiscountAveragesComputed(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-01', 100.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2025-01-01', 100.00)`)
	db.Exec(`INSERT INTO receipt_discounts (receipt_id, name, amount) VALUES ('r1', 'AH Bonus', 12.00)`)

	s, err := getFinancialSummary(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.DiscountAvgPerYear == 0 {
		t.Error("DiscountAvgPerYear should be non-zero")
	}
	if s.DiscountAvgPerMonth == 0 || s.DiscountAvgPerWeek == 0 {
		t.Error("DiscountAvgPerMonth and DiscountAvgPerWeek should be non-zero")
	}
	// Discount averages should scale proportionally to spend averages.
	if s.AvgPerYear == 0 {
		t.Fatal("AvgPerYear is zero, cannot check ratio")
	}
	ratio := s.DiscountAvgPerYear / s.AvgPerYear
	discRatio := s.TotalDiscount / s.TotalSpent
	if !approxEq(ratio, discRatio, 0.01) {
		t.Errorf("discount/spend ratio mismatch: avg ratio=%v, total ratio=%v", ratio, discRatio)
	}
}

// ---------------------------------------------------------------------------
// getSpendingByCategory
// ---------------------------------------------------------------------------

func TestGetSpendingByCategoryEmpty(t *testing.T) {
	db := newTestDB(t)
	rows, err := getSpendingByCategory(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows on empty DB, got %d", len(rows))
	}
}

func TestGetSpendingByCategoryFromReceiptItems(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-03-01', 10.00)`)
	db.Exec(`INSERT INTO products VALUES (1, 'Melk', 'AH', 'Zuivel', 'Melk', '1L', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO products VALUES (2, 'Chips', 'AH', 'Snacks', 'Chips', '200g', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Melk', 2, 4.00, 1)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Chips', 1, 3.00, 2)`)

	rows, err := getSpendingByCategory(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	byKey := make(map[string]float64)
	for _, r := range rows {
		byKey[r.Category+"/"+r.Subcategory] = r.TotalSpent
	}
	if !approxEq(byKey["Zuivel/Melk"], 4.0, 1e-6) {
		t.Errorf("Zuivel/Melk = %v, want 4.0", byKey["Zuivel/Melk"])
	}
	if !approxEq(byKey["Snacks/Chips"], 3.0, 1e-6) {
		t.Errorf("Snacks/Chips = %v, want 3.0", byKey["Snacks/Chips"])
	}
}

func TestGetSpendingByCategoryUnknownFallback(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-03-01', 5.00)`)
	// Item with no web_id — no category info.
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount) VALUES ('r1', 'Losse groente', 1, 2.50)`)

	rows, err := getSpendingByCategory(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Category == "Onbekend" {
			found = true
			if !approxEq(r.TotalSpent, 2.50, 1e-6) {
				t.Errorf("Onbekend total = %v, want 2.50", r.TotalSpent)
			}
		}
	}
	if !found {
		t.Error("expected an 'Onbekend' category row for items without web_id")
	}
}

func TestGetSpendingByCategoryFromOrderItems(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO orders (order_id, delivery_date, delivery_method, delivery_status, total_price) VALUES (1, '2024-03-01', 'delivery', 'delivered', 10.00)`)
	db.Exec(`INSERT INTO products VALUES (10, 'Yoghurt', 'AH', 'Zuivel', 'Yoghurt', '500g', NULL, NULL, NULL, NULL, NULL, NULL)`)
	// Order item with web_id → picks up ah_category from products.
	db.Exec(`INSERT INTO order_items (order_id, web_id, title, quantity, allocated_qty, unit_price) VALUES (1, 10, 'Yoghurt', 2, 2, 2.00)`)

	rows, err := getSpendingByCategory(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Category == "Zuivel" {
			found = true
			if !approxEq(r.TotalSpent, 4.0, 1e-6) {
				t.Errorf("Zuivel total = %v, want 4.0 (2 * €2.00)", r.TotalSpent)
			}
		}
	}
	if !found {
		t.Error("expected Zuivel row from order items")
	}
}

func TestGetSpendingByCategorySortedByTotalDesc(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-03-01', 20.00)`)
	db.Exec(`INSERT INTO products VALUES (1, 'Melk', 'AH', 'Zuivel', 'Melk', '1L', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO products VALUES (2, 'Chips', 'AH', 'Snacks', 'Chips', '200g', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Melk', 1, 2.00, 1)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Chips', 1, 15.00, 2)`)

	rows, err := getSpendingByCategory(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 rows, got %d", len(rows))
	}
	if rows[0].TotalSpent < rows[1].TotalSpent {
		t.Errorf("results should be sorted descending: first=%v second=%v", rows[0].TotalSpent, rows[1].TotalSpent)
	}
}

// ---------------------------------------------------------------------------
// getSpendingOverTime
// ---------------------------------------------------------------------------

func TestGetSpendingOverTimeEmpty(t *testing.T) {
	db := newTestDB(t)
	rows, err := getSpendingOverTime(db, "month", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows on empty DB, got %d", len(rows))
	}
}

func TestGetSpendingOverTimeMonthGrouping(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-10', 20.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2024-01-25', 10.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r3', '2024-02-05', 30.00)`)

	rows, err := getSpendingOverTime(db, "month", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 months, got %d", len(rows))
	}
	if rows[0].Period != "2024-01" {
		t.Errorf("first period = %q, want 2024-01", rows[0].Period)
	}
	if !approxEq(rows[0].Amount, 30.0, 1e-6) {
		t.Errorf("2024-01 amount = %v, want 30.0", rows[0].Amount)
	}
	if !approxEq(rows[1].Amount, 30.0, 1e-6) {
		t.Errorf("2024-02 amount = %v, want 30.0", rows[1].Amount)
	}
}

func TestGetSpendingOverTimeCombinesReceiptsAndOrders(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-03-05', 25.00)`)
	db.Exec(`INSERT INTO orders (order_id, delivery_date, delivery_method, delivery_status, total_price) VALUES (1, '2024-03-20', 'delivery', 'delivered', 35.00)`)

	rows, err := getSpendingOverTime(db, "month", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 period, got %d", len(rows))
	}
	if !approxEq(rows[0].Amount, 60.0, 1e-6) {
		t.Errorf("combined amount = %v, want 60.0", rows[0].Amount)
	}
}

func TestGetSpendingOverTimeYearGrouping(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2023-06-01', 100.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2024-03-01', 200.00)`)

	rows, err := getSpendingOverTime(db, "year", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 years, got %d", len(rows))
	}
	if rows[0].Period != "2023" || rows[1].Period != "2024" {
		t.Errorf("unexpected periods: %v %v", rows[0].Period, rows[1].Period)
	}
}

func TestGetSpendingOverTimeInvalidPeriodFallsBackToMonth(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-05-01', 50.00)`)

	rows, err := getSpendingOverTime(db, "invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// Should have fallen back to month format.
	if rows[0].Period != "2024-05" {
		t.Errorf("period = %q, want 2024-05 (month fallback)", rows[0].Period)
	}
}

func TestGetSpendingOverTimeSortedAscending(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-03-01', 10.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2024-01-01', 20.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r3', '2024-02-01', 30.00)`)

	rows, err := getSpendingOverTime(db, "month", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].Period > rows[1].Period || rows[1].Period > rows[2].Period {
		t.Errorf("expected ascending order, got %v %v %v", rows[0].Period, rows[1].Period, rows[2].Period)
	}
}

// ---------------------------------------------------------------------------
// shared helper
// ---------------------------------------------------------------------------
// getProductStats — since filter
// ---------------------------------------------------------------------------

func TestGetProductStatsNilSinceReturnsAll(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-10', 10.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2024-06-10', 10.00)`)
	db.Exec(`INSERT INTO products VALUES (1, 'Melk', NULL, NULL, NULL, '1L', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Melk', 2, 2.00, 1)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r2', 'Melk', 1, 1.00, 1)`)

	stats, err := getProductStats(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 product, got %d", len(stats))
	}
	// timesBought counts purchase lines, not total quantity (2 lines = 2 receipts)
	if stats[0].TimesBought != 2 {
		t.Errorf("times_bought = %d, want 2 (two receipt lines)", stats[0].TimesBought)
	}
}

func TestGetProductStatsWithSinceFiltersOldReceipts(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-10', 10.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2024-06-10', 10.00)`)
	db.Exec(`INSERT INTO products VALUES (1, 'Melk', NULL, NULL, NULL, '1L', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Melk', 2, 2.00, 1)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r2', 'Melk', 1, 1.00, 1)`)

	since, _ := time.Parse(dateISO, "2024-04-01")
	stats, err := getProductStats(db, &since)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 product, got %d", len(stats))
	}
	if stats[0].TimesBought != 1 {
		t.Errorf("times_bought = %d, want 1 (only r2 is on or after 2024-04-01)", stats[0].TimesBought)
	}
}

func TestGetProductStatsSinceExcludesAllReturnsEmpty(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-10', 10.00)`)
	db.Exec(`INSERT INTO products VALUES (1, 'Melk', NULL, NULL, NULL, '1L', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Melk', 1, 1.00, 1)`)

	since, _ := time.Parse(dateISO, "2025-01-01")
	stats, err := getProductStats(db, &since)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 0 {
		t.Errorf("expected 0 products when since is after all receipts, got %d", len(stats))
	}
}

func TestGetProductStatsSinceWithFullISOFormat(t *testing.T) {
	// Verify the filter works when receipts use the full ISO 8601 format with
	// timezone suffix that the AH API returns (e.g. "2024-06-10T10:30:00Z").
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-10T08:00:00Z', 10.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2024-06-10T10:30:00+02:00', 10.00)`)
	db.Exec(`INSERT INTO products VALUES (1, 'Melk', NULL, NULL, NULL, '1L', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Melk', 2, 2.00, 1)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r2', 'Melk', 1, 1.00, 1)`)

	since, _ := time.Parse(dateISO, "2024-04-01")
	stats, err := getProductStats(db, &since)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 product, got %d", len(stats))
	}
	if stats[0].TimesBought != 1 {
		t.Errorf("times_bought = %d, want 1 (only r2 is after 2024-04-01)", stats[0].TimesBought)
	}
}

// ---------------------------------------------------------------------------
// getNutriscoreDistribution — since filter
// ---------------------------------------------------------------------------

func TestGetNutriscoreDistributionWithSince(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-15', 10.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2024-06-15', 10.00)`)
	db.Exec(`INSERT INTO products VALUES (1, 'Melk', NULL, NULL, NULL, '1L', 'A', NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Melk', 2, 2.00, 1)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r2', 'Melk', 1, 1.00, 1)`)

	// No filter: total times_bought = 3
	all, err := getNutriscoreDistribution(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].TimesBought != 3 {
		t.Errorf("without since: want 1 entry with times_bought=3, got %v", all)
	}

	// Filter to after r1: only r2 counts
	since, _ := time.Parse(dateISO, "2024-04-01")
	filtered, err := getNutriscoreDistribution(db, &since)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].TimesBought != 1 {
		t.Errorf("with since: want 1 entry with times_bought=1, got %v", filtered)
	}
}

// ---------------------------------------------------------------------------
// getFinancialSummary — since filter
// ---------------------------------------------------------------------------

func TestGetFinancialSummaryWithSince(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-15', 30.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2024-06-15', 20.00)`)
	db.Exec(`INSERT INTO receipt_discounts (receipt_id, name, amount) VALUES ('r1', 'AH Bonus', 5.00)`)
	db.Exec(`INSERT INTO receipt_discounts (receipt_id, name, amount) VALUES ('r2', 'AH Bonus', 2.00)`)

	// No filter: total = 50, discount = 7
	all, err := getFinancialSummary(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !approxEq(all.TotalSpent, 50.0, 1e-6) {
		t.Errorf("TotalSpent (no filter) = %v, want 50.0", all.TotalSpent)
	}
	if !approxEq(all.TotalDiscount, 7.0, 1e-6) {
		t.Errorf("TotalDiscount (no filter) = %v, want 7.0", all.TotalDiscount)
	}

	// Filter to after r1: only r2 counts
	since, _ := time.Parse(dateISO, "2024-04-01")
	filtered, err := getFinancialSummary(db, &since)
	if err != nil {
		t.Fatal(err)
	}
	if !approxEq(filtered.TotalSpent, 20.0, 1e-6) {
		t.Errorf("TotalSpent (with since) = %v, want 20.0", filtered.TotalSpent)
	}
	if !approxEq(filtered.TotalDiscount, 2.0, 1e-6) {
		t.Errorf("TotalDiscount (with since) = %v, want 2.0", filtered.TotalDiscount)
	}
}

// ---------------------------------------------------------------------------
// getProducts — CO2EqPerUnit
// ---------------------------------------------------------------------------

func TestGetProductsComputesCO2PerUnit(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO products VALUES (1, 'Kipfilet', NULL, NULL, NULL, '500g', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO product_enrichment VALUES (1, 'Vlees', 'Kip', 4.0, 'keyword', NULL, NULL)`)

	products, err := getProducts(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	p := products[0]
	if p.CO2EqPerUnit == nil {
		t.Fatal("CO2EqPerUnit should not be nil")
	}
	// co2eq_per_kg=4.0, weight from "500g"=0.5 → 4.0*0.5=2.0
	if !approxEq(*p.CO2EqPerUnit, 2.0, 1e-6) {
		t.Errorf("CO2EqPerUnit = %v, want 2.0", *p.CO2EqPerUnit)
	}
}

func TestGetProductsCO2PerUnitNilWhenNoEnrichment(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO products VALUES (1, 'Iets', NULL, NULL, NULL, '500g', NULL, NULL, NULL, NULL, NULL, NULL)`)

	products, err := getProducts(db)
	if err != nil {
		t.Fatal(err)
	}
	if products[0].CO2EqPerUnit != nil {
		t.Error("CO2EqPerUnit should be nil when product has no enrichment")
	}
}

// ---------------------------------------------------------------------------
// getReceiptDetail — CO2EqPerEuro
// ---------------------------------------------------------------------------

func TestGetReceiptDetailCO2PerEuro(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-06-01', 10.00)`)
	db.Exec(`INSERT INTO products VALUES (10, 'Zalm', 'AH', 'Vis', NULL, '300g', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Zalm', 2, 10.00, 10)`)
	// co2=6.0 * 2 * 0.3 = 3.6; total_amount=10.00; co2_per_euro=0.36
	db.Exec(`INSERT INTO product_enrichment VALUES (10, 'Vis', 'Zalm', 6.0, 'keyword', NULL, NULL)`)

	detail, err := getReceiptDetail(db, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.CO2EqPerEuro == nil {
		t.Fatal("CO2EqPerEuro should not be nil")
	}
	if !approxEq(*detail.CO2EqPerEuro, 0.36, 1e-3) {
		t.Errorf("CO2EqPerEuro = %v, want 0.36", *detail.CO2EqPerEuro)
	}
}

func TestGetReceiptDetailCO2PerEuroNilWhenNoEnrichment(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-06-01', 5.00)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount) VALUES ('r1', 'Groente', 1, 5.00)`)

	detail, err := getReceiptDetail(db, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.CO2EqPerEuro != nil {
		t.Error("CO2EqPerEuro should be nil when no items are enriched")
	}
}

// ---------------------------------------------------------------------------
// getOrderDetail — LineTotal, CO2EqTotal, CO2EqPerEuro
// ---------------------------------------------------------------------------

func TestGetOrderDetailLineTotalAndCO2(t *testing.T) {
	db := newTestDB(t)
	db.Exec(`INSERT INTO orders (order_id, delivery_date, total_price) VALUES (1, '2024-06-01', 20.00)`)
	db.Exec(`INSERT INTO products VALUES (5, 'Melk', 'AH', 'Zuivel', NULL, '1 liter', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO order_items (order_id, web_id, title, quantity, allocated_qty, unit_price) VALUES (1, 5, 'Melk', 2, 2, 1.50)`)
	// co2=3.2 * 2 * 1.0 = 6.4; line_total=2*1.50=3.00
	db.Exec(`INSERT INTO product_enrichment VALUES (5, 'Zuivel', 'Melk', 3.2, 'keyword', NULL, NULL)`)

	detail, err := getOrderDetail(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(detail.Items))
	}
	it := detail.Items[0]
	if !approxEq(it.LineTotal, 3.00, 1e-6) {
		t.Errorf("LineTotal = %v, want 3.00", it.LineTotal)
	}
	if detail.CO2EqTotal == nil {
		t.Fatal("CO2EqTotal should not be nil")
	}
	if !approxEq(*detail.CO2EqTotal, 6.4, 1e-3) {
		t.Errorf("CO2EqTotal = %v, want 6.4", *detail.CO2EqTotal)
	}
	if detail.CO2EqPerEuro == nil {
		t.Fatal("CO2EqPerEuro should not be nil")
	}
	// 6.4 / 3.00 ≈ 2.133
	if !approxEq(*detail.CO2EqPerEuro, 6.4/3.0, 1e-3) {
		t.Errorf("CO2EqPerEuro = %v, want %.3f", *detail.CO2EqPerEuro, 6.4/3.0)
	}
}

// ---------------------------------------------------------------------------
// getSustainabilitySummary
// ---------------------------------------------------------------------------

func seedSustainabilityDB(t *testing.T) *sql.DB {
	t.Helper()
	db := newTestDB(t)
	// Two months of data: 2024-01 and 2024-02
	db.Exec(`INSERT INTO receipts VALUES ('r1', '2024-01-10', 20.00)`)
	db.Exec(`INSERT INTO receipts VALUES ('r2', '2024-02-15', 30.00)`)
	db.Exec(`INSERT INTO products VALUES (1, 'Kipfilet', 'AH', 'Vlees', NULL, '500g', NULL, NULL, NULL, NULL, NULL, NULL)`)
	db.Exec(`INSERT INTO products VALUES (2, 'Melk', 'AH', 'Zuivel', NULL, '1 liter', NULL, NULL, NULL, NULL, NULL, NULL)`)
	// r1: Kip 2x500g at co2=4.0 → 4.0*2*0.5=4.0; Melk 1x1L at co2=3.2 → 3.2
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Kipfilet', 2, 6.00, 1)`)
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'Melk', 1, 1.50, 2)`)
	// r2: Kip 4x500g → 4.0*4*0.5=8.0
	db.Exec(`INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r2', 'Kipfilet', 4, 12.00, 1)`)
	db.Exec(`INSERT INTO product_enrichment VALUES (1, 'Vlees', 'Kip', 4.0, 'keyword', NULL, NULL)`)
	db.Exec(`INSERT INTO product_enrichment VALUES (2, 'Zuivel', 'Melk', 3.2, 'keyword', NULL, NULL)`)
	return db
}

func TestGetSustainabilitySummaryEmpty(t *testing.T) {
	db := newTestDB(t)
	s, err := getSustainabilitySummary(db, nil, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Grade != "" {
		t.Errorf("expected empty grade on empty DB, got %q", s.Grade)
	}
}

func TestGetSustainabilitySummaryGradeAndTopCategory(t *testing.T) {
	db := seedSustainabilityDB(t)
	// 2024-01: 4.0+3.2=7.2; 2024-02: 8.0 → monthly avg = (7.2+8.0)/2 = 7.6 per AE
	// 7.6 << 42 (sustainable target) → grade A
	s, err := getSustainabilitySummary(db, nil, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Grade != "A" {
		t.Errorf("Grade = %q, want A (avg well below sustainable target)", s.Grade)
	}
	if s.TopCategory != "Vlees" {
		t.Errorf("TopCategory = %q, want Vlees (most CO₂)", s.TopCategory)
	}
	if s.PctAboveSustainable == nil {
		t.Fatal("PctAboveSustainable should not be nil")
	}
}

func TestGetSustainabilitySummaryHouseholdAEScales(t *testing.T) {
	db := seedSustainabilityDB(t)
	s1, _ := getSustainabilitySummary(db, nil, 1.0)
	s2, _ := getSustainabilitySummary(db, nil, 2.0)
	// higher AE → lower per-AE average → should be ≤ s1 average
	if s1.AvgKgPerAePerMonth == nil || s2.AvgKgPerAePerMonth == nil {
		t.Fatal("AvgKgPerAePerMonth should not be nil")
	}
	if *s2.AvgKgPerAePerMonth >= *s1.AvgKgPerAePerMonth {
		t.Errorf("higher AE should lower per-AE avg: ae1=%v ae2=%v", *s1.AvgKgPerAePerMonth, *s2.AvgKgPerAePerMonth)
	}
}

func TestGetSustainabilitySummarySinceFilter(t *testing.T) {
	db := seedSustainabilityDB(t)
	// Without filter: 2 months
	all, _ := getSustainabilitySummary(db, nil, 1.0)
	// With since=2024-02-01: only February data
	since, _ := time.Parse(dateISO, "2024-02-01")
	filtered, err := getSustainabilitySummary(db, &since, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	// Filtered avg should reflect only Feb data (8.0 kg), all-time avg is (7.2+8.0)/2=7.6
	if all.AvgKgPerAePerMonth == nil || filtered.AvgKgPerAePerMonth == nil {
		t.Fatal("averages should not be nil")
	}
	// Feb only: avg=8.0; all: avg=7.6 → filtered should be > all-time
	if *filtered.AvgKgPerAePerMonth <= *all.AvgKgPerAePerMonth {
		t.Errorf("filtered avg (%v) should be > all-time avg (%v)", *filtered.AvgKgPerAePerMonth, *all.AvgKgPerAePerMonth)
	}
}

// ---------------------------------------------------------------------------
// getSustainabilityTrend
// ---------------------------------------------------------------------------

func TestGetSustainabilityTrendEmpty(t *testing.T) {
	db := newTestDB(t)
	trend, err := getSustainabilityTrend(db, "month", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(trend) != 0 {
		t.Errorf("expected 0 entries on empty DB, got %d", len(trend))
	}
}

func TestGetSustainabilityTrendMonthGrouping(t *testing.T) {
	db := seedSustainabilityDB(t)
	trend, err := getSustainabilityTrend(db, "month", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Expect entries for 2024-01 and 2024-02, categories Vlees and Zuivel
	byKey := make(map[string]float64)
	for _, e := range trend {
		byKey[e.Period+"/"+e.Category] = e.CO2Eq
	}
	if !approxEq(byKey["2024-01/Vlees"], 4.0, 1e-3) {
		t.Errorf("2024-01/Vlees = %v, want 4.0", byKey["2024-01/Vlees"])
	}
	if !approxEq(byKey["2024-01/Zuivel"], 3.2, 1e-3) {
		t.Errorf("2024-01/Zuivel = %v, want 3.2", byKey["2024-01/Zuivel"])
	}
	if !approxEq(byKey["2024-02/Vlees"], 8.0, 1e-3) {
		t.Errorf("2024-02/Vlees = %v, want 8.0", byKey["2024-02/Vlees"])
	}
}

func TestGetSustainabilityTrendSortedByPeriod(t *testing.T) {
	db := seedSustainabilityDB(t)
	trend, err := getSustainabilityTrend(db, "month", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(trend); i++ {
		if trend[i-1].Period > trend[i].Period {
			t.Errorf("not sorted: %q > %q", trend[i-1].Period, trend[i].Period)
		}
	}
}

func TestGetSustainabilityTrendYearGrouping(t *testing.T) {
	db := seedSustainabilityDB(t)
	trend, err := getSustainabilityTrend(db, "year", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range trend {
		if e.Period != "2024" {
			t.Errorf("year period = %q, want 2024", e.Period)
		}
	}
}

// ---------------------------------------------------------------------------
// getSustainabilityCategories
// ---------------------------------------------------------------------------

func TestGetSustainabilityCategoriesEmpty(t *testing.T) {
	db := newTestDB(t)
	cats, err := getSustainabilityCategories(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 0 {
		t.Errorf("expected 0 categories on empty DB, got %d", len(cats))
	}
}

func TestGetSustainabilityCategoriesSortedDesc(t *testing.T) {
	db := seedSustainabilityDB(t)
	cats, err := getSustainabilityCategories(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) < 2 {
		t.Fatalf("expected at least 2 categories, got %d", len(cats))
	}
	// Vlees total = 4.0+8.0=12.0; Zuivel total = 3.2 → Vlees first
	if cats[0].Category != "Vlees" {
		t.Errorf("first category = %q, want Vlees (highest CO₂)", cats[0].Category)
	}
	for i := 1; i < len(cats); i++ {
		if cats[i-1].CO2Eq < cats[i].CO2Eq {
			t.Errorf("not sorted descending at index %d", i)
		}
	}
}

// ---------------------------------------------------------------------------
// getCategoryProducts
// ---------------------------------------------------------------------------

func TestGetCategoryProductsAllTime(t *testing.T) {
	db := seedSustainabilityDB(t)
	products, err := getCategoryProducts(db, "Vlees", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// r1: 1 Kip row; r2: 1 Kip row → 2 rows
	if len(products) != 2 {
		t.Fatalf("expected 2 product rows, got %d", len(products))
	}
	var totalPct float64
	for _, p := range products {
		totalPct += p.PctOfCategory
	}
	if !approxEq(totalPct, 100.0, 1e-3) {
		t.Errorf("percentages sum = %v, want 100", totalPct)
	}
}

func TestGetCategoryProductsPeriodFilter(t *testing.T) {
	db := seedSustainabilityDB(t)
	// January only: should return 1 Kip row from r1
	products, err := getCategoryProducts(db, "Vlees", "month", "2024-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 row for 2024-01, got %d", len(products))
	}
	if !approxEq(products[0].PctOfCategory, 100.0, 1e-3) {
		t.Errorf("single item should have 100%% of category, got %v", products[0].PctOfCategory)
	}
}

func TestGetCategoryProductsSortedByCO2Desc(t *testing.T) {
	db := seedSustainabilityDB(t)
	products, err := getCategoryProducts(db, "Vlees", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(products); i++ {
		if products[i-1].CO2EqTotal < products[i].CO2EqTotal {
			t.Errorf("not sorted descending at index %d", i)
		}
	}
}

func TestGetCategoryProductsEmpty(t *testing.T) {
	db := newTestDB(t)
	products, err := getCategoryProducts(db, "Vlees", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 0 {
		t.Errorf("expected 0 products on empty DB, got %d", len(products))
	}
}

// ---------------------------------------------------------------------------
// toPeriodLabel
// ---------------------------------------------------------------------------

func TestToPeriodLabelMonth(t *testing.T) {
	tt, _ := time.Parse("2006-01-02", "2024-01-15")
	if got := toPeriodLabel(tt, "month"); got != "2024-01" {
		t.Errorf("month = %q, want 2024-01", got)
	}
}

func TestToPeriodLabelYear(t *testing.T) {
	tt, _ := time.Parse("2006-01-02", "2024-06-01")
	if got := toPeriodLabel(tt, "year"); got != "2024" {
		t.Errorf("year = %q, want 2024", got)
	}
}

func TestToPeriodLabelQuarter(t *testing.T) {
	cases := []struct{ date, want string }{
		{"2024-01-15", "2024Q1"},
		{"2024-04-01", "2024Q2"},
		{"2024-07-31", "2024Q3"},
		{"2024-10-01", "2024Q4"},
	}
	for _, tc := range cases {
		tt, _ := time.Parse("2006-01-02", tc.date)
		if got := toPeriodLabel(tt, "quarter"); got != tc.want {
			t.Errorf("quarter(%s) = %q, want %q", tc.date, got, tc.want)
		}
	}
}

func TestToPeriodLabelWeekIsMonday(t *testing.T) {
	// 2024-01-15 is a Monday → week label = itself
	tt, _ := time.Parse("2006-01-02", "2024-01-15")
	if got := toPeriodLabel(tt, "week"); got != "2024-01-15" {
		t.Errorf("week(Monday) = %q, want 2024-01-15", got)
	}
	// 2024-01-17 is a Wednesday → Monday = 2024-01-15
	tt2, _ := time.Parse("2006-01-02", "2024-01-17")
	if got := toPeriodLabel(tt2, "week"); got != "2024-01-15" {
		t.Errorf("week(Wednesday) = %q, want 2024-01-15", got)
	}
	// 2024-01-21 is a Sunday → Monday = 2024-01-15
	tt3, _ := time.Parse("2006-01-02", "2024-01-21")
	if got := toPeriodLabel(tt3, "week"); got != "2024-01-15" {
		t.Errorf("week(Sunday) = %q, want 2024-01-15", got)
	}
}

// ---------------------------------------------------------------------------

func approxEq(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol*b+1e-9
}
