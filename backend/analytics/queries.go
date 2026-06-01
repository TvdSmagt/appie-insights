package analytics

import (
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"appie-insights/backend/schema"
	"appie-insights/backend/weight"
)

const (
	refSustainableKgPerPersonMonth = 42.0
	refAvgKgPerPersonMonth         = 88.0
)

const dateISO = "2006-01-02"

const sqlSinceReceipt = " AND EXISTS (SELECT 1 FROM receipts r WHERE r.transaction_id = i.receipt_id AND DATE(r.date) >= ?)"
const sqlSinceOrder = " WHERE EXISTS (SELECT 1 FROM orders o WHERE o.order_id = oi.order_id AND DATE(o.delivery_date) >= ?)"
const sqlSinceOrderAnd = " AND EXISTS (SELECT 1 FROM orders o WHERE o.order_id = oi.order_id AND DATE(o.delivery_date) >= ?)"

// nonFoodMatchMethodList is the set of schema.MatchMethod* values that mark items
// as non-food, unidentifiable, or user-ignored — excluded from CO₂ views. The
// vocabulary is defined once in schema (written by the enricher); this is the
// analytics policy for which of those values count as "not food". The SQL literals
// and isNonFoodMethod below are all derived from it.
var nonFoodMatchMethodList = []string{
	schema.MatchMethodNonFood,
	schema.MatchMethodNoProduct,
	schema.MatchMethodNoMetadata,
	schema.MatchMethodIgnored,
}

// nonFoodMethods is the SQL literal list for use in NOT IN() clauses.
var nonFoodMethods = sqlInList(nonFoodMatchMethodList)

// nonFoodExceptNoMetadata drops no_metadata, for queries that intentionally
// surface no-metadata items (the no-product-data issue list).
var nonFoodExceptNoMetadata = sqlInListExcept(nonFoodMatchMethodList, schema.MatchMethodNoMetadata)

// nonFoodAndUnmatchedMethods extends nonFoodMethods for weight queries, where
// items without a CO₂ match are also irrelevant (no CO₂ factor means weight doesn't matter).
var nonFoodAndUnmatchedMethods = sqlInList(append(append([]string{}, nonFoodMatchMethodList...), schema.MatchMethodUnmatched))

// sqlInList renders values as a comma-separated, single-quoted SQL literal list.
// Values are fixed schema.MatchMethod* constants, never user input, so no escaping
// is needed.
func sqlInList(vals []string) string {
	quoted := make([]string, len(vals))
	for i, v := range vals {
		quoted[i] = "'" + v + "'"
	}
	return strings.Join(quoted, ", ")
}

// sqlInListExcept renders vals as a SQL literal list, omitting one value, so a
// variant exclusion set can be derived from nonFoodMatchMethodList without
// re-listing it.
func sqlInListExcept(vals []string, omit string) string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v != omit {
			out = append(out, v)
		}
	}
	return sqlInList(out)
}

// isNonFoodMethod reports whether a match_method marks an item as non-food,
// unidentifiable, or user-ignored. Derived from nonFoodMatchMethodList.
func isNonFoodMethod(method string) bool {
	for _, m := range nonFoodMatchMethodList {
		if m == method {
			return true
		}
	}
	return false
}

func nullFloat64Ptr(n sql.NullFloat64) *float64 {
	if n.Valid {
		return &n.Float64
	}
	return nil
}

func computeCO2Total(co2PerKg *float64, quantity int, weightPerUnitKg *float64) *float64 {
	if co2PerKg == nil || weightPerUnitKg == nil {
		return nil
	}
	v := *co2PerKg * float64(quantity) * *weightPerUnitKg
	return &v
}

// unitSizeYieldsWeight reports whether unit_size alone resolves to a weight at
// query time (the unit_size branch of weight.EffectiveKg). The missing-weight
// lists use it to confirm a product with no stored weight_kg is truly weightless
// before surfacing it — there is deliberately no 1 kg fallback.
func unitSizeYieldsWeight(unitSize string) bool {
	_, ok := weight.ParseKg(unitSize)
	return ok
}

// --- Product stats accumulator (shared by getProductStats and getNutriscoreProducts) ---

type statsKey struct {
	webID int
	title string
}

type statsAcc struct {
	thumbnailURL string
	nutriscore   string
	timesBought  int
	totalSpent   float64
	totalKg      float64
	co2EqTotal   float64
	hasCO2       bool
}

// productStatsInput holds one scanned row from the items/order_items UNION query.
// The nutriscore field is empty for queries that don't filter by it.
type productStatsInput struct {
	webID        int
	title        string
	thumbnailURL string
	nutriscore   string
	quantity     int
	amount       float64
	co2PerKg     sql.NullFloat64
	weightKg     sql.NullFloat64
	unitSize     string
}

// accumulateProductRows drives the accumulation loop shared by getProductStats
// and getNutriscoreProducts. The caller supplies a scan function that matches
// the column layout of its specific query.
func accumulateProductRows(rows *sql.Rows, scan func(*sql.Rows) (productStatsInput, error)) ([]statsKey, map[statsKey]*statsAcc, error) {
	defer rows.Close()
	accs := make(map[statsKey]*statsAcc)
	var keys []statsKey

	for rows.Next() {
		row, err := scan(rows)
		if err != nil {
			return nil, nil, err
		}
		k := statsKey{webID: row.webID, title: row.title}
		a, ok := accs[k]
		if !ok {
			a = &statsAcc{thumbnailURL: row.thumbnailURL, nutriscore: row.nutriscore}
			accs[k] = a
			keys = append(keys, k)
		}
		a.timesBought++
		a.totalSpent += row.amount

		w := weight.EffectiveKg(nullFloat64Ptr(row.weightKg), row.unitSize)
		if w == nil {
			continue
		}
		a.totalKg += float64(row.quantity) * *w
		if co2 := computeCO2Total(nullFloat64Ptr(row.co2PerKg), row.quantity, w); co2 != nil {
			a.co2EqTotal += *co2
			a.hasCO2 = true
		}
	}
	return keys, accs, rows.Err()
}

// --- Queries ---

