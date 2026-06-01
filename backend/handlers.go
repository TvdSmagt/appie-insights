package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"appie-insights/backend/config"
	"appie-insights/backend/store"
	"appie-insights/backend/syncer"
)

const (
	sqlProductTitle   = "SELECT title FROM products WHERE web_id = ? AND title IS NOT NULL AND TRIM(title) != ''"
	sqlDeleteNotFound = "DELETE FROM product_not_found WHERE web_id = ?"
	logDeleteNotFound = "delete product_not_found"
)

func registerProductHandlers(mux *http.ServeMux, cfg config.Server) {
	mux.HandleFunc("POST /products/{web_id}/fetch", func(w http.ResponseWriter, r *http.Request) {
		fetchProductHandler(w, r, cfg)
	})
	mux.HandleFunc("POST /products/link-pos", func(w http.ResponseWriter, r *http.Request) {
		linkPosHandler(w, r, cfg)
	})
}

func fetchProductHandler(w http.ResponseWriter, r *http.Request, cfg config.Server) {
	webID, err := strconv.Atoi(r.PathValue("web_id"))
	if err != nil {
		http.Error(w, "invalid web_id", http.StatusBadRequest)
		return
	}
	client, err := syncer.InitClient(cfg.ConfigPath)
	if err != nil {
		http.Error(w, "no AH credentials: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	if _, err := db.Exec(sqlDeleteNotFound, webID); err != nil {
		slog.Warn(logDeleteNotFound, "web_id", webID, "err", err)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := syncer.FetchAndStoreBatch(ctx, client, db, []int{webID}, 15*time.Second); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	title := productTitleFromDB(db, webID)
	writeJSON(w, map[string]any{"found": title != nil, "title": title})
}

func linkPosHandler(w http.ResponseWriter, r *http.Request, cfg config.Server) {
	var body struct {
		PosID int `json:"pos_id"`
		WebID int `json:"web_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PosID == 0 || body.WebID == 0 {
		http.Error(w, "body must contain pos_id and web_id", http.StatusBadRequest)
		return
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	updated, err := linkPosToWebID(db, body.PosID, body.WebID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	title := maybeFetchProduct(r.Context(), db, body.WebID, cfg.ConfigPath)
	writeJSON(w, map[string]any{"updated_items": updated, "product_title": title})
}

func linkPosToWebID(db *sql.DB, posID, webID int) (int64, error) {
	// Relink every item for this POS product to the new web ID. We intentionally
	// overwrite any existing web_id (not just NULL ones): the relink flow is used
	// precisely when a product's current web ID is stale/unavailable and the user
	// wants to point it at a different one.
	res, err := db.Exec(
		"UPDATE items SET web_id = ? WHERE product_id = ? AND (web_id IS NULL OR web_id != ?)",
		webID, posID, webID,
	)
	if err != nil {
		return 0, err
	}
	updated, _ := res.RowsAffected()
	// Detach this POS id from any other (stale) product row so it isn't claimed
	// by two products at once after a relink.
	if _, err := db.Exec(
		"UPDATE products SET pos_id = NULL WHERE pos_id = ? AND web_id != ?",
		posID, webID,
	); err != nil {
		slog.Warn("clear stale pos_id", "pos_id", posID, "err", err)
	}
	if _, err := db.Exec(
		"INSERT INTO products (web_id, pos_id) VALUES (?, ?) ON CONFLICT(web_id) DO UPDATE SET pos_id = excluded.pos_id WHERE products.pos_id IS NULL",
		webID, posID,
	); err != nil {
		slog.Warn("upsert products pos_id", "web_id", webID, "err", err)
	}
	if _, err := db.Exec(sqlDeleteNotFound, webID); err != nil {
		slog.Warn(logDeleteNotFound, "web_id", webID, "err", err)
	}
	return updated, nil
}

// maybeFetchProduct fetches product metadata from AH if not already stored,
// and returns the title (nil if unavailable).
func maybeFetchProduct(ctx context.Context, db *sql.DB, webID int, cfgPath string) *string {
	if t := productTitleFromDB(db, webID); t != nil {
		return t
	}
	client, err := syncer.InitClient(cfgPath)
	if err != nil {
		return nil
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := syncer.FetchAndStoreBatch(fetchCtx, client, db, []int{webID}, 15*time.Second); err != nil {
		slog.Warn("fetch product after link-pos", "web_id", webID, "err", err)
		return nil
	}
	return productTitleFromDB(db, webID)
}

func productTitleFromDB(db *sql.DB, webID int) *string {
	var t string
	if db.QueryRow(sqlProductTitle, webID).Scan(&t) == nil {
		return &t
	}
	return nil
}
