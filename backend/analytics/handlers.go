package analytics

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"appie-insights/backend/config"
	"appie-insights/backend/enricher"
	"appie-insights/backend/store"
)

const errInvalidSince = "invalid since date, expected YYYY-MM-DD"

// parseSince reads the optional ?since=YYYY-MM-DD filter shared by most
// analytics endpoints. It returns (nil, nil) when the param is absent, the
// parsed time when valid, and an error when malformed.
func parseSince(r *http.Request) (*time.Time, error) {
	s := r.URL.Query().Get("since")
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(dateISO, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// queryPeriod reads the ?period= param, defaulting to "month".
func queryPeriod(r *http.Request) string {
	if p := r.URL.Query().Get("period"); p != "" {
		return p
	}
	return "month"
}

func receiptDetailHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		detail, err := getReceiptDetail(db, r.PathValue("id"))
		if err != nil {
			slog.Error("receiptDetail", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if detail == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, detail)
	}
}

func orderDetailHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			http.Error(w, "invalid order id", http.StatusBadRequest)
			return
		}
		detail, err := getOrderDetail(db, id)
		if err != nil {
			slog.Error("orderDetail", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if detail == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, detail)
	}
}

func searchHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if len(q) < 2 {
			http.Error(w, "q must be at least 2 characters", http.StatusBadRequest)
			return
		}
		results, err := search(db, q)
		if err != nil {
			slog.Error("search", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, results)
	}
}

func productDetailHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		webID, err := strconv.Atoi(r.PathValue("web_id"))
		if err != nil {
			http.Error(w, "invalid web_id", http.StatusBadRequest)
			return
		}
		p, err := getProductDetail(db, webID)
		if err != nil {
			slog.Error("productDetail", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if p == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, p)
	}
}

func productPurchasesHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		webID, err := strconv.Atoi(r.PathValue("web_id"))
		if err != nil {
			http.Error(w, "invalid web_id", http.StatusBadRequest)
			return
		}
		purchases, err := getProductPurchases(db, webID)
		if err != nil {
			slog.Error("productPurchases", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, purchases)
	}
}

func resetDatabaseHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := resetDatabase(db); err != nil {
			slog.Error("resetDatabase", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func enrichmentCountHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n, err := getEnrichmentCount(db)
		if err != nil {
			slog.Error("enrichmentCount", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]int{"count": n})
	}
}

func clearAllEnrichmentHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := clearAllEnrichment(db); err != nil {
			slog.Error("clearAllEnrichment", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func clearProductEnrichmentHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		webID, err := strconv.Atoi(r.PathValue("web_id"))
		if err != nil {
			http.Error(w, "invalid web_id", http.StatusBadRequest)
			return
		}
		n, err := clearProductEnrichment(db, webID)
		if err != nil {
			slog.Error("clearProductEnrichment", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]int{"deleted": n})
	}
}