func getReceipts(db *sql.DB) ([]Receipt, error) {
	rows, err := db.Query(`
		SELECT
			r.transaction_id,
			r.date,
			r.total_amount,
			COUNT(CASE WHEN i.web_id IS NOT NULL AND (pe.match_method NOT IN (` + nonFoodMethods + `) OR pe.match_method IS NULL) THEN 1 END),
			COUNT(CASE WHEN pe.co2eq_name IS NOT NULL THEN 1 END)
		FROM receipts r
		LEFT JOIN items i ON i.receipt_id = r.transaction_id
		LEFT JOIN product_enrichment pe ON pe.web_id = i.web_id
		GROUP BY r.transaction_id
		ORDER BY r.date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	receipts := make([]Receipt, 0)
	for rows.Next() {
		var r Receipt
		if err := rows.Scan(&r.TransactionID, &r.Date, &r.TotalAmount, &r.ItemCount, &r.MatchedCount); err != nil {
			return nil, err
		}
		receipts = append(receipts, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Compute CO2 and weight totals per receipt in Go (requires weight parsing from unit_size).
	co2Rows, err := db.Query(`
		SELECT i.receipt_id, i.quantity, pe.co2eq_per_kg, pe.weight_kg, COALESCE(p.unit_size, '')
		FROM items i
		LEFT JOIN products p ON p.web_id = i.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = i.web_id
		WHERE pe.weight_kg IS NOT NULL OR p.unit_size IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer co2Rows.Close()

	co2Totals := make(map[string]float64)
	weightTotals := make(map[string]float64)
	for co2Rows.Next() {
		var receiptID, unitSize string
		var quantity int
		var co2PerKg, weightKg sql.NullFloat64
		if err := co2Rows.Scan(&receiptID, &quantity, &co2PerKg, &weightKg, &unitSize); err != nil {
			return nil, err
		}
		w := weight.EffectiveKg(nullFloat64Ptr(weightKg), unitSize)
		if w != nil {
			weightTotals[receiptID] += float64(quantity) * *w
			if co2PerKg.Valid {
				if t := computeCO2Total(&co2PerKg.Float64, quantity, w); t != nil {
					co2Totals[receiptID] += *t
				}
			}
		}
	}
	if err := co2Rows.Err(); err != nil {
		return nil, err
	}

	discountRows, err := db.Query(`SELECT receipt_id, ABS(SUM(amount)) FROM receipt_discounts GROUP BY receipt_id`)
	if err != nil {
		return nil, err
	}
	defer discountRows.Close()

	discountTotals := make(map[string]float64)
	for discountRows.Next() {
		var receiptID string
		var amount float64
		if err := discountRows.Scan(&receiptID, &amount); err != nil {
			return nil, err
		}
		discountTotals[receiptID] = amount
	}
	if err := discountRows.Err(); err != nil {
		return nil, err
	}

	for i, r := range receipts {
		if v, ok := co2Totals[r.TransactionID]; ok {
			receipts[i].CO2EqTotal = &v
		}
		if v, ok := weightTotals[r.TransactionID]; ok {
			receipts[i].WeightTotal = &v
		}
		if v, ok := discountTotals[r.TransactionID]; ok {
			receipts[i].DiscountTotal = &v
		}
	}
	return receipts, nil
}

func getReceiptDetail(db *sql.DB, receiptID string) (*ReceiptDetail, error) {
	var d ReceiptDetail
	err := db.QueryRow(`
		SELECT transaction_id, date, total_amount FROM receipts WHERE transaction_id = ?`, receiptID).
		Scan(&d.TransactionID, &d.Date, &d.TotalAmount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT
			i.id, i.description, COALESCE(p.title, ''), i.quantity, i.amount, i.web_id, i.product_id,
			COALESCE(p.ah_category, ''), COALESCE(pe.co2eq_category, ''), COALESCE(pe.co2eq_name, ''),
			pe.co2eq_per_kg, COALESCE(pe.match_method, ''),
			pe.weight_kg, COALESCE(p.unit_size, ''), COALESCE(p.thumbnail_url, '')
		FROM items i
		LEFT JOIN products p ON p.web_id = i.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = i.web_id
		WHERE i.receipt_id = ?
		ORDER BY i.amount DESC`, receiptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	d.Items = make([]ReceiptItem, 0)
	for rows.Next() {
		var it ReceiptItem
		var webID, posID sql.NullInt64
		var co2PerKg, weightKg sql.NullFloat64
		if err := rows.Scan(
			&it.ID, &it.Description, &it.WebTitle, &it.Quantity, &it.Amount, &webID, &posID,
			&it.AHCategory, &it.CO2EqCategory, &it.CO2EqName, &co2PerKg, &it.MatchMethod,
			&weightKg, &it.UnitSize, &it.ThumbnailURL,
		); err != nil {
			return nil, err
		}
		if webID.Valid {
			v := int(webID.Int64)
			it.WebID = &v
		}
		if posID.Valid {
			v := int(posID.Int64)
			it.PosID = &v
		}
		if co2PerKg.Valid {
			it.CO2EqPerKg = &co2PerKg.Float64
		}
		if weightKg.Valid {
			it.WeightKg = &weightKg.Float64
		}
		it.WeightPerUnitKg = weight.EffectiveKg(it.WeightKg, it.UnitSize)
		it.CO2EqTotal = computeCO2Total(it.CO2EqPerKg, it.Quantity, it.WeightPerUnitKg)
		d.Items = append(d.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var totalCO2 float64
	for _, it := range d.Items {
		if it.WebID != nil && !isNonFoodMethod(it.MatchMethod) {
			d.ItemCount++
		}
		if it.CO2EqName != "" {
			d.MatchedCount++
		}
		if it.CO2EqTotal != nil {
			totalCO2 += *it.CO2EqTotal
		}
	}
	if totalCO2 > 0 {
		d.CO2EqTotal = &totalCO2
		if d.TotalAmount > 0 {
			v := totalCO2 / d.TotalAmount
			d.CO2EqPerEuro = &v
		}
	}
	return &d, nil
}

func getItems(db *sql.DB) ([]Item, error) {
	rRows, err := db.Query(`
		SELECT
			'receipt',
			i.description, i.quantity, i.amount, r.date,
			COALESCE(pe.co2eq_category, ''), COALESCE(pe.co2eq_name, ''),
			pe.co2eq_per_kg, COALESCE(pe.match_method, ''), pe.weight_kg,
			COALESCE(p.unit_size, ''), i.web_id, COALESCE(p.title, '')
		FROM items i
		JOIN receipts r ON r.transaction_id = i.receipt_id
		LEFT JOIN products p ON p.web_id = i.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = i.web_id
		ORDER BY r.date DESC`)
	if err != nil {
		return nil, err
	}
	items, err := scanItems(rRows)
	if err != nil {
		return nil, err
	}

	oRows, err := db.Query(`
		SELECT
			'order',
			oi.title, oi.allocated_qty,
			ROUND(oi.allocated_qty * COALESCE(oi.unit_price, 0), 2),
			o.delivery_date,
			COALESCE(pe.co2eq_category, ''), COALESCE(pe.co2eq_name, ''),
			pe.co2eq_per_kg, COALESCE(pe.match_method, ''), pe.weight_kg,
			COALESCE(oi.sales_unit_size, COALESCE(p.unit_size, '')),
			oi.web_id, COALESCE(p.title, '')
		FROM order_items oi
		JOIN orders o ON o.order_id = oi.order_id
		LEFT JOIN products p ON p.web_id = oi.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = oi.web_id
		ORDER BY o.delivery_date DESC`)
	if err != nil {
		return nil, err
	}
	orderItems, err := scanItems(oRows)
	if err != nil {
		return nil, err
	}

	return append(items, orderItems...), nil
}

func scanItems(rows *sql.Rows) ([]Item, error) {
	defer rows.Close()
	result := make([]Item, 0)
	for rows.Next() {
		var it Item
		var webID sql.NullInt64
		var co2PerKg, weightKg sql.NullFloat64
		if err := rows.Scan(
			&it.SourceType, &it.Description, &it.Quantity, &it.Amount, &it.Date,
			&it.CO2EqCategory, &it.CO2EqName, &co2PerKg, &it.MatchMethod,
			&weightKg, &it.UnitSize, &webID, &it.WebTitle,
		); err != nil {
			return nil, err
		}
		if webID.Valid {
			v := int(webID.Int64)
			it.WebID = &v
		}
		if co2PerKg.Valid {
			it.CO2EqPerKg = &co2PerKg.Float64
		}
		if weightKg.Valid {
			it.WeightKg = &weightKg.Float64
		}
		it.WeightPerUnitKg = weight.EffectiveKg(it.WeightKg, it.UnitSize)
		it.CO2EqTotal = computeCO2Total(it.CO2EqPerKg, it.Quantity, it.WeightPerUnitKg)
		result = append(result, it)
	}
	return result, rows.Err()
}

func getProducts(db *sql.DB) ([]Product, error) {
	rows, err := db.Query(`
		SELECT
			p.web_id,
			COALESCE(p.thumbnail_url, ''), COALESCE(p.title, ''), COALESCE(p.brand, ''),
			COALESCE(p.ah_category, ''), COALESCE(p.ah_subcategory, ''),
			COALESCE(p.unit_size, ''), COALESCE(p.nutriscore, ''),
			COALESCE(p.unit_price_description, ''), COALESCE(p.property_icons, ''),
			pe.co2eq_per_kg, COALESCE(pe.co2eq_category, ''),
			pe.weight_kg
		FROM products p
		LEFT JOIN product_enrichment pe ON pe.web_id = p.web_id
		WHERE p.title IS NOT NULL AND TRIM(p.title) != ''
		ORDER BY p.title ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]Product, 0)
	for rows.Next() {
		var pr Product
		var co2PerKg, weightKg sql.NullFloat64
		if err := rows.Scan(
			&pr.WebID, &pr.ThumbnailURL, &pr.Title, &pr.Brand,
			&pr.AHCategory, &pr.AHSubcategory, &pr.UnitSize, &pr.Nutriscore,
			&pr.UnitPriceDescription, &pr.PropertyIcons,
			&co2PerKg, &pr.CO2EqCategory,
			&weightKg,
		); err != nil {
			return nil, err
		}
		if co2PerKg.Valid {
			pr.CO2EqPerKg = &co2PerKg.Float64
		}
		var wkgPtr *float64
		if weightKg.Valid {
			wkgPtr = &weightKg.Float64
		}
		pr.WeightPerUnitKg = weight.EffectiveKg(wkgPtr, pr.UnitSize)
		if pr.CO2EqPerKg != nil && pr.WeightPerUnitKg != nil {
			v := *pr.CO2EqPerKg * *pr.WeightPerUnitKg
			pr.CO2EqPerUnit = &v
		}
		result = append(result, pr)
	}
	return result, rows.Err()
}

