package syncer

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"testing"
	"time"

	appie "github.com/gwillem/appie-go"
	_ "modernc.org/sqlite"

	"appie-insights/backend/schema"
)

// openTestDB returns an in-memory SQLite DB built from the production schema, so
// that any column the syncer references but the real schema lacks fails here
// (in `make test-fast`) instead of only against the live database during sync.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(schema.DDL); err != nil {
		t.Fatal(err)
	}
	return db
}

// mockProductResponse is a minimal JSON-serialisable replica of appie.productResponse.
type mockProductResponse struct {
	WebshopID     int    `json:"webshopId"`
	Title         string `json:"title"`
	Brand         string `json:"brand"`
	MainCategory  string `json:"mainCategory"`
	SubCategory   string `json:"subCategory"`
	NutriScore    string `json:"nutriscore"`
	SalesUnitSize string `json:"salesUnitSize"`
}

// productJSONFor returns a JSON array containing one entry per given web ID.
func productJSONFor(ids ...int) []byte {
	products := make([]mockProductResponse, len(ids))
	for i, id := range ids {
		products[i] = mockProductResponse{
			WebshopID:     id,
			Title:         "Product " + strconv.Itoa(id),
			Brand:         "Brand",
			MainCategory:  "Zuivel",
			SubCategory:   "Melk",
			NutriScore:    "A",
			SalesUnitSize: "500 g",
		}
	}
	b, _ := json.Marshal(products)
	return b
}

// newTestClient returns an appie.Client pointed at the given test server URL.
func newTestClient(t *testing.T, srvURL string) *appie.Client {
	t.Helper()
	return appie.New(appie.WithBaseURL(srvURL), appie.WithTokens("test", "test"))
}

// requestedIDs parses the `ids` query params from a GET request.
func requestedIDs(r *http.Request) []int {
	var ids []int
	for _, s := range r.URL.Query()["ids"] {
		if id, err := strconv.Atoi(s); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// ---------------------------------------------------------------------------
// fetchAndStoreBatch
// ---------------------------------------------------------------------------

func TestFetchAndStoreBatch_StoresProducts(t *testing.T) {
	db := openTestDB(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(productJSONFor(10, 20, 30))
	}))
	defer srv.Close()

	if err := FetchAndStoreBatch(context.Background(), newTestClient(t, srv.URL), db, []int{10, 20, 30}, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM products WHERE title IS NOT NULL").Scan(&count)
	if count != 3 {
		t.Errorf("want 3 products stored, got %d", count)
	}

	var title string
	db.QueryRow("SELECT title FROM products WHERE web_id = 20").Scan(&title)
	if title != "Product 20" {
		t.Errorf("title = %q, want \"Product 20\"", title)
	}
}

func TestFetchAndStoreBatch_StoresAllFields(t *testing.T) {
	db := openTestDB(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(productJSONFor(42))
	}))
	defer srv.Close()

	if err := FetchAndStoreBatch(context.Background(), newTestClient(t, srv.URL), db, []int{42}, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	var brand, category, subcategory, nutriscore, unitSize string
	db.QueryRow(`SELECT brand, ah_category, ah_subcategory, nutriscore, unit_size
		FROM products WHERE web_id = 42`).Scan(&brand, &category, &subcategory, &nutriscore, &unitSize)

	if brand != "Brand" {
		t.Errorf("brand = %q, want \"Brand\"", brand)
	}
	if category != "Zuivel" {
		t.Errorf("ah_category = %q, want \"Zuivel\"", category)
	}
	if subcategory != "Melk" {
		t.Errorf("ah_subcategory = %q, want \"Melk\"", subcategory)
	}
	if nutriscore != "A" {
		t.Errorf("nutriscore = %q, want \"A\"", nutriscore)
	}
	if unitSize != "500 g" {
		t.Errorf("unit_size = %q, want \"500 g\"", unitSize)
	}
}

func TestFetchAndStoreBatch_RecordsNotFound(t *testing.T) {
	db := openTestDB(t)

	// Return only IDs 10 and 30; 20 is absent (product deleted / never existed).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(productJSONFor(10, 30))
	}))
	defer srv.Close()

	if err := FetchAndStoreBatch(context.Background(), newTestClient(t, srv.URL), db, []int{10, 20, 30}, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	var notFoundCount int
	db.QueryRow("SELECT COUNT(*) FROM product_not_found").Scan(&notFoundCount)
	if notFoundCount != 1 {
		t.Fatalf("want 1 product_not_found row, got %d", notFoundCount)
	}

	var wid int
	db.QueryRow("SELECT web_id FROM product_not_found").Scan(&wid)
	if wid != 20 {
		t.Errorf("product_not_found web_id = %d, want 20", wid)
	}
}

