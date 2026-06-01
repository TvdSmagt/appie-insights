package syncer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appie "github.com/gwillem/appie-go"
)

func pickThumbnailURL(images []appie.Image, targetWidth int) string {
	if len(images) == 0 {
		return ""
	}
	best := images[0]
	bestDiff := abs(best.Width - targetWidth)
	for _, img := range images[1:] {
		if diff := abs(img.Width - targetWidth); diff < bestDiff {
			best = img
			bestDiff = diff
		}
	}
	return best.URL
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// upsertProductMetadata inserts a products row or, if one already exists with a
// missing title, updates it with fresh metadata from the API.
func upsertProductMetadata(ctx context.Context, db *sql.DB, webID int, product *appie.Product, iconsJSON []byte, thumbnailURL string) error {
	res, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO products
			(web_id, title, brand, ah_category, ah_subcategory,
			 nutriscore, nutriscore_checked_at, unit_size, unit_price_description, property_icons, thumbnail_url)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'), ?, ?, ?, ?)`,
		webID, product.Title, product.Brand,
		product.Category, product.SubCategory,
		product.NutriScore, product.UnitSize, product.UnitPriceDescription,
		string(iconsJSON), thumbnailURL)
	if err != nil {
		return fmt.Errorf("insert web_id=%d: %w", webID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := db.ExecContext(ctx, `
			UPDATE products
			SET title = ?, brand = ?, ah_category = ?, ah_subcategory = ?,
			    nutriscore = ?, nutriscore_checked_at = datetime('now'),
			    unit_size = ?, unit_price_description = ?,
			    property_icons = ?, thumbnail_url = ?
			WHERE web_id = ? AND (title IS NULL OR TRIM(title) = '')`,
			product.Title, product.Brand,
			product.Category, product.SubCategory,
			product.NutriScore,
			product.UnitSize, product.UnitPriceDescription,
			string(iconsJSON), thumbnailURL, webID); err != nil {
			return fmt.Errorf("update web_id=%d: %w", webID, err)
		}
		// Always refresh nutriscore and its timestamp: picks up scores AH adds over
		// time and advances the 30-day re-check window for existing scores.
		if _, err := db.ExecContext(ctx,
			`UPDATE products SET nutriscore = ?, nutriscore_checked_at = datetime('now') WHERE web_id = ?`,
			product.NutriScore, webID); err != nil {
			return fmt.Errorf("update nutriscore web_id=%d: %w", webID, err)
		}
	}
	return nil
}

const productBatchSize = 50

// pieceSizeSQL matches unit_size values that represent whole pieces rather than
// a measurable quantity (e.g. "per stuk", "6 stuks", "per bosje").
const pieceSizeSQL = `(
	LOWER(p.unit_size) = 'per stuk'
	OR LOWER(p.unit_size) = 'per bosje'
	OR LOWER(p.unit_size) = 'per pakket'
	OR LOWER(p.unit_size) LIKE '% stuks'
	OR LOWER(p.unit_size) LIKE '% stuk'
)`

// backfillMappedProductDetails fetches full product metadata from the AH API
// for web_ids referenced by items or order_items that have no products row or
// a row with a missing title. Requests are batched to minimise round trips.
// Web IDs absent from the API response are recorded in product_not_found and
// permanently skipped on future runs.
func backfillMappedProductDetails(ctx context.Context, client *appie.Client, db *sql.DB, delay, apiTimeout time.Duration) error {
	rows, err := db.QueryContext(ctx, `
		WITH all_web_ids AS (
			SELECT web_id FROM items       WHERE web_id IS NOT NULL
			UNION
			SELECT web_id FROM order_items WHERE web_id IS NOT NULL
		)
		SELECT DISTINCT a.web_id
		FROM all_web_ids a
		LEFT JOIN products p ON p.web_id = a.web_id
		WHERE (p.web_id IS NULL OR p.title IS NULL OR TRIM(p.title) = '')
		  AND a.web_id NOT IN (SELECT web_id FROM product_not_found)`)
	if err != nil {
		return fmt.Errorf("query web_ids needing product details: %w", err)
	}
	defer rows.Close()

	var webIDs []int
	for rows.Next() {
		var wid int
		if err := rows.Scan(&wid); err != nil {
			return err
		}
		webIDs = append(webIDs, wid)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(webIDs) == 0 {
		return nil
	}
	slog.Info("backfill product details: starting", "count", len(webIDs))

	for i := 0; i < len(webIDs); i += productBatchSize {
		if i > 0 {
			time.Sleep(delay)
		}
		batch := webIDs[i:min(i+productBatchSize, len(webIDs))]
		if err := FetchAndStoreBatch(ctx, client, db, batch, apiTimeout); err != nil {
			return err
		}
		slog.Info("backfill product details",
			"progress", fmt.Sprintf("%d/%d", min(i+productBatchSize, len(webIDs)), len(webIDs)))
	}
	return nil
}

// backfillMissingNutriscores re-fetches product metadata for products whose
// nutriscore is missing or has not been checked in the past 30 days. This
// handles products stored before nutriscore tracking and picks up scores that
// AH adds over time.
func backfillMissingNutriscores(ctx context.Context, client *appie.Client, db *sql.DB, delay, apiTimeout time.Duration) error {
	rows, err := db.QueryContext(ctx, `
		SELECT web_id FROM products
		WHERE (title IS NOT NULL AND TRIM(title) != '')
		  AND (nutriscore IS NULL
		       OR nutriscore_checked_at IS NULL
		       OR nutriscore_checked_at < datetime('now', '-30 days'))
		  AND web_id NOT IN (SELECT web_id FROM product_not_found)`)
	if err != nil {
		return fmt.Errorf("query web_ids needing nutriscore: %w", err)
	}
	defer rows.Close()

	var webIDs []int
	for rows.Next() {
		var wid int
		if err := rows.Scan(&wid); err != nil {
			return err
		}
		webIDs = append(webIDs, wid)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(webIDs) == 0 {
		return nil
	}
	slog.Info("backfill nutriscores: starting", "count", len(webIDs))

	for i := 0; i < len(webIDs); i += productBatchSize {
		if i > 0 {
			time.Sleep(delay)
		}
		batch := webIDs[i:min(i+productBatchSize, len(webIDs))]
		if err := FetchAndStoreBatch(ctx, client, db, batch, apiTimeout); err != nil {
			return err
		}
		slog.Info("backfill nutriscores",
			"progress", fmt.Sprintf("%d/%d", min(i+productBatchSize, len(webIDs)), len(webIDs)))
	}
	return nil
}

// FetchAndStoreBatch fetches product metadata for the given web IDs from the AH
// API and stores results. IDs absent from the API response are recorded in
// product_not_found and permanently skipped on future sync runs.
func FetchAndStoreBatch(ctx context.Context, client *appie.Client, db *sql.DB, batch []int, apiTimeout time.Duration) error {
	reqCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	products, err := client.GetProductsByIDs(reqCtx, batch)
	cancel()
	if err != nil {
		return fmt.Errorf("GetProductsByIDs: %w", err)
	}

	// Detect IDs the API silently omitted (product gone / 404).
	returned := make(map[int]struct{}, len(products))
	for _, p := range products {
		returned[p.ID] = struct{}{}
	}
	for _, wid := range batch {
		if _, ok := returned[wid]; !ok {
			slog.Info("product not found, skipping permanently", "web_id", wid)
			if _, err := db.ExecContext(ctx, "INSERT OR IGNORE INTO product_not_found (web_id) VALUES (?)", wid); err != nil {
				slog.Warn("record product_not_found", "web_id", wid, "err", err)
			}
		}
	}

	for _, product := range products {
		iconsJSON, err := json.Marshal(product.PropertyIcons)
		if err != nil {
			return fmt.Errorf("marshal property icons for web_id=%d: %w", product.ID, err)
		}
		thumbnailURL := pickThumbnailURL(product.Images, 200)
		if err := upsertProductMetadata(ctx, db, product.ID, &product, iconsJSON, thumbnailURL); err != nil {
			slog.Warn("upsert product metadata", "web_id", product.ID, "err", err)
		}
	}
	return nil
}

// backfillServingSizes fetches serving size descriptions via GraphQL for products
// whose unit_size is piece-based (e.g. "per stuk") and don't yet have a serving_size.
// The serving size (e.g. "70 gram") is stored in products.serving_size and later
// used by the enricher to derive weight_kg for CO2 calculations.
func backfillServingSizes(ctx context.Context, client *appie.Client, db *sql.DB, delay, apiTimeout time.Duration) error {
	webIDs, err := pieceSizeWebIDs(ctx, db)
	if err != nil {
		return err
	}
	if len(webIDs) == 0 {
		return nil
	}
	slog.Info("backfill serving sizes: starting", "count", len(webIDs))
	for i, wid := range webIDs {
		if i > 0 {
			time.Sleep(delay)
		}
		fetchAndStoreServingSize(ctx, client, db, wid, apiTimeout)
	}
	slog.Info("backfill serving sizes: done", "count", len(webIDs))
	return nil
}

func pieceSizeWebIDs(ctx context.Context, db *sql.DB) ([]int, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT p.web_id FROM products p
		WHERE `+pieceSizeSQL+`
		  AND p.serving_size IS NULL
		  AND p.web_id NOT IN (SELECT web_id FROM product_not_found)`)
	if err != nil {
		return nil, fmt.Errorf("query web_ids needing serving size: %w", err)
	}
	defer rows.Close()
	var webIDs []int
	for rows.Next() {
		var wid int
		if err := rows.Scan(&wid); err != nil {
			return nil, err
		}
		webIDs = append(webIDs, wid)
	}
	return webIDs, rows.Err()
}

func fetchAndStoreServingSize(ctx context.Context, client *appie.Client, db *sql.DB, wid int, apiTimeout time.Duration) {
	reqCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	nutritions, err := client.FetchNutritionalInfo(reqCtx, wid)
	cancel()
	if err != nil {
		slog.Warn("backfill serving size: fetch failed", "web_id", wid, "err", err)
		return
	}
	desc := firstServingSizeDescription(nutritions)
	if _, err := db.ExecContext(ctx,
		"UPDATE products SET serving_size = ? WHERE web_id = ?", desc, wid,
	); err != nil {
		slog.Warn("backfill serving size: store failed", "web_id", wid, "err", err)
	}
}

func firstServingSizeDescription(nutritions []appie.NutritionalInfo) string {
	for _, n := range nutritions {
		if n.ServingSizeDescription != "" {
			return n.ServingSizeDescription
		}
	}
	return ""
}

// backfillNetContents fetches tradeItem.contents.netContents via GraphQL for
// piece-based products that don't yet have a net_content value. The first
// entry containing a parseable weight (e.g. "50.0 Gram") is stored in
// products.net_content and later used by the enricher to compute weight_kg
// for products whose serving_size gives no usable weight (e.g. tea boxes).
func backfillNetContents(ctx context.Context, client *appie.Client, db *sql.DB, delay, apiTimeout time.Duration) error {
	rows, err := db.QueryContext(ctx, `
		SELECT p.web_id FROM products p
		WHERE `+pieceSizeSQL+`
		  AND p.net_content IS NULL
		  AND p.web_id NOT IN (SELECT web_id FROM product_not_found)`)
	if err != nil {
		return fmt.Errorf("query web_ids needing net content: %w", err)
	}
	defer rows.Close()
	var webIDs []int
	for rows.Next() {
		var wid int
		if err := rows.Scan(&wid); err != nil {
			return err
		}
		webIDs = append(webIDs, wid)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	if len(webIDs) == 0 {
		return nil
	}
	slog.Info("backfill net contents: starting", "count", len(webIDs))
	for i, wid := range webIDs {
		if i > 0 {
			time.Sleep(delay)
		}
		fetchAndStoreNetContent(ctx, client, db, wid, apiTimeout)
	}
	slog.Info("backfill net contents: done", "count", len(webIDs))
	return nil
}

func fetchAndStoreNetContent(ctx context.Context, client *appie.Client, db *sql.DB, wid int, apiTimeout time.Duration) {
	reqCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	netContents, err := client.FetchNetContents(reqCtx, wid)
	cancel()
	if err != nil {
		slog.Warn("backfill net content: fetch failed", "web_id", wid, "err", err)
		return
	}
	// Store the first entry that looks like a weight (e.g. "50.0 Gram"), or ""
	// to mark as fetched-but-empty so we don't retry on every sync.
	val := firstWeightEntry(netContents)
	if _, err := db.ExecContext(ctx,
		"UPDATE products SET net_content = ? WHERE web_id = ?", val, wid,
	); err != nil {
		slog.Warn("backfill net content: store failed", "web_id", wid, "err", err)
	}
}

// firstWeightEntry returns the first string from netContents that parses as a
// weight (contains a unit like "gram" or "kg"), or "" if none found.
func firstWeightEntry(netContents []string) string {
	for _, s := range netContents {
		lower := strings.ToLower(s)
		for _, unit := range []string{"gram", "kg", "kilo", "liter", "litre", "ml", "cl"} {
			if strings.Contains(lower, unit) {
				return s
			}
		}
	}
	return ""
}