func getOrders(db *sql.DB) ([]Order, error) {
	rows, err := db.Query(`
		SELECT o.order_id, o.delivery_date,
		       COALESCE(o.delivery_method, ''), COALESCE(o.delivery_status, ''),
		       COALESCE(o.total_price, 0),
		       COUNT(CASE WHEN oi.web_id IS NOT NULL AND (pe.match_method NOT IN (` + nonFoodMethods + `) OR pe.match_method IS NULL) THEN 1 END),
		       COUNT(CASE WHEN pe.co2eq_name IS NOT NULL THEN 1 END)
		FROM orders o
		LEFT JOIN order_items oi ON oi.order_id = o.order_id
		LEFT JOIN product_enrichment pe ON pe.web_id = oi.web_id
		GROUP BY o.order_id
		ORDER BY o.delivery_date DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := make([]Order, 0)
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.OrderID, &o.DeliveryDate, &o.DeliveryMethod, &o.DeliveryStatus,
			&o.TotalPrice, &o.ItemCount, &o.MatchedCount); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	co2Rows, err := db.Query(`
		SELECT oi.order_id, oi.allocated_qty, pe.co2eq_per_kg, pe.weight_kg,
		       COALESCE(oi.sales_unit_size, COALESCE(p.unit_size, '')) AS unit_size
		FROM order_items oi
		LEFT JOIN products p ON p.web_id = oi.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = oi.web_id
		WHERE pe.weight_kg IS NOT NULL OR oi.sales_unit_size IS NOT NULL OR p.unit_size IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer co2Rows.Close()

	co2Totals := make(map[int]float64)
	weightTotals := make(map[int]float64)
	for co2Rows.Next() {
		var orderID, quantity int
		var co2PerKg, weightKg sql.NullFloat64
		var unitSize string
		if err := co2Rows.Scan(&orderID, &quantity, &co2PerKg, &weightKg, &unitSize); err != nil {
			return nil, err
		}
		w := weight.EffectiveKg(nullFloat64Ptr(weightKg), unitSize)
		if w != nil {
			weightTotals[orderID] += float64(quantity) * *w
			if co2PerKg.Valid {
				if t := computeCO2Total(&co2PerKg.Float64, quantity, w); t != nil {
					co2Totals[orderID] += *t
				}
			}
		}
	}
	if err := co2Rows.Err(); err != nil {
		return nil, err
	}

	discountRows, err := db.Query(`
		SELECT order_id, SUM((was_price - unit_price) * allocated_qty)
		FROM order_items
		WHERE was_price IS NOT NULL AND unit_price IS NOT NULL AND was_price > unit_price
		GROUP BY order_id`)
	if err != nil {
		return nil, err
	}
	defer discountRows.Close()

	discountTotals := make(map[int]float64)
	for discountRows.Next() {
		var orderID int
		var amount float64
		if err := discountRows.Scan(&orderID, &amount); err != nil {
			return nil, err
		}
		discountTotals[orderID] = amount
	}
	if err := discountRows.Err(); err != nil {
		return nil, err
	}

	for i, o := range orders {
		if v, ok := co2Totals[o.OrderID]; ok {
			orders[i].CO2EqTotal = &v
		}
		if v, ok := weightTotals[o.OrderID]; ok {
			orders[i].WeightTotal = &v
		}
		if v, ok := discountTotals[o.OrderID]; ok {
			orders[i].DiscountTotal = &v
		}
	}
	return orders, nil
}

func getOrderDetail(db *sql.DB, orderID int) (*OrderDetail, error) {
	var d OrderDetail
	err := db.QueryRow(`
		SELECT order_id, delivery_date, COALESCE(total_price, 0),
		       COALESCE(delivery_method, ''), COALESCE(invoice_id, ''),
		       COALESCE(address_street, ''), COALESCE(address_number, ''),
		       COALESCE(address_extra, ''), COALESCE(address_postcode, ''),
		       COALESCE(address_city, '')
		FROM orders WHERE order_id = ?`, orderID).
		Scan(&d.OrderID, &d.DeliveryDate, &d.TotalPrice, &d.DeliveryMethod,
			&d.InvoiceID, &d.AddressStreet, &d.AddressNumber,
			&d.AddressExtra, &d.AddressPostcode, &d.AddressCity)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT oi.web_id,
		       COALESCE(oi.title, ''), COALESCE(oi.brand, ''),
		       COALESCE(p.ah_category, oi.category, ''), COALESCE(oi.sales_unit_size, ''),
		       oi.quantity, oi.allocated_qty, oi.unit_price, oi.was_price,
		       COALESCE(oi.image_url, ''),
		       COALESCE(pe.co2eq_category, ''), COALESCE(pe.co2eq_name, ''),
		       pe.co2eq_per_kg, pe.weight_kg,
		       COALESCE(oi.sales_unit_size, COALESCE(p.unit_size, ''))
		FROM order_items oi
		LEFT JOIN products p ON p.web_id = oi.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = oi.web_id
		WHERE oi.order_id = ?
		ORDER BY oi.category, oi.title`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	d.Items = make([]OrderItem, 0)
	for rows.Next() {
		var it OrderItem
		var webID sql.NullInt64
		var unitPrice, wasPrice, co2PerKg, weightKg sql.NullFloat64
		var unitSize string
		if err := rows.Scan(
			&webID, &it.Title, &it.Brand, &it.Category, &it.SalesUnitSize,
			&it.Quantity, &it.AllocatedQty, &unitPrice, &wasPrice, &it.ImageURL,
			&it.CO2EqCategory, &it.CO2EqName, &co2PerKg, &weightKg, &unitSize,
		); err != nil {
			return nil, err
		}
		if webID.Valid {
			v := int(webID.Int64)
			it.WebID = &v
		}
		if unitPrice.Valid {
			it.UnitPrice = &unitPrice.Float64
		}
		if wasPrice.Valid {
			it.WasPrice = &wasPrice.Float64
		}
		if co2PerKg.Valid {
			it.CO2EqPerKg = &co2PerKg.Float64
		}
		it.WeightPerUnitKg = weight.EffectiveKg(nullFloat64Ptr(weightKg), unitSize)
		it.CO2EqTotal = computeCO2Total(it.CO2EqPerKg, it.AllocatedQty, it.WeightPerUnitKg)
		if it.UnitPrice != nil {
			it.LineTotal = float64(it.AllocatedQty) * *it.UnitPrice
		}
		d.Items = append(d.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var totalCO2, totalSpent float64
	for _, it := range d.Items {
		if it.CO2EqTotal != nil {
			totalCO2 += *it.CO2EqTotal
		}
		totalSpent += it.LineTotal
	}
	if totalCO2 > 0 {
		d.CO2EqTotal = &totalCO2
	}
	if totalCO2 > 0 && totalSpent > 0 {
		v := totalCO2 / totalSpent
		d.CO2EqPerEuro = &v
	}
	return &d, nil
}

func search(db *sql.DB, q string) (*SearchResults, error) {
	like := "%" + q + "%"
	var res SearchResults

	pRows, err := db.Query(`
		SELECT p.web_id, COALESCE(p.thumbnail_url, ''), COALESCE(p.title, ''),
		       COALESCE(p.brand, ''), COALESCE(p.ah_category, ''), COALESCE(p.unit_size, '')
		FROM products p
		WHERE p.title LIKE ? OR p.brand LIKE ? OR p.ah_category LIKE ?
		ORDER BY p.title ASC
		LIMIT 50`, like, like, like)
	if err != nil {
		return nil, err
	}
	defer pRows.Close()
	res.Products = make([]SearchProduct, 0)
	for pRows.Next() {
		var p SearchProduct
		if err := pRows.Scan(&p.WebID, &p.ThumbnailURL, &p.Title, &p.Brand, &p.AHCategory, &p.UnitSize); err != nil {
			return nil, err
		}
		res.Products = append(res.Products, p)
	}
	if err := pRows.Err(); err != nil {
		return nil, err
	}

	rRows, err := db.Query(`
		SELECT r.date, r.transaction_id, i.description, i.quantity, i.amount
		FROM items i
		JOIN receipts r ON r.transaction_id = i.receipt_id
		WHERE i.description LIKE ?
		ORDER BY r.date DESC
		LIMIT 50`, like)
	if err != nil {
		return nil, err
	}
	defer rRows.Close()
	res.ReceiptItems = make([]SearchReceiptItem, 0)
	for rRows.Next() {
		var it SearchReceiptItem
		if err := rRows.Scan(&it.Date, &it.TransactionID, &it.Description, &it.Quantity, &it.Amount); err != nil {
			return nil, err
		}
		res.ReceiptItems = append(res.ReceiptItems, it)
	}
	if err := rRows.Err(); err != nil {
		return nil, err
	}

	oRows, err := db.Query(`
		SELECT o.delivery_date, o.order_id, COALESCE(oi.title, ''), COALESCE(oi.brand, ''),
		       oi.allocated_qty, ROUND(oi.allocated_qty * COALESCE(oi.unit_price, 0), 2)
		FROM order_items oi
		JOIN orders o ON o.order_id = oi.order_id
		WHERE oi.title LIKE ? OR oi.brand LIKE ?
		ORDER BY o.delivery_date DESC
		LIMIT 50`, like, like)
	if err != nil {
		return nil, err
	}
	defer oRows.Close()
	res.OrderItems = make([]SearchOrderItem, 0)
	for oRows.Next() {
		var it SearchOrderItem
		if err := oRows.Scan(&it.DeliveryDate, &it.OrderID, &it.Title, &it.Brand, &it.Quantity, &it.Amount); err != nil {
			return nil, err
		}
		res.OrderItems = append(res.OrderItems, it)
	}
	if err := oRows.Err(); err != nil {
		return nil, err
	}

	return &res, nil
}

func getProductStats(db *sql.DB, since *time.Time) ([]ProductStats, error) {
	itemsQ := `
		SELECT
			COALESCE(p.web_id, i.web_id) AS web_id,
			COALESCE(p.title, i.description) AS title,
			COALESCE(p.thumbnail_url, '') AS thumbnail_url,
			i.quantity, i.amount, pe.co2eq_per_kg, pe.weight_kg,
			COALESCE(p.unit_size, '') AS unit_size
		FROM items i
		LEFT JOIN products p ON p.web_id = i.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = i.web_id
		WHERE (p.web_id IS NOT NULL OR i.web_id IS NOT NULL)`

	ordersQ := `
		SELECT
			COALESCE(p.web_id, oi.web_id) AS web_id,
			COALESCE(p.title, oi.title) AS title,
			COALESCE(p.thumbnail_url, '') AS thumbnail_url,
			oi.allocated_qty AS quantity,
			ROUND(oi.allocated_qty * COALESCE(oi.unit_price, 0), 2) AS amount,
			pe.co2eq_per_kg, pe.weight_kg,
			COALESCE(oi.sales_unit_size, COALESCE(p.unit_size, '')) AS unit_size
		FROM order_items oi
		LEFT JOIN products p ON p.web_id = oi.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = oi.web_id`

	var args []any
	if since != nil {
		s := since.Format(dateISO)
		itemsQ += sqlSinceReceipt
		ordersQ += sqlSinceOrder
		args = append(args, s, s)
	}

	rows, err := db.Query(itemsQ+" UNION ALL "+ordersQ, args...)
	if err != nil {
		return nil, err
	}

	scan := func(rows *sql.Rows) (productStatsInput, error) {
		var row productStatsInput
		err := rows.Scan(&row.webID, &row.title, &row.thumbnailURL,
			&row.quantity, &row.amount, &row.co2PerKg, &row.weightKg, &row.unitSize)
		return row, err
	}
	keys, accs, err := accumulateProductRows(rows, scan)
	if err != nil {
		return nil, err
	}

	result := make([]ProductStats, 0, len(keys))
	for _, k := range keys {
		a := accs[k]
		ps := ProductStats{
			WebID:        k.webID,
			Title:        k.title,
			ThumbnailURL: a.thumbnailURL,
			TimesBought:  a.timesBought,
			TotalSpent:   a.totalSpent,
			TotalKg:      a.totalKg,
		}
		if a.hasCO2 {
			ps.CO2EqTotal = &a.co2EqTotal
		}
		result = append(result, ps)
	}
	return result, nil
}

func getNutriscoreDistribution(db *sql.DB, since *time.Time) ([]NutriscoreEntry, error) {
	itemsWhere := "i.web_id IS NOT NULL"
	ordersWhere := ""
	var args []any
	if since != nil {
		s := since.Format(dateISO)
		itemsWhere += sqlSinceReceipt
		ordersWhere = sqlSinceOrder
		args = append(args, s, s)
	}
	rows, err := db.Query(`
		WITH purchases AS (
			SELECT i.web_id, i.quantity
			FROM items i
			WHERE `+itemsWhere+`
			UNION ALL
			SELECT oi.web_id, oi.allocated_qty AS quantity
			FROM order_items oi`+ordersWhere+`
		)
		SELECT
			COALESCE(p.nutriscore, '') AS score,
			COUNT(DISTINCT purchases.web_id) AS count,
			SUM(purchases.quantity) AS times_bought
		FROM purchases
		LEFT JOIN products p ON p.web_id = purchases.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = purchases.web_id
		WHERE (pe.match_method IS NULL OR pe.match_method NOT IN (`+nonFoodMethods+`))
		  AND (p.ah_category IS NULL OR LOWER(p.ah_category) NOT IN ('drogisterij', 'huishouden', 'koken, tafelen, vrije tijd'))
		GROUP BY score
		ORDER BY score`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]NutriscoreEntry, 0)
	for rows.Next() {
		var e NutriscoreEntry
		if err := rows.Scan(&e.Score, &e.Count, &e.TimesBought); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

func getNutriscoreProducts(db *sql.DB, score string, since *time.Time) ([]NutriscoreProductStats, error) {
	itemsSince := ""
	ordersSince := ""
	args := []any{score, score}
	if since != nil {
		s := since.Format(dateISO)
		itemsSince = sqlSinceReceipt
		ordersSince = sqlSinceOrderAnd
		args = []any{score, s, score, s}
	}
	rows, err := db.Query(`
		SELECT
			COALESCE(p.web_id, i.web_id) AS web_id,
			COALESCE(p.title, i.description) AS title,
			COALESCE(p.thumbnail_url, '') AS thumbnail_url,
			COALESCE(p.nutriscore, '') AS nutriscore,
			i.quantity, i.amount, pe.co2eq_per_kg, pe.weight_kg,
			COALESCE(p.unit_size, '') AS unit_size
		FROM items i
		LEFT JOIN products p ON p.web_id = i.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = i.web_id
		WHERE (p.web_id IS NOT NULL OR i.web_id IS NOT NULL)
		  AND COALESCE(p.nutriscore, '') = ?
		  AND (pe.match_method IS NULL OR pe.match_method NOT IN (`+nonFoodMethods+`))
		  AND (p.ah_category IS NULL OR LOWER(p.ah_category) NOT IN ('drogisterij', 'huishouden', 'koken, tafelen, vrije tijd'))`+itemsSince+`

		UNION ALL

		SELECT
			COALESCE(p.web_id, oi.web_id) AS web_id,
			COALESCE(p.title, oi.title) AS title,
			COALESCE(p.thumbnail_url, '') AS thumbnail_url,
			COALESCE(p.nutriscore, '') AS nutriscore,
			oi.allocated_qty AS quantity,
			ROUND(oi.allocated_qty * COALESCE(oi.unit_price, 0), 2) AS amount,
			pe.co2eq_per_kg, pe.weight_kg,
			COALESCE(oi.sales_unit_size, COALESCE(p.unit_size, '')) AS unit_size
		FROM order_items oi
		LEFT JOIN products p ON p.web_id = oi.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = oi.web_id
		WHERE COALESCE(p.nutriscore, '') = ?
		  AND (pe.match_method IS NULL OR pe.match_method NOT IN (`+nonFoodMethods+`))
		  AND (p.ah_category IS NULL OR LOWER(p.ah_category) NOT IN ('drogisterij', 'huishouden', 'koken, tafelen, vrije tijd'))`+ordersSince, args...)
	if err != nil {
		return nil, err
	}

	scan := func(rows *sql.Rows) (productStatsInput, error) {
		var row productStatsInput
		err := rows.Scan(&row.webID, &row.title, &row.thumbnailURL, &row.nutriscore,
			&row.quantity, &row.amount, &row.co2PerKg, &row.weightKg, &row.unitSize)
		return row, err
	}
	keys, accs, err := accumulateProductRows(rows, scan)
	if err != nil {
		return nil, err
	}

	result := make([]NutriscoreProductStats, 0, len(keys))
	for _, k := range keys {
		a := accs[k]
		ps := NutriscoreProductStats{
			WebID:        k.webID,
			Title:        k.title,
			ThumbnailURL: a.thumbnailURL,
			Nutriscore:   a.nutriscore,
			TimesBought:  a.timesBought,
			TotalSpent:   a.totalSpent,
			TotalKg:      a.totalKg,
		}
		if a.hasCO2 {
			ps.CO2EqTotal = &a.co2EqTotal
		}
		result = append(result, ps)
	}
	return result, nil
}

func scanProductIssueItems(rows *sql.Rows) ([]ProductIssueItem, error) {
	defer rows.Close()
	result := make([]ProductIssueItem, 0)
	for rows.Next() {
		var it ProductIssueItem
		var posID sql.NullInt64
		var weightKg, co2PerKg sql.NullFloat64
		if err := rows.Scan(
			&it.WebID, &posID, &it.Title, &it.AHCategory,
			&it.AHSubcategory, &it.UnitSize,
			&weightKg, &it.CO2EqName, &it.CO2EqCategory, &co2PerKg,
			&it.POSDescription,
		); err != nil {
			return nil, err
		}
		if posID.Valid {
			v := int(posID.Int64)
			it.PosID = &v
		}
		if weightKg.Valid {
			it.WeightKg = &weightKg.Float64
		}
		if co2PerKg.Valid {
			it.CO2EqPerKg = &co2PerKg.Float64
		}
		result = append(result, it)
	}
	return result, rows.Err()
}

const sqlProductIssueSelect = `
	SELECT p.web_id, p.pos_id, COALESCE(p.title,''), COALESCE(p.ah_category,''),
	       COALESCE(p.ah_subcategory,''), COALESCE(p.unit_size,''),
	       pe.weight_kg, COALESCE(pe.co2eq_name,''), COALESCE(pe.co2eq_category,''), pe.co2eq_per_kg,
	       COALESCE((SELECT i.description FROM items i WHERE i.web_id = p.web_id LIMIT 1), '')
	FROM products p
	LEFT JOIN product_enrichment pe ON pe.web_id = p.web_id`

func getProductIssues(db *sql.DB) (*ProductIssues, error) {
	var res ProductIssues

	if err := db.QueryRow(`
		SELECT COUNT(DISTINCT bought.web_id)
		FROM (
			SELECT i.web_id FROM items i WHERE i.web_id IS NOT NULL
			UNION
			SELECT oi.web_id FROM order_items oi WHERE oi.web_id IS NOT NULL
		) bought
		LEFT JOIN product_enrichment pe ON pe.web_id = bought.web_id
		WHERE pe.match_method IS NULL OR pe.match_method NOT IN (` + nonFoodMethods + `)
	`).Scan(&res.Summary.TotalFoodProducts); err != nil {
		return nil, err
	}

	noWebIDRows, err := db.Query(`
		SELECT DISTINCT i.product_id, i.description
		FROM items i
		WHERE i.product_id IS NOT NULL AND i.web_id IS NULL
		ORDER BY i.description ASC`)
	if err != nil {
		return nil, err
	}
	res.NoWebID = make([]NoWebIDItem, 0)
	for noWebIDRows.Next() {
		var it NoWebIDItem
		if err := noWebIDRows.Scan(&it.PosID, &it.Description); err != nil {
			noWebIDRows.Close()
			return nil, err
		}
		res.NoWebID = append(res.NoWebID, it)
	}
	noWebIDRows.Close()
	if err := noWebIDRows.Err(); err != nil {
		return nil, err
	}
	res.Summary.NoWebID = len(res.NoWebID)

	noPosIDRows, err := db.Query(sqlProductIssueSelect + `
		WHERE p.pos_id IS NULL
		  AND p.title IS NOT NULL AND TRIM(p.title) != ''
		  AND (pe.match_method IS NULL OR pe.match_method NOT IN (` + nonFoodMethods + `))
		ORDER BY p.title ASC`)
	if err != nil {
		return nil, err
	}
	if res.NoPosID, err = scanProductIssueItems(noPosIDRows); err != nil {
		return nil, err
	}
	res.Summary.NoPosID = len(res.NoPosID)

	// no_metadata is intentionally NOT excluded here — those items are exactly what this list shows.
	noDataRows, err := db.Query(sqlProductIssueSelect + `
		WHERE (p.title IS NULL OR TRIM(p.title) = '')
		  AND (pe.match_method IS NULL OR pe.match_method NOT IN (` + nonFoodExceptNoMetadata + `))
		ORDER BY p.web_id ASC`)
	if err != nil {
		return nil, err
	}
	if res.NoProductData, err = scanProductIssueItems(noDataRows); err != nil {
		return nil, err
	}
	res.Summary.NoProductData = len(res.NoProductData)

	noWeightRows, err := db.Query(`
		SELECT p.web_id, p.pos_id, COALESCE(p.title,''), COALESCE(p.ah_category,''),
		       COALESCE(p.ah_subcategory,''), COALESCE(p.unit_size,''),
		       pe.weight_kg, COALESCE(pe.co2eq_name,''), COALESCE(pe.co2eq_category,''), pe.co2eq_per_kg,
		       COALESCE((SELECT i.description FROM items i WHERE i.web_id = p.web_id LIMIT 1), '')
		FROM product_enrichment pe
		JOIN products p ON p.web_id = pe.web_id
		WHERE pe.co2eq_per_kg IS NOT NULL
		  AND pe.match_method NOT IN (` + nonFoodAndUnmatchedMethods + `)
		  AND (pe.weight_kg IS NULL OR pe.weight_kg = 0)
		ORDER BY p.title ASC`)
	if err != nil {
		return nil, err
	}
	noWeight, err := scanProductIssueItems(noWeightRows)
	if err != nil {
		return nil, err
	}
	res.NoWeight = dropResolvableWeights(noWeight)
	res.Summary.NoWeight = len(res.NoWeight)

	if res.UnmatchedSubcategories, err = getUnmatchedSubcategories(db); err != nil {
		return nil, err
	}
	res.Summary.UnmatchedSubcategories = len(res.UnmatchedSubcategories)

	noSubcatRows, err := db.Query(sqlProductIssueSelect + `
		WHERE pe.match_method = '` + schema.MatchMethodUnmatched + `'
		  AND TRIM(COALESCE(p.ah_subcategory, '')) = ''
		ORDER BY p.title ASC`)
	if err != nil {
		return nil, err
	}
	if res.UnmatchedNoSubcategory, err = scanProductIssueItems(noSubcatRows); err != nil {
		return nil, err
	}
	res.Summary.UnmatchedNoSubcategory = len(res.UnmatchedNoSubcategory)

	return &res, nil
}

// dropResolvableWeights keeps only items that truly lack a weight: a product with
// a NULL/0 weight_kg still resolves at query time if its unit_size parses (no 1 kg
// fallback), so those are dropped from the no-weight issue list.
func dropResolvableWeights(items []ProductIssueItem) []ProductIssueItem {
	out := make([]ProductIssueItem, 0, len(items))
	for _, it := range items {
		if unitSizeYieldsWeight(it.UnitSize) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// maxUnmatchedExampleTitles caps how many example product titles are returned per
// unmatched subcategory — enough to judge whether it is food without bloating the payload.
const maxUnmatchedExampleTitles = 3

// getUnmatchedSubcategories lists the distinct AH subcategories whose products
// land in match_method='unmatched' (a food category, but the subcategory is not
// in ah_subcategory_map.csv), with the number of affected products and a few
// example titles. Each row is one mapping decision; ordered by impact.
func getUnmatchedSubcategories(db *sql.DB) ([]UnmatchedSubcategory, error) {
	rows, err := db.Query(`
		SELECT COALESCE(p.ah_category, ''), p.ah_subcategory,
		       COUNT(DISTINCT p.web_id),
		       GROUP_CONCAT(DISTINCT COALESCE(p.title, ''))
		FROM products p
		JOIN product_enrichment pe ON pe.web_id = p.web_id
		WHERE pe.match_method = '` + schema.MatchMethodUnmatched + `'
		  AND TRIM(COALESCE(p.ah_subcategory, '')) != ''
		GROUP BY p.ah_category, p.ah_subcategory
		ORDER BY COUNT(DISTINCT p.web_id) DESC, p.ah_subcategory ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]UnmatchedSubcategory, 0)
	for rows.Next() {
		var it UnmatchedSubcategory
		var titles sql.NullString
		if err := rows.Scan(&it.AHCategory, &it.AHSubcategory, &it.ProductCount, &titles); err != nil {
			return nil, err
		}
		it.ExampleTitles = exampleTitles(titles.String, maxUnmatchedExampleTitles)
		result = append(result, it)
	}
	return result, rows.Err()
}