// RegisterHandlers registers analytics endpoints on mux.
// A read-optimized DB connection is opened and held for the process lifetime.
func RegisterHandlers(mux *http.ServeMux, cfg config.Server) {
	db, err := store.OpenReader(cfg.DBPath)
	if err != nil {
		slog.Error("analytics: open db", "err", err)
		return
	}

	handle := func(method, pattern string, fn func(*sql.DB) (any, error)) {
		mux.HandleFunc(method+" "+pattern, func(w http.ResponseWriter, r *http.Request) {
			v, err := fn(db)
			if err != nil {
				slog.Error(method+" "+pattern, "err", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, v)
		})
	}

	handle("GET", "/receipts", func(db *sql.DB) (any, error) { return getReceipts(db) })
	handle("GET", "/items", func(db *sql.DB) (any, error) { return getItems(db) })
	handle("GET", "/orders", func(db *sql.DB) (any, error) { return getOrders(db) })
	handle("GET", "/products", func(db *sql.DB) (any, error) { return getProducts(db) })
	mux.HandleFunc("GET /products/stats", func(w http.ResponseWriter, r *http.Request) {
		since, err := parseSince(r)
		if err != nil {
			http.Error(w, errInvalidSince, http.StatusBadRequest)
			return
		}
		v, err := getProductStats(db, since)
		if err != nil {
			slog.Error("GET /products/stats", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, v)
	})
	handle("GET", "/corrections/missing-category", func(db *sql.DB) (any, error) { return getMissingCategory(db) })
	handle("GET", "/corrections/missing-weight", func(db *sql.DB) (any, error) { return getMissingWeight(db) })
	mux.HandleFunc("GET /corrections/redundancy", func(w http.ResponseWriter, r *http.Request) {
		corrections, err := enricher.LoadCorrections(cfg.DataDir)
		if err != nil {
			slog.Error("GET /corrections/redundancy: load corrections", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if corrections == nil {
			corrections = []enricher.Correction{}
		}
		statuses, err := enricher.CheckRedundancy(cfg.DataDir, db, corrections)
		if err != nil {
			slog.Error("GET /corrections/redundancy: check redundancy", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, statuses)
	})
	handle("GET", "/products/issues", func(db *sql.DB) (any, error) { return getProductIssues(db) })
	mux.HandleFunc("GET /products/nutriscores", func(w http.ResponseWriter, r *http.Request) {
		since, err := parseSince(r)
		if err != nil {
			http.Error(w, errInvalidSince, http.StatusBadRequest)
			return
		}
		if _, hasScore := r.URL.Query()["score"]; !hasScore {
			dist, err := getNutriscoreDistribution(db, since)
			if err != nil {
				slog.Error("GET /products/nutriscores", "err", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, dist)
			return
		}
		score := r.URL.Query().Get("score")
		products, err := getNutriscoreProducts(db, score, since)
		if err != nil {
			slog.Error("GET /products/nutriscores?score=", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, products)
	})
	handle("GET", "/enrichment/pending", func(db *sql.DB) (any, error) {
		n, err := getPendingEnrichmentCount(db)
		return map[string]int{"count": n}, err
	})

	mux.HandleFunc("GET /receipts/{id}", receiptDetailHandler(db))
	mux.HandleFunc("GET /orders/{id}", orderDetailHandler(db))
	mux.HandleFunc("GET /products/{web_id}", productDetailHandler(db))
	mux.HandleFunc("GET /products/{web_id}/purchases", productPurchasesHandler(db))
	mux.HandleFunc("GET /pos/{pos_id}", func(w http.ResponseWriter, r *http.Request) {
		posID, err := strconv.Atoi(r.PathValue("pos_id"))
		if err != nil {
			http.Error(w, "invalid pos_id", http.StatusBadRequest)
			return
		}
		info, err := getProductByPosID(db, posID)
		if err != nil {
			slog.Error("productByPosID", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if info == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, info)
	})
	mux.HandleFunc("GET /search", searchHandler(db))
	mux.HandleFunc("GET /enrichment/count", enrichmentCountHandler(db))
	mux.HandleFunc("DELETE /enrichment", clearAllEnrichmentHandler(db))
	mux.HandleFunc("DELETE /enrichment/{web_id}", clearProductEnrichmentHandler(db))
	mux.HandleFunc("POST /database/reset", resetDatabaseHandler(db))

	mux.HandleFunc("GET /finances/summary", func(w http.ResponseWriter, r *http.Request) {
		since, err := parseSince(r)
		if err != nil {
			http.Error(w, errInvalidSince, http.StatusBadRequest)
			return
		}
		v, err := getFinancialSummary(db, since)
		if err != nil {
			slog.Error("GET /finances/summary", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, v)
	})
	mux.HandleFunc("GET /finances/by-category", func(w http.ResponseWriter, r *http.Request) {
		since, err := parseSince(r)
		if err != nil {
			http.Error(w, errInvalidSince, http.StatusBadRequest)
			return
		}
		v, err := getSpendingByCategory(db, since)
		if err != nil {
			slog.Error("GET /finances/by-category", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, v)
	})
	mux.HandleFunc("GET /finances/top-discounts", func(w http.ResponseWriter, r *http.Request) {
		since, err := parseSince(r)
		if err != nil {
			http.Error(w, errInvalidSince, http.StatusBadRequest)
			return
		}
		v, err := getTopDiscounts(db, since)
		if err != nil {
			slog.Error("GET /finances/top-discounts", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, v)
	})
	mux.HandleFunc("GET /finances/over-time", func(w http.ResponseWriter, r *http.Request) {
		since, err := parseSince(r)
		if err != nil {
			http.Error(w, errInvalidSince, http.StatusBadRequest)
			return
		}
		data, err := getSpendingOverTime(db, queryPeriod(r), since)
		if err != nil {
			slog.Error("GET /finances/over-time", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, data)
	})

	mux.HandleFunc("GET /sustainability/summary", func(w http.ResponseWriter, r *http.Request) {
		since, err := parseSince(r)
		if err != nil {
			http.Error(w, errInvalidSince, http.StatusBadRequest)
			return
		}
		ae := 1.0
		if s := r.URL.Query().Get("household_ae"); s != "" {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil || v <= 0 {
				http.Error(w, "invalid household_ae, expected positive number", http.StatusBadRequest)
				return
			}
			ae = v
		}
		data, err := getSustainabilitySummary(db, since, ae)
		if err != nil {
			slog.Error("GET /sustainability/summary", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, data)
	})

	mux.HandleFunc("GET /sustainability/trend", func(w http.ResponseWriter, r *http.Request) {
		since, err := parseSince(r)
		if err != nil {
			http.Error(w, errInvalidSince, http.StatusBadRequest)
			return
		}
		data, err := getSustainabilityTrend(db, queryPeriod(r), since)
		if err != nil {
			slog.Error("GET /sustainability/trend", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, data)
	})

	handle("GET", "/sustainability/categories", func(db *sql.DB) (any, error) {
		return getSustainabilityCategories(db)
	})

	mux.HandleFunc("GET /sustainability/categories/{category}/products", func(w http.ResponseWriter, r *http.Request) {
		category := r.PathValue("category")
		periodType := r.URL.Query().Get("period_type")
		periodLabel := r.URL.Query().Get("period_label")
		data, err := getCategoryProducts(db, category, periodType, periodLabel)
		if err != nil {
			slog.Error("GET /sustainability/categories/{category}/products", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, data)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("analytics: writeJSON", "err", err)
	}
}
