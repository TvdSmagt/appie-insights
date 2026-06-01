package enricher

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"appie-insights/backend/schema"
	"appie-insights/backend/store"
	"appie-insights/backend/weight"
)

// Enrich enriches all pending unenriched products in the database.
func Enrich(ctx context.Context, dbPath, dataDir string, progress func(done, total int, label string)) (int, error) {
	db, err := store.Open(dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	webIDs, err := unenrichedWebIDs(db)
	if err != nil {
		return 0, err
	}
	if len(webIDs) == 0 {
		return 0, nil
	}
	slog.Info("enriching products", "count", len(webIDs))
	cfg, err := loadInputs(dataDir)
	if err != nil {
		return 0, err
	}
	if err := runLoop(ctx, db, webIDs, cfg, progress); err != nil {
		return 0, err
	}
	return len(webIDs), nil
}

// EnrichReceipt re-enriches all products on a specific receipt.
func EnrichReceipt(ctx context.Context, dbPath, dataDir, receiptID string, progress func(done, total int, label string)) (int, error) {
	db, err := store.Open(dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT DISTINCT web_id FROM items
		WHERE receipt_id = ? AND web_id IS NOT NULL`, receiptID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var webIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		webIDs = append(webIDs, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(webIDs) == 0 {
		return 0, nil
	}

	placeholders := make([]string, len(webIDs))
	args := make([]any, len(webIDs))
	for i, id := range webIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	if _, err := db.Exec(
		"DELETE FROM product_enrichment WHERE web_id IN ("+strings.Join(placeholders, ",")+")",
		args...,
	); err != nil {
		return 0, err
	}

	slog.Info("re-enriching receipt products", "receipt_id", receiptID, "count", len(webIDs))
	cfg, err := loadInputs(dataDir)
	if err != nil {
		return 0, err
	}
	if err := runLoop(ctx, db, webIDs, cfg, progress); err != nil {
		return 0, err
	}
	return len(webIDs), nil
}

// CountUnenriched returns the number of products pending enrichment.
func CountUnenriched(dbPath string) (int, error) {
	db, err := store.Open(dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT web_id FROM items       WHERE web_id IS NOT NULL
			UNION
			SELECT web_id FROM order_items WHERE web_id IS NOT NULL
		)
		WHERE web_id NOT IN (SELECT web_id FROM product_enrichment)
	`).Scan(&n)
	return n, err
}

type enrichmentConfig struct {
	ahSubcatLookup map[string]ahSubcategoryEntry
	corrections    map[int]Correction
	weights        []defaultWeight
}

func loadInputs(dataDir string) (*enrichmentConfig, error) {
	ahSubcatEntries, err := loadAHSubcategoryMap(dataDir)
	if err != nil {
		return nil, err
	}
	corrections, err := LoadCorrections(dataDir)
	if err != nil {
		return nil, err
	}
	weights, err := loadDefaultWeights(dataDir)
	if err != nil {
		return nil, err
	}

	corrMap := make(map[int]Correction, len(corrections))
	for _, c := range corrections {
		corrMap[c.WebID] = c
	}

	return &enrichmentConfig{
		ahSubcatLookup: buildAHSubcategoryLookup(ahSubcatEntries),
		corrections:    corrMap,
		weights:        weights,
	}, nil
}

func enrichProduct(db *sql.DB, webID int, cfg *enrichmentConfig) error {
	if corr, ok := cfg.corrections[webID]; ok {
		if corr.Action == "ignore" {
			return storeEnrichment(db, webID, matchResult{method: schema.MatchMethodIgnored}, nil, "")
		}
		r := matchResult{
			name:     nilIfEmpty(corr.CO2Name),
			category: nilIfEmpty(corr.CO2Category),
			co2PerKg: corr.CO2PerKg,
			method:   schema.MatchMethodCorrection,
		}
		// A correction's weight (if any) is authoritative; otherwise the
		// product is left without a weight here and resolveWeights derives one.
		source := ""
		if corr.WeightKg != nil {
			source = weight.SourceCorrection
		}
		return storeEnrichment(db, webID, r, corr.WeightKg, source)
	}

	result := matchCO2Category(webID, db, cfg.ahSubcatLookup)
	// Weight is intentionally left nil: resolveWeights runs after the match
	// pass and resolves all weights in priority order (default keyword
	// estimates last). Setting a weight here would let that rough estimate
	// override more accurate sources like net_content and serving_size.
	return storeEnrichment(db, webID, result, nil, "")
}

// resolveWeights fills product_enrichment.weight_kg for every enriched product
// that doesn't yet have a weight, applying sources from most to least reliable.
// It runs at the end of each enrichment pass, so re-enriched products (whose
// enrichment rows were deleted first) get their weights recomputed from scratch.
//
// The order is driven by weight.SourcePriority — the single source of truth for
// the policy (see docs/weight-resolution.md). Each source maps to its fill
// function below; weight.SourceCorrection has none (corrections are applied
// during matching, in enrichProduct) and is skipped here.
func resolveWeights(db *sql.DB, weights []defaultWeight) {
	fills := map[string]func() (int, error){
		weight.SourceUnitSize:    func() (int, error) { return fillUnitSizeWeights(db) },
		weight.SourceNetContent:  func() (int, error) { return fillNetContentWeights(db) },
		weight.SourceMultipack:   func() (int, error) { return fillMultipackWeights(db) },
		weight.SourceDefault:     func() (int, error) { return fillDefaultWeights(db, weights) },
		weight.SourceServingSize: func() (int, error) { return fillServingSizeWeights(db) },
	}
	for _, source := range weight.SourcePriority {
		fill, ok := fills[source]
		if !ok {
			continue // correction is applied during matching, not here
		}
		if _, err := fill(); err != nil {
			slog.Warn("resolveWeights", "source", source, "err", err)
		}
	}
}

func runLoop(
	ctx context.Context,
	db *sql.DB,
	webIDs []int,
	cfg *enrichmentConfig,
	progress func(done, total int, label string),
) error {
	for i, webID := range webIDs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := enrichProduct(db, webID, cfg); err != nil {
			slog.Warn("enrich product failed", "web_id", webID, "err", err)
		}
		if progress != nil {
			var title sql.NullString
			_ = db.QueryRow("SELECT title FROM products WHERE web_id = ?", webID).Scan(&title)
			label := title.String
			if label == "" {
				label = fmt.Sprintf("%d", webID)
			}
			progress(i+1, len(webIDs), label)
		}
	}
	resolveWeights(db, cfg.weights)
	return nil
}

func unenrichedWebIDs(db *sql.DB) ([]int, error) {
	rows, err := db.Query(`
		SELECT DISTINCT web_id FROM (
			SELECT web_id FROM items       WHERE web_id IS NOT NULL
			UNION
			SELECT web_id FROM order_items WHERE web_id IS NOT NULL
		)
		WHERE web_id NOT IN (SELECT web_id FROM product_enrichment)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func storeEnrichment(db *sql.DB, webID int, r matchResult, weightKg *float64, weightSource string) error {
	var src *string
	if weightSource != "" {
		src = &weightSource
	}
	_, err := db.Exec(`
		INSERT OR REPLACE INTO product_enrichment
		(web_id, co2eq_category, co2eq_name, co2eq_per_kg, match_method, weight_kg, weight_source)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		webID, r.category, r.name, r.co2PerKg, r.method, weightKg, src,
	)
	return err
}