// exampleTitles splits a GROUP_CONCAT result, dropping blanks, and returns at most n entries.
func exampleTitles(concatenated string, n int) []string {
	out := make([]string, 0, n)
	for _, t := range strings.Split(concatenated, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		out = append(out, t)
		if len(out) == n {
			break
		}
	}
	return out
}

func getMissingCategory(db *sql.DB) ([]MissingCategoryItem, error) {
	rows, err := db.Query(`
		SELECT pe.web_id, COALESCE(p.title, ''), COALESCE(p.ah_category, ''),
		       COALESCE(p.unit_size, ''), COALESCE(pe.co2eq_name, ''),
		       pe.co2eq_per_kg, pe.weight_kg, COALESCE(pe.match_method, '')
		FROM product_enrichment pe
		JOIN products p ON p.web_id = pe.web_id
		WHERE pe.match_method NOT IN (` + nonFoodMethods + `)
		  AND (pe.co2eq_per_kg IS NULL OR pe.match_method = '` + schema.MatchMethodUnmatched + `')
		ORDER BY p.title ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]MissingCategoryItem, 0)
	for rows.Next() {
		var it MissingCategoryItem
		var co2PerKg, weightKg sql.NullFloat64
		if err := rows.Scan(
			&it.WebID, &it.Title, &it.AHCategory, &it.UnitSize,
			&it.CO2EqName, &co2PerKg, &weightKg, &it.MatchMethod,
		); err != nil {
			return nil, err
		}
		if co2PerKg.Valid {
			it.CO2EqPerKg = &co2PerKg.Float64
		}
		if weightKg.Valid {
			it.WeightKg = &weightKg.Float64
		}
		result = append(result, it)
	}
	return result, rows.Err()
}

func getMissingWeight(db *sql.DB) ([]MissingWeightItem, error) {
	rows, err := db.Query(`
		SELECT pe.web_id, COALESCE(p.title, ''), COALESCE(p.unit_size, ''),
		       COALESCE(pe.co2eq_name, ''), COALESCE(pe.co2eq_category, ''),
		       pe.co2eq_per_kg, pe.weight_kg
		FROM product_enrichment pe
		JOIN products p ON p.web_id = pe.web_id
		WHERE pe.co2eq_per_kg IS NOT NULL
		  AND pe.match_method NOT IN (` + nonFoodAndUnmatchedMethods + `)
		  AND (pe.weight_kg IS NULL OR pe.weight_kg = 0)
		ORDER BY p.title ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]MissingWeightItem, 0)
	for rows.Next() {
		var it MissingWeightItem
		var co2PerKg, weightKg sql.NullFloat64
		if err := rows.Scan(
			&it.WebID, &it.Title, &it.UnitSize, &it.CO2EqName, &it.CO2EqCategory,
			&co2PerKg, &weightKg,
		); err != nil {
			return nil, err
		}
		// weight_kg is NULL/0 here; the product is only truly missing a weight if
		// its unit_size yields nothing either ("500 g"/"los per 500 g" would
		// resolve at query time and are not listed).
		if unitSizeYieldsWeight(it.UnitSize) {
			continue
		}
		if co2PerKg.Valid {
			it.CO2EqPerKg = &co2PerKg.Float64
		}
		if weightKg.Valid {
			it.WeightKg = &weightKg.Float64
		}
		result = append(result, it)
	}
	return result, rows.Err()
}