func TestFetchAndStoreBatch_NotFoundDoesNotPreventOtherStores(t *testing.T) {
	db := openTestDB(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(productJSONFor(10, 30)) // 20 missing
	}))
	defer srv.Close()

	if err := FetchAndStoreBatch(context.Background(), newTestClient(t, srv.URL), db, []int{10, 20, 30}, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	var stored int
	db.QueryRow("SELECT COUNT(*) FROM products WHERE title IS NOT NULL").Scan(&stored)
	if stored != 2 {
		t.Errorf("want 2 products stored, got %d", stored)
	}
}

// ---------------------------------------------------------------------------
// backfillMappedProductDetails
// ---------------------------------------------------------------------------

func TestBackfill_SkipsProductsAlreadyWithTitle(t *testing.T) {
	db := openTestDB(t)
	db.Exec("INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'item', 1, 1.0, 10)")
	db.Exec("INSERT INTO products (web_id, title) VALUES (10, 'Already here')")

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	if err := backfillMappedProductDetails(context.Background(), newTestClient(t, srv.URL), db, 0, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("expected no API calls for already-fetched products, got %d", calls)
	}
}

func TestBackfill_SkipsProductsInNotFoundTable(t *testing.T) {
	db := openTestDB(t)
	db.Exec("INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'item', 1, 1.0, 99)")
	db.Exec("INSERT INTO product_not_found (web_id) VALUES (99)")

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	if err := backfillMappedProductDetails(context.Background(), newTestClient(t, srv.URL), db, 0, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("expected no API calls for product_not_found entries, got %d", calls)
	}
}

func TestBackfill_IncludesWebIDsFromOrderItems(t *testing.T) {
	db := openTestDB(t)
	db.Exec("INSERT INTO order_items (order_id, web_id, title, quantity, allocated_qty) VALUES (1, 42, 'x', 1, 1)")

	var requestedWebIDs []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedWebIDs = requestedIDs(r)
		w.Header().Set("Content-Type", "application/json")
		w.Write(productJSONFor(requestedWebIDs...))
	}))
	defer srv.Close()

	if err := backfillMappedProductDetails(context.Background(), newTestClient(t, srv.URL), db, 0, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if len(requestedWebIDs) != 1 || requestedWebIDs[0] != 42 {
		t.Errorf("requestedIDs = %v, want [42]", requestedWebIDs)
	}
}

func TestBackfill_BatchesByProductBatchSize(t *testing.T) {
	db := openTestDB(t)

	// 60 items → expect 2 batches: productBatchSize + 10.
	for i := 1; i <= 60; i++ {
		db.Exec("INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', ?, 1, 1.0, ?)",
			"item"+strconv.Itoa(i), i)
	}

	var batchSizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := requestedIDs(r)
		sort.Ints(ids)
		batchSizes = append(batchSizes, len(ids))
		w.Header().Set("Content-Type", "application/json")
		w.Write(productJSONFor(ids...))
	}))
	defer srv.Close()

	if err := backfillMappedProductDetails(context.Background(), newTestClient(t, srv.URL), db, 0, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	if len(batchSizes) != 2 {
		t.Fatalf("want 2 batches, got %d: %v", len(batchSizes), batchSizes)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(batchSizes))) // largest first
	if batchSizes[0] != productBatchSize {
		t.Errorf("largest batch = %d, want %d", batchSizes[0], productBatchSize)
	}
	if batchSizes[1] != 10 {
		t.Errorf("remainder batch = %d, want 10", batchSizes[1])
	}
}

func TestBackfill_ExactlyOneBatchWhenBelowLimit(t *testing.T) {
	db := openTestDB(t)
	for i := 1; i <= 3; i++ {
		db.Exec("INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', ?, 1, 1.0, ?)",
			"item"+strconv.Itoa(i), i)
	}

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		ids := requestedIDs(r)
		w.Header().Set("Content-Type", "application/json")
		w.Write(productJSONFor(ids...))
	}))
	defer srv.Close()

	if err := backfillMappedProductDetails(context.Background(), newTestClient(t, srv.URL), db, 0, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("want 1 API call for 3 products, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// backfillMissingNutriscores
// ---------------------------------------------------------------------------

func TestBackfillNutriscores_FetchesProductsWithNullNutriscore(t *testing.T) {
	db := openTestDB(t)
	// Product with title but NULL nutriscore — should be re-fetched.
	db.Exec("INSERT INTO products (web_id, title, nutriscore) VALUES (1, 'Milk', NULL)")

	var requestedWebIDs []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedWebIDs = requestedIDs(r)
		w.Header().Set("Content-Type", "application/json")
		w.Write(productJSONFor(requestedWebIDs...)) // returns nutriscore "A"
	}))
	defer srv.Close()

	if err := backfillMissingNutriscores(context.Background(), newTestClient(t, srv.URL), db, 0, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	if len(requestedWebIDs) != 1 || requestedWebIDs[0] != 1 {
		t.Errorf("requestedWebIDs = %v, want [1]", requestedWebIDs)
	}
	var nutriscore string
	db.QueryRow("SELECT nutriscore FROM products WHERE web_id = 1").Scan(&nutriscore)
	if nutriscore != "A" {
		t.Errorf("nutriscore = %q after backfill, want \"A\"", nutriscore)
	}
}

