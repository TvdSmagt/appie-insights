package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"appie-insights/backend/config"
	"appie-insights/backend/store"
	"appie-insights/backend/syncer"
	"appie-insights/backend/worker"
)

func newTestHandlerMux(t *testing.T) (mux *http.ServeMux, enrichReqCh chan worker.Request, syncReqCh chan struct{}, st *worker.Status, syncSt *syncer.Status) {
	t.Helper()
	mux = http.NewServeMux()
	enrichReqCh = make(chan worker.Request, 1)
	syncReqCh = make(chan struct{}, 1)
	st = worker.New(dbPath, dataDir).EnrichStatus()
	syncSt = syncer.NewStatus()
	cfg := config.Server{DBPath: dbPath, ConfigPath: configPath, DataDir: dataDir}
	registerHandlers(mux, cfg, enrichReqCh, st, syncReqCh, syncSt)
	return
}

// ---------------------------------------------------------------------------
// POST /enrich
// ---------------------------------------------------------------------------

func TestHandlerPostEnrich(t *testing.T) {
	mux, enrichReqCh, _, _, _ := newTestHandlerMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/enrich", strings.NewReader(`{}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
	select {
	case <-enrichReqCh:
	default:
		t.Error("expected a request on the enrich channel")
	}
}

func TestHandlerPostEnrichWithReceiptID(t *testing.T) {
	mux, enrichReqCh, _, _, _ := newTestHandlerMux(t)
	req := httptest.NewRequest("POST", "/enrich", strings.NewReader(`{"receipt_id":"TRX-001"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	select {
	case r := <-enrichReqCh:
		if r.ReceiptID != "TRX-001" {
			t.Errorf("want ReceiptID=TRX-001, got %q", r.ReceiptID)
		}
	default:
		t.Error("expected a request on the enrich channel")
	}
}

// ---------------------------------------------------------------------------
// GET /status
// ---------------------------------------------------------------------------

func TestHandlerGetStatus(t *testing.T) {
	mux, _, _, _, _ := newTestHandlerMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "idle" {
		t.Errorf("expected status=idle, got %v", body["status"])
	}
	if _, ok := body["items_total"]; !ok {
		t.Error("missing items_total")
	}
	if _, ok := body["items_processed"]; !ok {
		t.Error("missing items_processed")
	}
}

// ---------------------------------------------------------------------------
// GET /categories
// ---------------------------------------------------------------------------

func TestHandlerGetCategories(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "co2eq_categories.csv"),
		[]byte("name,category,subcategory,co2eq_per_kg,source,notes\nMelk,dairy,milk,3.2,test,\n"),
		0600,
	); err != nil {
		t.Fatal(err)
	}
	old := dataDir
	dataDir = dir
	t.Cleanup(func() { dataDir = old })

	mux, _, _, _, _ := newTestHandlerMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/categories", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 {
		t.Fatalf("want 1 category, got %d", len(body))
	}
	if body[0]["name"] != "Melk" {
		t.Errorf("unexpected name: %v", body[0]["name"])
	}
}

// ---------------------------------------------------------------------------
// GET /corrections fallback behaviour
//
// With no corrections.csv on disk, the backend falls back to the curated
// corrections baked into the binary (see embed.go / enricher.EmbeddedData), so
// a self-contained build ships sensible defaults. An empty file on disk still
// overrides that fallback (disk takes precedence over embedded).
// ---------------------------------------------------------------------------

func TestHandlerGetCorrectionsFallsBackToEmbedded(t *testing.T) {
	old := dataDir
	dataDir = t.TempDir() // no corrections.csv here → embedded baseline applies
	t.Cleanup(func() { dataDir = old })

	mux, _, _, _, _ := newTestHandlerMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/corrections", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body []any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Error("expected embedded corrections baseline, got empty")
	}
}

func TestHandlerGetCorrectionsDiskOverridesEmbedded(t *testing.T) {
	dir := t.TempDir()
	old := dataDir
	dataDir = dir
	t.Cleanup(func() { dataDir = old })

	// A header-only corrections.csv on disk represents "user has no
	// corrections" and must override the embedded baseline.
	if err := os.WriteFile(
		filepath.Join(dir, "corrections.csv"),
		[]byte("web_id,action,co2eq_category,co2eq_name,co2eq_per_kg,weight_kg,notes\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	mux, _, _, _, _ := newTestHandlerMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/corrections", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body []any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 0 {
		t.Errorf("expected empty corrections (disk overrides embedded), got %d", len(body))
	}
}

// ---------------------------------------------------------------------------
// POST /corrections + GET /corrections roundtrip
// ---------------------------------------------------------------------------

func TestHandlerCorrectionsRoundtrip(t *testing.T) {
	dir := t.TempDir()
	old := dataDir
	dataDir = dir
	t.Cleanup(func() { dataDir = old })

	mux, _, _, _, _ := newTestHandlerMux(t)

	payload := `[{"web_id":456,"action":"set_category","co2eq_category":"dairy","co2eq_name":"Melk","notes":"test"}]`
	req := httptest.NewRequest("POST", "/corrections", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /corrections status %d: %s", rec.Code, rec.Body.String())
	}
	var postBody map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&postBody); err != nil {
		t.Fatal(err)
	}
	if postBody["ok"] != true {
		t.Errorf("expected ok=true, got %v", postBody["ok"])
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/corrections", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /corrections status %d", rec.Code)
	}
	var getBody []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&getBody); err != nil {
		t.Fatal(err)
	}
	if len(getBody) != 1 {
		t.Fatalf("want 1 correction, got %d", len(getBody))
	}
	if getBody[0]["web_id"] != float64(456) {
		t.Errorf("unexpected web_id: %v", getBody[0]["web_id"])
	}
}

// ---------------------------------------------------------------------------
// POST /sync
// ---------------------------------------------------------------------------

func TestHandlerPostSync(t *testing.T) {
	mux, _, syncReqCh, _, _ := newTestHandlerMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/sync", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
	select {
	case <-syncReqCh:
	default:
		t.Error("expected a request on the sync channel")
	}
}

// ---------------------------------------------------------------------------
// GET /sync/status
// ---------------------------------------------------------------------------

func TestHandlerGetSyncStatus(t *testing.T) {
	mux, _, _, _, _ := newTestHandlerMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/sync/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["status"]; !ok {
		t.Error("missing status field")
	}
	if _, ok := body["receipts_found"]; !ok {
		t.Error("missing receipts_found field")
	}
}

// ---------------------------------------------------------------------------
// POST /products/{web_id}/fetch
// ---------------------------------------------------------------------------

func TestHandlerFetchProduct_NoCredentials(t *testing.T) {
	t.Setenv("AH_ACCESS_TOKEN", "")
	t.Setenv("AH_REFRESH_TOKEN", "")
	// NewWithConfig only errors on a malformed (not missing) config file
	cfgFile := filepath.Join(t.TempDir(), "appie.json")
	if err := os.WriteFile(cfgFile, []byte("invalid json"), 0600); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	registerProductHandlers(mux, config.Server{DBPath: dbPath, ConfigPath: cfgFile})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/products/456/fetch", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /products/link-pos
// ---------------------------------------------------------------------------

func TestHandlerLinkPos_BadRequest(t *testing.T) {
	mux := http.NewServeMux()
	registerProductHandlers(mux, config.Server{DBPath: dbPath, ConfigPath: configPath})

	rec := httptest.NewRecorder()
	// Missing pos_id and web_id → 400
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/products/link-pos", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func newLinkPosTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "linkpos.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := initDB(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO receipts (transaction_id, date, total_amount) VALUES ('TRX', '2026-01-01', 0)",
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func itemWebID(t *testing.T, db *sql.DB, posID int) (int64, bool) {
	t.Helper()
	var w sql.NullInt64
	if err := db.QueryRow(
		"SELECT web_id FROM items WHERE product_id = ? LIMIT 1", posID,
	).Scan(&w); err != nil {
		t.Fatal(err)
	}
	return w.Int64, w.Valid
}

// A POS item with no web_id yet gets linked to the given web_id.
func TestLinkPosToWebID_FreshLink(t *testing.T) {
	db := newLinkPosTestDB(t)
	if _, err := db.Exec(
		"INSERT INTO items (receipt_id, description, quantity, amount, product_id, web_id) VALUES ('TRX', 'X', 1, 1, 100, NULL)",
	); err != nil {
		t.Fatal(err)
	}

	updated, err := linkPosToWebID(db, 100, 555)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("want 1 updated item, got %d", updated)
	}
	if w, ok := itemWebID(t, db, 100); !ok || w != 555 {
		t.Fatalf("item web_id = %d (valid=%v), want 555", w, ok)
	}
}

// A POS item that already has a stale web_id is re-pointed to a new web_id.
// This is the relink-over-existing case that previously did nothing because
// the UPDATE was restricted to web_id IS NULL rows.
func TestLinkPosToWebID_RelinkOverwritesStale(t *testing.T) {
	db := newLinkPosTestDB(t)
	if _, err := db.Exec(
		"INSERT INTO items (receipt_id, description, quantity, amount, product_id, web_id) VALUES ('TRX', 'X', 1, 1, 100, 111)",
	); err != nil {
		t.Fatal(err)
	}
	// Stale product row claiming the POS id.
	if _, err := db.Exec(
		"INSERT INTO products (web_id, pos_id) VALUES (111, 100)",
	); err != nil {
		t.Fatal(err)
	}

	updated, err := linkPosToWebID(db, 100, 222)
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("want 1 updated item, got %d", updated)
	}
	if w, ok := itemWebID(t, db, 100); !ok || w != 222 {
		t.Fatalf("item web_id = %d (valid=%v), want 222", w, ok)
	}

	// The stale product row must no longer claim this POS id.
	var stalePos sql.NullInt64
	if err := db.QueryRow("SELECT pos_id FROM products WHERE web_id = 111").Scan(&stalePos); err != nil {
		t.Fatal(err)
	}
	if stalePos.Valid {
		t.Fatalf("stale product still claims pos_id %d", stalePos.Int64)
	}
	// The new product row owns the POS id.
	var newPos sql.NullInt64
	if err := db.QueryRow("SELECT pos_id FROM products WHERE web_id = 222").Scan(&newPos); err != nil {
		t.Fatal(err)
	}
	if !newPos.Valid || newPos.Int64 != 100 {
		t.Fatalf("new product pos_id = %d (valid=%v), want 100", newPos.Int64, newPos.Valid)
	}
}