func getProductDetail(db *sql.DB, webID int) (*ProductDetail, error) {
	var p ProductDetail
	var co2PerKg, weightKg sql.NullFloat64
	var posID sql.NullInt64
	var weightSource sql.NullString
	err := db.QueryRow(`
		SELECT
			p.web_id,
			COALESCE(p.thumbnail_url, ''), COALESCE(p.title, ''), COALESCE(p.brand, ''),
			COALESCE(p.ah_category, ''), COALESCE(p.ah_subcategory, ''),
			COALESCE(p.unit_size, ''), COALESCE(p.nutriscore, ''),
			COALESCE(p.unit_price_description, ''), COALESCE(p.property_icons, ''),
			COALESCE(p.net_content, ''), COALESCE(p.serving_size, ''),
			pe.co2eq_per_kg, COALESCE(pe.co2eq_category, ''),
			p.pos_id,
			COALESCE(pe.co2eq_name, ''), COALESCE(pe.match_method, ''),
			pe.weight_kg, pe.weight_source
		FROM products p
		LEFT JOIN product_enrichment pe ON pe.web_id = p.web_id
		WHERE p.web_id = ?`, webID).
		Scan(
			&p.WebID, &p.ThumbnailURL, &p.Title, &p.Brand,
			&p.AHCategory, &p.AHSubcategory, &p.UnitSize, &p.Nutriscore,
			&p.UnitPriceDescription, &p.PropertyIcons,
			&p.NetContent, &p.ServingSize,
			&co2PerKg, &p.CO2EqCategory,
			&posID, &p.CO2EqName, &p.MatchMethod, &weightKg, &weightSource,
		)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if co2PerKg.Valid {
		p.CO2EqPerKg = &co2PerKg.Float64
	}
	if weightKg.Valid {
		p.WeightKg = &weightKg.Float64
	}
	if posID.Valid {
		v := int(posID.Int64)
		p.PosID = &v
	}
	p.WeightSource = weightSource.String
	p.WeightPerUnitKg = weight.EffectiveKg(p.WeightKg, p.UnitSize)
	// When no source was stored but a weight still resolved, it came from the
	// query-time unit_size fallback in weight.EffectiveKg — label it so for display.
	if p.WeightSource == "" && p.WeightPerUnitKg != nil {
		p.WeightSource = weight.SourceUnitSize
	}
	p.WeightBreakdown = buildWeightBreakdown(&p)
	return &p, nil
}