func TestBackfillNutriscores_SkipsRecentlyCheckedProducts(t *testing.T) {
	db := openTestDB(t)
	// Products with nutriscore and a recent check timestamp — should not be re-fetched.
	db.Exec("INSERT INTO products (web_id, title, nutriscore, nutriscore_checked_at) VALUES (1, 'Milk', 'B', datetime('now'))")
	db.Exec("INSERT INTO products (web_id, title, nutriscore, nutriscore_checked_at) VALUES (2, 'Juice', '', datetime('now'))")

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	if err := backfillMissingNutriscores(context.Background(), newTestClient(t, srv.URL), db, 0, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("expected no API calls for recently-checked products, got %d", calls)
	}
}

func TestBackfillNutriscores_RechecksStaledProducts(t *testing.T) {
	db := openTestDB(t)
	// Product with empty nutriscore checked more than 30 days ago — should be re-fetched.
	db.Exec("INSERT INTO products (web_id, title, nutriscore, nutriscore_checked_at) VALUES (3, 'Crackers', '', datetime('now', '-31 days'))")

	var requestedWebIDs []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedWebIDs = requestedIDs(r)
		w.Header().Set("Content-Type", "application/json")
		w.Write(productJSONFor(requestedWebIDs...))
	}))
	defer srv.Close()

	if err := backfillMissingNutriscores(context.Background(), newTestClient(t, srv.URL), db, 0, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if len(requestedWebIDs) != 1 || requestedWebIDs[0] != 3 {
		t.Errorf("requestedWebIDs = %v, want [3]", requestedWebIDs)
	}
}

func TestBackfillNutriscores_SkipsProductsInNotFoundTable(t *testing.T) {
	db := openTestDB(t)
	db.Exec("INSERT INTO products (web_id, title, nutriscore) VALUES (5, 'Gone', NULL)")
	db.Exec("INSERT INTO product_not_found (web_id) VALUES (5)")

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	if err := backfillMissingNutriscores(context.Background(), newTestClient(t, srv.URL), db, 0, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Errorf("expected no API calls for product_not_found entries, got %d", calls)
	}
}

func TestUpsertProductMetadata_BackfillsNutriscoreWhenNull(t *testing.T) {
	db := openTestDB(t)
	// Pre-existing product with title but NULL nutriscore.
	db.Exec("INSERT INTO products (web_id, title, nutriscore) VALUES (10, 'Cookies', NULL)")

	product := &appie.Product{
		ID:         10,
		Title:      "Cookies",
		NutriScore: "C",
	}
	iconsJSON, _ := json.Marshal([]string{})

	if err := upsertProductMetadata(context.Background(), db, 10, product, iconsJSON, ""); err != nil {
		t.Fatal(err)
	}

	var nutriscore string
	db.QueryRow("SELECT nutriscore FROM products WHERE web_id = 10").Scan(&nutriscore)
	if nutriscore != "C" {
		t.Errorf("nutriscore = %q, want \"C\"", nutriscore)
	}
}

func TestBackfill_DeduplicatesAcrossItemsAndOrderItems(t *testing.T) {
	db := openTestDB(t)
	// web_id 7 appears in both items and order_items — should only be fetched once.
	db.Exec("INSERT INTO items (receipt_id, description, quantity, amount, web_id) VALUES ('r1', 'item', 1, 1.0, 7)")
	db.Exec("INSERT INTO order_items (order_id, web_id, title, quantity, allocated_qty) VALUES (1, 7, 'x', 1, 1)")

	var totalRequested int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := requestedIDs(r)
		totalRequested += len(ids)
		w.Header().Set("Content-Type", "application/json")
		w.Write(productJSONFor(ids...))
	}))
	defer srv.Close()

	if err := backfillMappedProductDetails(context.Background(), newTestClient(t, srv.URL), db, 0, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if totalRequested != 1 {
		t.Errorf("web_id 7 requested %d time(s), want 1", totalRequested)
	}
}
