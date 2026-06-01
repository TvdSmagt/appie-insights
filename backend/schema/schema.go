// Package schema holds the canonical database DDL so that production
// initialization and tests share a single source of truth. A column referenced
// in code but missing here will fail tests that build their DB from DDL, rather
// than only surfacing against the live database during a sync.
package schema

// DDL is the full database schema. It must stay in sync with the migrations in
// migrate.go: any column added by a migration must also be present here so that
// freshly-created databases match migrated ones.
const DDL = `
	CREATE TABLE IF NOT EXISTS receipts (
		transaction_id TEXT PRIMARY KEY,
		date           TEXT NOT NULL,
		total_amount   REAL NOT NULL,
		synced_at      TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE TABLE IF NOT EXISTS items (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		receipt_id      TEXT NOT NULL REFERENCES receipts(transaction_id),
		description     TEXT NOT NULL,
		quantity        INTEGER NOT NULL,
		amount          REAL NOT NULL,
		unit_price      REAL,
		product_id      INTEGER,
		web_id          INTEGER,
		UNIQUE(receipt_id, description, quantity, amount)
	);
	CREATE TABLE IF NOT EXISTS receipt_discounts (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		receipt_id TEXT NOT NULL REFERENCES receipts(transaction_id),
		name       TEXT NOT NULL,
		amount     REAL NOT NULL,
		UNIQUE(receipt_id, name, amount)
	);
	CREATE TABLE IF NOT EXISTS receipt_payments (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		receipt_id TEXT NOT NULL REFERENCES receipts(transaction_id),
		method     TEXT NOT NULL,
		amount     REAL NOT NULL,
		UNIQUE(receipt_id, method, amount)
	);
	CREATE TABLE IF NOT EXISTS products (
		web_id                 INTEGER PRIMARY KEY,
		pos_id                 INTEGER,
		title                  TEXT,
		brand                  TEXT,
		short_description      TEXT, -- reserved: not yet populated (planned richer product info)
		ah_category            TEXT,
		ah_subcategory         TEXT,
		nutriscore             TEXT,
		nutriscore_checked_at  TEXT,
		unit_size              TEXT,
		unit_price_description TEXT,
		property_icons         TEXT,
		nutritional_info       TEXT, -- reserved: not yet populated (planned richer product info)
		thumbnail_url          TEXT,
		serving_size           TEXT,
		net_content            TEXT,
		fetched_at             TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE TABLE IF NOT EXISTS product_not_found (
		web_id      INTEGER PRIMARY KEY,
		recorded_at TEXT    NOT NULL DEFAULT (datetime('now'))
	);
	CREATE TABLE IF NOT EXISTS product_enrichment (
		web_id         INTEGER PRIMARY KEY,
		co2eq_category TEXT,
		co2eq_name     TEXT,
		co2eq_per_kg   REAL,
		match_method   TEXT,
		weight_kg      REAL,
		weight_source  TEXT  -- which source set weight_kg: unit_size, net_content, serving_size, multipack, default, correction
	);
	CREATE TABLE IF NOT EXISTS orders (
		order_id         INTEGER PRIMARY KEY,
		delivery_date    TEXT NOT NULL,
		delivery_method  TEXT,
		delivery_status  TEXT,
		closing_datetime TEXT,
		invoice_id       TEXT,
		total_price      REAL,
		address_street   TEXT,
		address_number   INTEGER,
		address_extra    TEXT,
		address_postcode TEXT,
		address_city     TEXT,
		synced_at        TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE TABLE IF NOT EXISTS order_items (
		id               INTEGER PRIMARY KEY AUTOINCREMENT,
		order_id         INTEGER NOT NULL REFERENCES orders(order_id),
		web_id           INTEGER NOT NULL,
		title            TEXT NOT NULL,
		brand            TEXT,
		category         TEXT,
		sales_unit_size  TEXT,
		quantity         INTEGER NOT NULL,
		allocated_qty    INTEGER NOT NULL,
		unit_price       REAL,
		was_price        REAL,
		image_url        TEXT,
		UNIQUE(order_id, web_id)
	);`

// match_method values written to product_enrichment.match_method during
// enrichment. This is the canonical vocabulary: the enricher (backend/enricher)
// writes these and the analytics layer (backend/analytics) filters on them. Keep
// it here, beside the DDL, so producer and consumer share one definition.
const (
	MatchMethodCorrection        = "correction"         // manual corrections.csv entry
	MatchMethodSubcategoryVegan  = "subcategory_vegan"  // matched via a "(vegan)" subcategory key
	MatchMethodSubcategoryDirect = "subcategory_direct" // matched via the AH subcategory map
	MatchMethodNonFood           = "non_food"           // subcategory/category is non-food (no CO₂ factor)
	MatchMethodUnmatched         = "unmatched"          // subcategory not in the map; no CO₂ factor
	MatchMethodNoProduct         = "no_product"         // no products row for the web_id
	MatchMethodNoMetadata        = "no_metadata"        // product has no title and no subcategory
	MatchMethodIgnored           = "ignored"            // corrections.csv "ignore" action
)