// buildWeightBreakdown computes the per-source candidate weights shown on the
// product page, iterating weight.SourcePriority so the order matches enrichment.
// The measured sources (unit size, net content, serving size) are recomputed from
// the product's own fields via the shared weight parsers; the enrichment-only
// sources (correction, multipack, default) can't be recomputed at query time, so
// they show the stored weight when they are the active source. The entry matching
// p.WeightSource is marked Active.
func buildWeightBreakdown(p *ProductDetail) []WeightSourceValue {
	parsed := func(s string) *float64 {
		if kg, ok := weight.ParseKg(s); ok {
			return &kg
		}
		return nil
	}
	// storedIf returns the stored weight when src is the active source, so that
	// enrichment-only sources still display the value they contributed.
	storedIf := func(src string) *float64 {
		if p.WeightSource == src {
			return p.WeightKg
		}
		return nil
	}
	candidate := func(src string) *float64 {
		switch src {
		case weight.SourceUnitSize:
			return parsed(p.UnitSize)
		case weight.SourceNetContent:
			return parsed(p.NetContent)
		case weight.SourceServingSize:
			if kg, ok := weight.ServingSizeKg(p.UnitSize, p.ServingSize); ok {
				return &kg
			}
			return nil
		default: // correction, multipack, default — derivable only at enrichment time
			return storedIf(src)
		}
	}

	breakdown := make([]WeightSourceValue, 0, len(weight.SourcePriority))
	for _, src := range weight.SourcePriority {
		breakdown = append(breakdown, WeightSourceValue{
			Source:  src,
			ValueKg: candidate(src),
			Active:  src == p.WeightSource,
		})
	}
	return breakdown
}

func getProductPurchases(db *sql.DB, webID int) ([]ProductPurchase, error) {
	result := make([]ProductPurchase, 0)

	rRows, err := db.Query(`
		SELECT DATE(r.date), i.description, i.quantity, i.amount,
		       ROUND(CAST(i.amount AS REAL) / NULLIF(i.quantity, 0), 2),
		       'receipt'
		FROM items i
		JOIN receipts r ON r.transaction_id = i.receipt_id
		WHERE i.web_id = ?
		ORDER BY r.date DESC`, webID)
	if err != nil {
		return nil, err
	}
	defer rRows.Close()
	for rRows.Next() {
		var pp ProductPurchase
		var unitPrice sql.NullFloat64
		if err := rRows.Scan(&pp.Date, &pp.Description, &pp.Quantity, &pp.Amount, &unitPrice, &pp.Source); err != nil {
			return nil, err
		}
		if unitPrice.Valid {
			pp.UnitPrice = &unitPrice.Float64
		}
		result = append(result, pp)
	}
	if err := rRows.Err(); err != nil {
		return nil, err
	}

	oRows, err := db.Query(`
		SELECT o.delivery_date, oi.title, oi.allocated_qty,
		       ROUND(oi.allocated_qty * COALESCE(oi.unit_price, 0), 2),
		       oi.unit_price, 'order'
		FROM order_items oi
		JOIN orders o ON o.order_id = oi.order_id
		WHERE oi.web_id = ?
		ORDER BY o.delivery_date DESC`, webID)
	if err != nil {
		return nil, err
	}
	defer oRows.Close()
	for oRows.Next() {
		var pp ProductPurchase
		var unitPrice sql.NullFloat64
		if err := oRows.Scan(&pp.Date, &pp.Description, &pp.Quantity, &pp.Amount, &unitPrice, &pp.Source); err != nil {
			return nil, err
		}
		if unitPrice.Valid {
			pp.UnitPrice = &unitPrice.Float64
		}
		result = append(result, pp)
	}
	return result, oRows.Err()
}

func getProductByPosID(db *sql.DB, posID int) (*PosProductInfo, error) {
	var info PosProductInfo
	info.PosID = posID

	var webID sql.NullInt64
	err := db.QueryRow(`
		SELECT i.description, i.web_id
		FROM items i
		WHERE i.product_id = ?
		LIMIT 1`, posID).Scan(&info.Description, &webID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if webID.Valid {
		v := int(webID.Int64)
		info.WebID = &v
		_ = db.QueryRow(`SELECT COALESCE(title,''), COALESCE(thumbnail_url,'') FROM products WHERE web_id = ?`, v).
			Scan(&info.Title, &info.ThumbnailURL)
		var n int
		if db.QueryRow(`SELECT COUNT(*) FROM product_not_found WHERE web_id = ?`, v).Scan(&n) == nil {
			info.InNotFound = n > 0
		}
	}

	return &info, nil
}

func resetDatabase(db *sql.DB) error {
	for _, t := range []string{"product_enrichment", "items", "receipts", "order_items", "orders", "products"} {
		if _, err := db.Exec("DELETE FROM " + t); err != nil {
			slog.Warn("resetDatabase: could not clear table", "table", t, "err", err)
		}
	}
	return nil
}

func getEnrichmentCount(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow("SELECT COUNT(*) FROM product_enrichment").Scan(&n)
	return n, err
}

func getPendingEnrichmentCount(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT web_id FROM items       WHERE web_id IS NOT NULL
			UNION
			SELECT web_id FROM order_items WHERE web_id IS NOT NULL
		) WHERE web_id NOT IN (SELECT web_id FROM product_enrichment)`).Scan(&n)
	return n, err
}

func clearAllEnrichment(db *sql.DB) error {
	_, err := db.Exec("DELETE FROM product_enrichment")
	return err
}

func clearProductEnrichment(db *sql.DB, webID int) (int, error) {
	res, err := db.Exec("DELETE FROM product_enrichment WHERE web_id = ?", webID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func getFinancialSummary(db *sql.DB, since *time.Time) (*FinancialSummary, error) {
	receiptWhere := ""
	orderWhere := ""
	var spendArgs []any
	var discountArgs []any
	if since != nil {
		s := since.Format(dateISO)
		receiptWhere = " WHERE DATE(date) >= ?"
		orderWhere = " WHERE DATE(delivery_date) >= ?"
		spendArgs = append(spendArgs, s, s)
		discountArgs = append(discountArgs, s)
	}

	var totalSpent float64
	var firstDate, lastDate string
	err := db.QueryRow(`
		WITH all_purchases AS (
			SELECT date AS dt, total_amount AS amount FROM receipts`+receiptWhere+`
			UNION ALL
			SELECT delivery_date AS dt, total_price AS amount FROM orders`+orderWhere+`
		)
		SELECT
			COALESCE(SUM(amount), 0),
			COALESCE(MIN(dt), ''),
			COALESCE(MAX(dt), '')
		FROM all_purchases`, spendArgs...).Scan(&totalSpent, &firstDate, &lastDate)
	if err != nil {
		return nil, err
	}

	discountQuery := `SELECT COALESCE(SUM(rd.amount), 0) FROM receipt_discounts rd`
	if since != nil {
		discountQuery += ` JOIN receipts r ON rd.receipt_id = r.transaction_id WHERE DATE(r.date) >= ?`
	}
	var totalDiscount float64
	if err := db.QueryRow(discountQuery, discountArgs...).Scan(&totalDiscount); err != nil {
		return nil, err
	}

	s := &FinancialSummary{
		TotalSpent:    totalSpent,
		TotalDiscount: totalDiscount,
		FirstDate:     firstDate,
		LastDate:      lastDate,
	}

	// Compute averages from date range.
	if firstDate != "" && lastDate != "" && firstDate != lastDate {
		var days float64
		if err := db.QueryRow(`
			SELECT CAST(julianday(?) - julianday(?) AS REAL)`, lastDate, firstDate).Scan(&days); err == nil && days > 0 {
			s.AvgPerWeek = totalSpent / (days / 7)
			s.AvgPerMonth = totalSpent / (days / 30.4375)
			s.AvgPerYear = totalSpent / (days / 365.25)
			s.DiscountAvgPerWeek = totalDiscount / (days / 7)
			s.DiscountAvgPerMonth = totalDiscount / (days / 30.4375)
			s.DiscountAvgPerYear = totalDiscount / (days / 365.25)
		}
	}

	return s, nil
}

func getSpendingByCategory(db *sql.DB, since *time.Time) ([]CategorySpending, error) {
	receiptWhere := "i.amount > 0"
	orderWhere := ""
	var args []any
	if since != nil {
		s := since.Format(dateISO)
		receiptWhere += sqlSinceReceipt
		orderWhere = sqlSinceOrder
		args = append(args, s, s)
	}
	rows, err := db.Query(`
		WITH receipt_spending AS (
			SELECT
				COALESCE(NULLIF(p.ah_category, ''), 'Onbekend') AS category,
				COALESCE(NULLIF(p.ah_subcategory, ''), 'Onbekend') AS subcategory,
				SUM(i.amount) AS total_spent
			FROM items i
			LEFT JOIN products p ON i.web_id = p.web_id
			WHERE `+receiptWhere+`
			GROUP BY p.ah_category, p.ah_subcategory
		),
		order_spending AS (
			SELECT
				COALESCE(NULLIF(p.ah_category, ''), NULLIF(oi.category, ''), 'Onbekend') AS category,
				COALESCE(NULLIF(p.ah_subcategory, ''), 'Onbekend') AS subcategory,
				SUM(COALESCE(oi.unit_price, 0) * oi.quantity) AS total_spent
			FROM order_items oi
			LEFT JOIN products p ON oi.web_id = p.web_id`+orderWhere+`
			GROUP BY category, subcategory
		)
		SELECT category, subcategory, SUM(total_spent) AS total_spent
		FROM (SELECT * FROM receipt_spending UNION ALL SELECT * FROM order_spending)
		GROUP BY category, subcategory
		ORDER BY total_spent DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []CategorySpending
	for rows.Next() {
		var c CategorySpending
		if err := rows.Scan(&c.Category, &c.Subcategory, &c.TotalSpent); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// validPeriods maps period query param to SQLite strftime format.
// "quarter" is handled separately because SQLite has no native quarter format.
var validPeriods = map[string]string{
	"day":   "%Y-%m-%d",
	"week":  "%Y-W%W",
	"month": "%Y-%m",
	"year":  "%Y",
}

// quarterExpr is the SQLite expression that produces a "YYYYQn" label for a date column.
func quarterExpr(dateCol string) string {
	return fmt.Sprintf(
		"(strftime('%%Y', DATE(%s)) || 'Q' || ((CAST(strftime('%%m', DATE(%s)) AS INTEGER) - 1) / 3 + 1))",
		dateCol, dateCol,
	)
}

func getTopDiscounts(db *sql.DB, since *time.Time) ([]DiscountStats, error) {
	q := `
		SELECT rd.name, ABS(SUM(rd.amount)) AS total_discount
		FROM receipt_discounts rd`
	var args []any
	if since != nil {
		q += " JOIN receipts r ON rd.receipt_id = r.transaction_id WHERE DATE(r.date) >= ?"
		args = append(args, since.Format(dateISO))
	}
	q += " GROUP BY rd.name ORDER BY total_discount DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]DiscountStats, 0)
	for rows.Next() {
		var d DiscountStats
		if err := rows.Scan(&d.Name, &d.TotalDiscount); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func getSpendingOverTime(db *sql.DB, period string, since *time.Time) ([]PeriodSpending, error) {
	receiptWhere := ""
	orderWhere := ""
	var args []any
	if since != nil {
		s := since.Format(dateISO)
		receiptWhere = " WHERE DATE(r.date) >= ?"
		orderWhere = " WHERE DATE(o.delivery_date) >= ?"
		args = append(args, s, s)
	}

	var receiptPeriod, orderPeriod string
	if period == "quarter" {
		receiptPeriod = quarterExpr("r.date")
		orderPeriod = quarterExpr("o.delivery_date")
	} else {
		format, ok := validPeriods[period]
		if !ok {
			format = validPeriods["month"]
		}
		receiptPeriod = "strftime('" + format + "', r.date)"
		orderPeriod = "strftime('" + format + "', o.delivery_date)"
	}

	rows, err := db.Query(`
		SELECT period, SUM(amount) AS amount, SUM(discount) AS discount FROM (
			SELECT `+receiptPeriod+` AS period, r.total_amount AS amount,
				COALESCE((
					SELECT ABS(SUM(rd.amount)) FROM receipt_discounts rd WHERE rd.receipt_id = r.transaction_id
				), 0) AS discount
			FROM receipts r`+receiptWhere+`
			UNION ALL
			SELECT `+orderPeriod+` AS period, o.total_price AS amount, 0 AS discount
			FROM orders o`+orderWhere+`
		)
		GROUP BY period
		ORDER BY period`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PeriodSpending
	for rows.Next() {
		var p PeriodSpending
		if err := rows.Scan(&p.Period, &p.Amount, &p.Discount); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// ---------------------------------------------------------------------------
// Sustainability
// ---------------------------------------------------------------------------

type rawCO2Row struct {
	date     string
	category string
	qty      int
	co2PerKg sql.NullFloat64
	weightKg sql.NullFloat64
	unitSize string
}

// fetchCO2Rows returns enriched CO2 data for both receipt and order items,
// optionally filtered to records on or after since.
func fetchCO2Rows(db *sql.DB, since *time.Time) ([]rawCO2Row, error) {
	rQ := `
		SELECT r.date, COALESCE(pe.co2eq_category,''), i.quantity,
		       pe.co2eq_per_kg, pe.weight_kg, COALESCE(p.unit_size,'')
		FROM items i
		JOIN receipts r ON r.transaction_id = i.receipt_id
		LEFT JOIN products p ON p.web_id = i.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = i.web_id
		WHERE pe.co2eq_per_kg IS NOT NULL`
	oQ := `
		SELECT o.delivery_date, COALESCE(pe.co2eq_category,''), oi.allocated_qty,
		       pe.co2eq_per_kg, pe.weight_kg,
		       COALESCE(oi.sales_unit_size, COALESCE(p.unit_size,''))
		FROM order_items oi
		JOIN orders o ON o.order_id = oi.order_id
		LEFT JOIN products p ON p.web_id = oi.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = oi.web_id
		WHERE pe.co2eq_per_kg IS NOT NULL`
	var rArgs, oArgs []any
	if since != nil {
		s := since.Format(dateISO)
		rQ += " AND DATE(r.date) >= ?"
		oQ += " AND DATE(o.delivery_date) >= ?"
		rArgs = append(rArgs, s)
		oArgs = append(oArgs, s)
	}
	scan := func(rows *sql.Rows) ([]rawCO2Row, error) {
		defer rows.Close()
		var out []rawCO2Row
		for rows.Next() {
			var row rawCO2Row
			if err := rows.Scan(&row.date, &row.category, &row.qty,
				&row.co2PerKg, &row.weightKg, &row.unitSize); err != nil {
				return nil, err
			}
			out = append(out, row)
		}
		return out, rows.Err()
	}
	rRows, err := db.Query(rQ, rArgs...)
	if err != nil {
		return nil, err
	}
	result, err := scan(rRows)
	if err != nil {
		return nil, err
	}
	oRows, err := db.Query(oQ, oArgs...)
	if err != nil {
		return nil, err
	}
	oResult, err := scan(oRows)
	if err != nil {
		return nil, err
	}
	return append(result, oResult...), nil
}

// parseDateStr parses a SQLite date string in several common formats.
func parseDateStr(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date: %q", s)
}

// toPeriodLabel converts a time to the label string for the given period type.
// Supported: day, week (ISO Monday date), month, quarter, year.
func toPeriodLabel(t time.Time, period string) string {
	switch period {
	case "day":
		return t.Format("2006-01-02")
	case "week":
		wd := int(t.Weekday())
		if wd == 0 {
			wd = 7
		}
		monday := t.AddDate(0, 0, -(wd - 1))
		return monday.Format("2006-01-02")
	case "month":
		return t.Format("2006-01")
	case "quarter":
		q := (int(t.Month())-1)/3 + 1
		return fmt.Sprintf("%dQ%d", t.Year(), q)
	case "year":
		return t.Format("2006")
	default:
		return t.Format("2006-01")
	}
}

// periodFilterExpr returns a SQL boolean expression that matches dateCol to a
// period label produced by toPeriodLabel for the given period type.
func periodFilterExpr(dateCol, period string) string {
	switch period {
	case "day":
		return fmt.Sprintf("strftime('%%Y-%%m-%%d', DATE(%s)) = ?", dateCol)
	case "week":
		return fmt.Sprintf(
			"date(DATE(%s), '-' || ((strftime('%%w', DATE(%s)) + 6) %% 7) || ' days') = ?",
			dateCol, dateCol)
	case "month":
		return fmt.Sprintf("strftime('%%Y-%%m', DATE(%s)) = ?", dateCol)
	case "quarter":
		return fmt.Sprintf(
			"(strftime('%%Y', DATE(%s)) || 'Q' || ((CAST(strftime('%%m', DATE(%s)) AS INTEGER) - 1) / 3 + 1)) = ?",
			dateCol, dateCol)
	case "year":
		return fmt.Sprintf("strftime('%%Y', DATE(%s)) = ?", dateCol)
	default:
		return fmt.Sprintf("strftime('%%Y-%%m', DATE(%s)) = ?", dateCol)
	}
}

func getSustainabilitySummary(db *sql.DB, since *time.Time, householdAE float64) (*SustainabilitySummary, error) {
	rows, err := fetchCO2Rows(db, since)
	if err != nil {
		return nil, err
	}
	monthlyTotals := map[string]float64{}
	categoryTotals := map[string]float64{}
	for _, row := range rows {
		t, err := parseDateStr(row.date)
		if err != nil {
			slog.Warn("sustainability: skip unparseable date", "date", row.date)
			continue
		}
		w := weight.EffectiveKg(nullFloat64Ptr(row.weightKg), row.unitSize)
		co2 := computeCO2Total(nullFloat64Ptr(row.co2PerKg), row.qty, w)
		if co2 == nil {
			continue
		}
		month := t.Format("2006-01")
		monthlyTotals[month] += *co2
		if row.category != "" {
			categoryTotals[row.category] += *co2
		}
	}
	if len(monthlyTotals) == 0 {
		return &SustainabilitySummary{}, nil
	}
	var sum float64
	for _, v := range monthlyTotals {
		sum += v
	}

	// Compute a fractional month count so that partial months at the start/end
	// of the selected period don't distort the average. Without this, "Last 3 months"
	// starting on Feb 28 produces 4 calendar-month entries (Feb, Mar, Apr, May) even
	// though the period is only ~3 months, making the average appear ~25% lower than
	// it actually is and causing the grade to disagree with the chart reference line.
	now := time.Now().UTC().Truncate(24 * time.Hour)
	var numMonths float64
	for month := range monthlyTotals {
		monthStart, _ := time.Parse(dateISO, month+"-01")
		monthEnd := monthStart.AddDate(0, 1, 0)

		effectiveStart := monthStart
		if since != nil && since.After(monthStart) {
			effectiveStart = *since
		}
		effectiveEnd := monthEnd
		if tomorrow := now.AddDate(0, 0, 1); tomorrow.Before(monthEnd) {
			effectiveEnd = tomorrow
		}

		totalDays := monthEnd.Sub(monthStart).Hours() / 24
		activeDays := effectiveEnd.Sub(effectiveStart).Hours() / 24
		if totalDays > 0 && activeDays > 0 {
			numMonths += activeDays / totalDays
		}
	}
	if numMonths <= 0 {
		numMonths = 1
	}

	avgPerMonth := sum / numMonths
	avgPerAE := avgPerMonth / math.Max(householdAE, 1.0)
	sus := refSustainableKgPerPersonMonth
	avgRef := refAvgKgPerPersonMonth
	var grade string
	switch {
	case avgPerAE <= sus:
		grade = "A"
	case avgPerAE <= sus+(avgRef-sus)*0.33:
		grade = "B"
	case avgPerAE <= avgRef:
		grade = "C"
	case avgPerAE <= avgRef*1.2:
		grade = "D"
	default:
		grade = "E"
	}
	pctAbove := (avgPerAE - sus) / sus * 100
	var topCat string
	var topCO2 float64
	for cat, co2 := range categoryTotals {
		if co2 > topCO2 {
			topCO2 = co2
			topCat = cat
		}
	}
	return &SustainabilitySummary{
		Grade:               grade,
		PctAboveSustainable: &pctAbove,
		TopCategory:         topCat,
		AvgKgPerAePerMonth:  &avgPerAE,
	}, nil
}

func getSustainabilityTrend(db *sql.DB, period string, since *time.Time) ([]TrendEntry, error) {
	rows, err := fetchCO2Rows(db, since)
	if err != nil {
		return nil, err
	}
	type key struct{ period, category string }
	totals := map[key]float64{}
	for _, row := range rows {
		t, err := parseDateStr(row.date)
		if err != nil {
			continue
		}
		if row.category == "" {
			continue
		}
		w := weight.EffectiveKg(nullFloat64Ptr(row.weightKg), row.unitSize)
		co2 := computeCO2Total(nullFloat64Ptr(row.co2PerKg), row.qty, w)
		if co2 == nil {
			continue
		}
		totals[key{toPeriodLabel(t, period), row.category}] += *co2
	}
	result := make([]TrendEntry, 0, len(totals))
	for k, v := range totals {
		result = append(result, TrendEntry{Period: k.period, Category: k.category, CO2Eq: v})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Period != result[j].Period {
			return result[i].Period < result[j].Period
		}
		return result[i].Category < result[j].Category
	})
	return result, nil
}

func getSustainabilityCategories(db *sql.DB) ([]CategoryCO2, error) {
	rows, err := fetchCO2Rows(db, nil)
	if err != nil {
		return nil, err
	}
	totals := map[string]float64{}
	for _, row := range rows {
		if row.category == "" {
			continue
		}
		w := weight.EffectiveKg(nullFloat64Ptr(row.weightKg), row.unitSize)
		co2 := computeCO2Total(nullFloat64Ptr(row.co2PerKg), row.qty, w)
		if co2 == nil {
			continue
		}
		totals[row.category] += *co2
	}
	result := make([]CategoryCO2, 0, len(totals))
	for cat, co2 := range totals {
		result = append(result, CategoryCO2{Category: cat, CO2Eq: co2})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CO2Eq > result[j].CO2Eq
	})
	return result, nil
}

// getCategoryProducts returns individual item rows for a given CO₂ category,
// optionally filtered to a specific period. Pass empty periodType/periodLabel
// for an all-time result. percentage_of_category is set on each row.
func getCategoryProducts(db *sql.DB, category, periodType, periodLabel string) ([]CategoryProduct, error) {
	rFilter := periodFilterExpr("r.date", periodType)
	oFilter := periodFilterExpr("o.delivery_date", periodType)

	rQ := `
		SELECT i.description, COALESCE(p.title,''), COALESCE(pe.co2eq_name,''),
		       i.quantity, i.amount, pe.co2eq_per_kg, pe.weight_kg,
		       COALESCE(p.unit_size,''), i.web_id
		FROM items i
		JOIN receipts r ON r.transaction_id = i.receipt_id
		LEFT JOIN products p ON p.web_id = i.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = i.web_id
		WHERE pe.co2eq_category = ? AND pe.co2eq_per_kg IS NOT NULL`
	oQ := `
		SELECT oi.title, COALESCE(p.title,''), COALESCE(pe.co2eq_name,''),
		       oi.allocated_qty,
		       ROUND(oi.allocated_qty * COALESCE(oi.unit_price,0), 2),
		       pe.co2eq_per_kg, pe.weight_kg,
		       COALESCE(oi.sales_unit_size, COALESCE(p.unit_size,'')), oi.web_id
		FROM order_items oi
		JOIN orders o ON o.order_id = oi.order_id
		LEFT JOIN products p ON p.web_id = oi.web_id
		LEFT JOIN product_enrichment pe ON pe.web_id = oi.web_id
		WHERE pe.co2eq_category = ? AND pe.co2eq_per_kg IS NOT NULL`
	var rArgs, oArgs []any
	rArgs = append(rArgs, category)
	oArgs = append(oArgs, category)
	if periodLabel != "" {
		rQ += " AND " + rFilter
		oQ += " AND " + oFilter
		rArgs = append(rArgs, periodLabel)
		oArgs = append(oArgs, periodLabel)
	}

	scan := func(rows *sql.Rows) ([]CategoryProduct, error) {
		defer rows.Close()
		var out []CategoryProduct
		for rows.Next() {
			var p CategoryProduct
			var webID sql.NullInt64
			var co2PerKg, weightKg sql.NullFloat64
			var unitSize string
			if err := rows.Scan(&p.Description, &p.WebTitle, &p.CO2EqName,
				&p.Quantity, &p.Amount, &co2PerKg, &weightKg, &unitSize, &webID); err != nil {
				return nil, err
			}
			if webID.Valid {
				v := int(webID.Int64)
				p.WebID = &v
			}
			if co2PerKg.Valid {
				p.CO2EqPerKg = &co2PerKg.Float64
			}
			w := weight.EffectiveKg(nullFloat64Ptr(weightKg), unitSize)
			p.WeightPerUnitKg = w
			co2 := computeCO2Total(p.CO2EqPerKg, int(p.Quantity), w)
			if co2 != nil {
				p.CO2EqTotal = *co2
			}
			out = append(out, p)
		}
		return out, rows.Err()
	}

	rRows, err := db.Query(rQ, rArgs...)
	if err != nil {
		return nil, err
	}
	result, err := scan(rRows)
	if err != nil {
		return nil, err
	}
	oRows, err := db.Query(oQ, oArgs...)
	if err != nil {
		return nil, err
	}
	oResult, err := scan(oRows)
	if err != nil {
		return nil, err
	}
	result = append(result, oResult...)

	var totalCO2 float64
	for _, p := range result {
		totalCO2 += p.CO2EqTotal
	}
	for i := range result {
		if totalCO2 > 0 {
			result[i].PctOfCategory = result[i].CO2EqTotal / totalCO2 * 100
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CO2EqTotal > result[j].CO2EqTotal
	})
	return result, nil
}
