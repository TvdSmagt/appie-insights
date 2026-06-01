# Backend API

The backend exposes a single HTTP service (default port `8001`, configured via `ENRICH_HTTP_PORT`) consumed exclusively by the dashboard. All responses are `application/json`.

Base URL (in Docker Compose): `http://enrich:8001`

---

## Auth & Login

### `GET /auth/status`
Returns the current authentication state.

**Response**
```json
{
  "logged_in": true,
  "in_progress": false,
  "error": ""
}
```

### `POST /login/start`
Triggers the AH OAuth flow on the backend (opens a browser window on the host). No-ops if already logged in or a login is already in progress.

**Response** `{"ok": true}`

### `POST /logout`
Deletes the stored credentials file, logging the user out. Succeeds even if no credentials are stored.

**Response** `{"ok": true}`

---

## Enrichment Worker

### `POST /enrich`
Enqueues an enrichment run. If `receipt_id` is provided, only items from that receipt are re-enriched.

**Request body** *(both fields optional)*
```json
{ "receipt_id": "TRX-123" }
```

**Response** `{"ok": true}`

### `GET /status`
Returns the current enrichment worker state. Poll this after `POST /enrich`.

**Response**
```json
{
  "status": "idle | running",
  "items_total": 42,
  "items_processed": 17,
  "updated_at": "2026-05-24T12:00:00Z"
}
```

---

### `GET /version`
Returns the running backend version.

**Response**
```json
{
  "version": "1.0.0"
}
```

---

## Sync

### `POST /sync`
Enqueues one sync cycle (fetches new receipts and orders from AH).

**Response** `{"ok": true}`

### `GET /sync/status`
Returns the state of the last sync run.

**Response**
```json
{
  "status": "idle | running",
  "receipts_found": 10,
  "receipts_synced": 3,
  "updated_at": "2026-05-24T12:00:00Z"
}
```

---

## CO2 Categories & Corrections

### `GET /categories`
Returns all CO2eq categories loaded from the data directory.

**Response** `[{ "name": "...", "co2eq_per_kg": 1.5, ... }, ...]`

### `GET /corrections`
Returns all manual corrections.

**Response** `[{ "web_id": 123, "action": "set_category | ignore", "co2eq_per_kg": 1.5, "weight_kg": 0.4, "co2eq_category": "...", "co2eq_name": "...", "notes": "..." }, ...]`

### `POST /corrections`
Replaces the entire corrections list.

**Request body** Array of correction objects (same schema as above).

**Response** `{"ok": true}`

---

## Receipts

### `GET /receipts`
List all receipts, newest first.

**Response**
```json
[{
  "transaction_id": "TRX-123",
  "date": "2026-05-20T10:00:00Z",
  "total_amount": 34.50,
  "item_count": 8,
  "matched_count": 6,
  "co2eq_total": 2.1
}]
```
`co2eq_total` is `null` when no enrichment data is available.

### `GET /receipts/{id}`
Receipt detail with all line items.

**Response**
```json
{
  "transaction_id": "TRX-123",
  "date": "...",
  "total_amount": 34.50,
  "item_count": 8,
  "matched_count": 6,
  "co2eq_total": 2.1,
  "items": [{
    "id": 1,
    "description": "Melk",
    "web_title": "Melk Halfvol",
    "quantity": 2,
    "amount": 3.00,
    "web_id": 456,
    "pos_id": 789,
    "ah_category": "Zuivel",
    "co2eq_category": "dairy",
    "co2eq_name": "Melk",
    "co2eq_per_kg": 3.2,
    "match_method": "subcategory_direct",
    "weight_kg": 1.0,
    "unit_size": "1L",
    "thumbnail_url": "...",
    "weight_per_unit_kg": 1.0,
    "co2eq_total": 6.4
  }]
}
```
**404** when the receipt does not exist.

---

## Items

### `GET /items`
All purchased items across receipts and orders, newest first. Each item carries `source_type: "receipt" | "order"`.

**Response**
```json
[{
  "source_type": "receipt",
  "description": "Melk",
  "quantity": 2,
  "amount": 3.00,
  "date": "2026-05-20",
  "co2eq_category": "dairy",
  "co2eq_name": "Melk",
  "co2eq_per_kg": 3.2,
  "match_method": "subcategory_direct",
  "weight_kg": 1.0,
  "unit_size": "1L",
  "web_id": 456,
  "web_title": "Melk Halfvol",
  "weight_per_unit_kg": 1.0,
  "co2eq_total": 6.4
}]
```

---

## Orders

### `GET /orders`
List all delivery orders, newest first.

**Response**
```json
[{
  "order_id": 1001,
  "delivery_date": "2026-05-21",
  "delivery_method": "home",
  "delivery_status": "delivered",
  "total_price": 78.40,
  "item_count": 12,
  "matched_count": 10,
  "co2eq_total": 5.3
}]
```

### `GET /orders/{id}`
Order detail with all line items.

**Response**
```json
{
  "order_id": 1001,
  "delivery_date": "2026-05-21",
  "total_price": 78.40,
  "delivery_method": "home",
  "invoice_id": "INV-999",
  "address_street": "Hoofdstraat",
  "address_number": "1",
  "address_extra": "",
  "address_postcode": "1234AB",
  "address_city": "Amsterdam",
  "items": [{
    "web_id": 456,
    "title": "Melk Halfvol",
    "brand": "AH",
    "category": "Zuivel",
    "sales_unit_size": "1L",
    "quantity": 3,
    "allocated_qty": 3,
    "unit_price": 1.09,
    "was_price": null,
    "image_url": "...",
    "co2eq_category": "dairy",
    "co2eq_name": "Melk",
    "co2eq_per_kg": 3.2,
    "weight_per_unit_kg": 1.0,
    "co2eq_total": 6.4
  }]
}
```
**404** when the order does not exist.

---

## Products

### `GET /products`
All known products (only those with a title).

**Response**
```json
[{
  "web_id": 456,
  "thumbnail_url": "...",
  "title": "Melk Halfvol",
  "brand": "AH",
  "ah_category": "Zuivel",
  "ah_subcategory": "Melk",
  "unit_size": "1L",
  "nutriscore": "B",
  "unit_price_description": "€1.09/L",
  "property_icons": "...",
  "co2eq_per_kg": 3.2,
  "co2eq_category": "dairy",
  "weight_per_unit_kg": 1.0
}]
```

### `GET /products/issues`
Returns a data-quality summary for all food products (non-food items excluded throughout).

**Response**
```json
{
  "summary": {
    "total_food_products": 150,
    "no_web_id": 5,
    "no_pos_id": 30,
    "no_product_data": 3,
    "no_weight": 12,
    "unmatched_subcategories": 4,
    "unmatched_no_subcategory": 2
  },
  "no_web_id": [{
    "pos_id": 789,
    "description": "Biologische Melk"
  }],
  "no_pos_id": [{
    "web_id": 456,
    "pos_id": null,
    "title": "Melk Halfvol",
    "ah_category": "Zuivel",
    "ah_subcategory": "Melk",
    "unit_size": "1L",
    "weight_kg": null,
    "co2eq_name": "Melk",
    "co2eq_category": "dairy",
    "co2eq_per_kg": 3.2
  }],
  "no_product_data": [{ "web_id": 999, "pos_id": 101, "title": "", ... }],
  "no_weight": [{ "web_id": 456, "title": "Koekjes", "unit_size": "per stuk", "co2eq_category": "biscuits", ... }],
  "unmatched_subcategories": [{
    "ah_category": "Wereldkeuken",
    "ah_subcategory": "Midden-Oosterse pasta",
    "product_count": 2,
    "example_titles": ["Tahin", "Sesampasta"]
  }],
  "unmatched_no_subcategory": [{ "web_id": 555, "title": "Onbekend product", "ah_category": "Diversen", "ah_subcategory": "", ... }]
}
```

Issue categories:
- `no_web_id` — receipt items (`items` table) that carry a `product_id` (POS barcode) but have no `web_id` linked yet.
- `no_pos_id` — food products in the `products` table that have a `web_id` but no `pos_id`.
- `no_product_data` — products with a `web_id` where AH site data (title, subcategory) has not been fetched yet.
- `no_weight` — enriched food products whose weight cannot be resolved from any source (unit size, net content, serving size, …) and have no `weight_kg` override; these are excluded from CO₂ totals until a weight is added (there is no 1 kg/unit fallback).
- `unmatched_subcategories` — distinct AH subcategories, under a food category, that are not in `ah_subcategory_map.csv`, so their products land in `match_method = unmatched` with no CO₂ factor. Grouped by subcategory (the unit of decision: mapping one resolves every product under it), ordered by `product_count` descending. Each entry carries the parent `ah_category`, the affected `product_count`, and up to 3 `example_titles`.
- `unmatched_no_subcategory` — products in `match_method = unmatched` that carry *no* AH subcategory string, so they cannot be resolved via the subcategory map. They need their AH product data (re)fetched or a per-product correction instead.

The per-product arrays (`no_pos_id`, `no_product_data`, `no_weight`, `unmatched_no_subcategory`) use the same per-item schema as listed above; `pos_id`, `weight_kg`, `co2eq_per_kg` are `null` when absent. `unmatched_subcategories` uses the distinct grouped schema shown above.

### `GET /products/stats?since={date}`
Purchase statistics aggregated per product (across receipts and orders).

`times_bought` counts the number of purchase lines (one per receipt or order row), not total quantity.

**Query params** *(optional)*
- `since` — ISO date (`YYYY-MM-DD`): only include purchases on or after this date. Applies to receipt date and order delivery date.

**Response**
```json
[{
  "web_id": 456,
  "title": "Melk Halfvol",
  "thumbnail_url": "...",
  "times_bought": 8,
  "total_spent": 8.72,
  "total_kg": 8.0,
  "co2eq_total": 25.6
}]
```

### `GET /products/nutriscores?since={date}`
Nutriscore distribution across all purchased products (receipts + orders).

`count` is the number of distinct products purchased with that score; `times_bought` is the total number of units bought.

**Query params** *(optional)*
- `since` — ISO date (`YYYY-MM-DD`): only count purchases on or after this date.

**Response**
```json
[
  { "score": "A", "count": 5, "times_bought": 20 },
  { "score": "B", "count": 12, "times_bought": 45 },
  { "score": "",  "count": 3,  "times_bought": 10 }
]
```
`score` is `""` for products with no nutriscore data. Results are ordered by score.

### `GET /products/nutriscores?score={score}`
All purchased products with the given nutriscore, with aggregated purchase stats. Pass `score=` (empty value) for products without a score.

**Response**
```json
[{
  "web_id": 456,
  "thumbnail_url": "...",
  "title": "Melk Halfvol",
  "nutriscore": "B",
  "times_bought": 8,
  "total_spent": 8.72,
  "total_kg": 8.0,
  "co2eq_total": 25.6
}]
```

### `GET /products/{web_id}`
Full product detail including enrichment metadata.

**Response**
```json
{
  "web_id": 456,
  "thumbnail_url": "...",
  "title": "Melk Halfvol",
  "brand": "AH",
  "ah_category": "Zuivel",
  "ah_subcategory": "Melk",
  "unit_size": "1L",
  "nutriscore": "B",
  "unit_price_description": "€1.09/L",
  "property_icons": "...",
  "co2eq_per_kg": 3.2,
  "co2eq_category": "dairy",
  "pos_id": 789,
  "co2eq_name": "Melk",
  "match_method": "subcategory_direct",
  "net_content": "1000 ml",
  "serving_size": "250 ml",
  "weight_kg": 1.0,
  "weight_per_unit_kg": 1.0,
  "weight_source": "unit_size",
  "weight_breakdown": [
    { "source": "correction",   "value_kg": null, "active": false },
    { "source": "unit_size",    "value_kg": 1.0,  "active": true },
    { "source": "net_content",  "value_kg": 1.0,  "active": false },
    { "source": "multipack",    "value_kg": null, "active": false },
    { "source": "default",      "value_kg": null, "active": false },
    { "source": "serving_size", "value_kg": null, "active": false }
  ]
}
```
- `weight_source` names the source that set `weight_kg` (`unit_size`, `net_content`, `serving_size`, `multipack`, `default`, `correction`), or `""` if no weight resolved.
- `weight_breakdown` lists every source's candidate weight in priority order; `value_kg` is `null` when that source yields nothing, and exactly one entry is `active` (the one used). The measured sources (unit size, net content, serving size) are computed from product fields; `multipack`/`default`/`correction` only show a value when they are the active source.

**404** when the product does not exist.

### `GET /products/{web_id}/purchases`
All purchases (receipts + orders) for one product, newest first.

**Response**
```json
[{
  "date": "2026-05-20",
  "description": "Melk",
  "quantity": 2,
  "amount": 2.18,
  "unit_price": 1.09,
  "source": "receipt | order"
}]
```

### `POST /products/{web_id}/fetch`
Force-fetches product metadata from the AH API and stores it.

**Response**
```json
{ "found": true, "title": "Melk Halfvol" }
```
**503** when no AH credentials are available.

### `POST /products/link-pos`
Links a POS `product_id` to an AH `web_id` and updates all unlinked items.

**Request body**
```json
{ "pos_id": 789, "web_id": 456 }
```
**Response**
```json
{ "updated_items": 3, "product_title": "Melk Halfvol" }
```

---

## POS Lookup

### `GET /pos/{pos_id}`
Looks up a POS product ID and returns any known web product link.

**Response**
```json
{
  "pos_id": 789,
  "description": "Melk",
  "web_id": 456,
  "title": "Melk Halfvol",
  "thumbnail_url": "...",
  "in_not_found": false
}
```
**404** when the POS ID is not found in any receipt.

---

## Search

### `GET /search?q={query}`
Full-text search across products, receipt items, and order items. Minimum 2 characters.

**Response**
```json
{
  "products": [{
    "web_id": 456,
    "thumbnail_url": "...",
    "title": "Melk Halfvol",
    "brand": "AH",
    "ah_category": "Zuivel",
    "unit_size": "1L"
  }],
  "receipt_items": [{
    "date": "2026-05-20",
    "transaction_id": "TRX-123",
    "description": "Melk",
    "quantity": 2,
    "amount": 2.18
  }],
  "order_items": [{
    "delivery_date": "2026-05-21",
    "order_id": 1001,
    "title": "Melk Halfvol",
    "brand": "AH",
    "quantity": 1,
    "amount": 1.09
  }]
}
```
Each section is capped at 50 results.

---

## Correction Helpers

### `GET /corrections/missing-category`
Products that have enrichment data but no CO2 category match.

**Response**
```json
[{
  "web_id": 456,
  "title": "Exotisch product",
  "ah_category": "...",
  "unit_size": "...",
  "co2eq_name": "",
  "co2eq_per_kg": null,
  "weight_kg": null,
  "match_method": "unmatched"
}]
```

### `GET /corrections/missing-weight`
Products that have a CO2 category but no resolvable weight (so CO2 total cannot be computed).

**Response**
```json
[{
  "web_id": 456,
  "title": "...",
  "unit_size": "per stuk",
  "co2eq_name": "...",
  "co2eq_category": "...",
  "co2eq_per_kg": 1.5,
  "weight_kg": null
}]
```

---

## Enrichment Management

### `GET /enrichment/count`
Total number of rows in the `product_enrichment` table.

**Response** `{"count": 142}`

### `GET /enrichment/pending`
Number of products that have been seen in receipts/orders but not yet enriched.

**Response** `{"count": 5}`

### `DELETE /enrichment`
Clears all enrichment data (triggers re-enrichment on next worker cycle).

**Response** `204 No Content`

### `DELETE /enrichment/{web_id}`
Clears enrichment for one product.

**Response** `{"deleted": 1}`

---

## Finances

### `GET /finances/summary?since={date}`
Returns aggregated financial metrics across all receipts and orders.

**Query params** *(optional)*
- `since` — ISO date (`YYYY-MM-DD`): only include receipts and orders on or after this date. All metrics (totals, discounts, averages) are scoped to the filtered date range.

**Response**
```json
{
  "total_spent": 1234.56,
  "avg_per_year": 617.28,
  "avg_per_month": 51.44,
  "avg_per_week": 11.87,
  "total_discount": 45.20,
  "discount_avg_per_year": 22.60,
  "discount_avg_per_month": 1.88,
  "discount_avg_per_week": 0.43,
  "first_date": "2024-01-03T10:15:00",
  "last_date": "2025-05-20T09:30:00"
}
```

### `GET /finances/by-category`
Returns total spending grouped by AH product category and subcategory, sorted by total descending. Falls back to order item category when product data is unavailable.

**Response**
```json
[
  { "category": "Zuivel", "subcategory": "Melk", "total_spent": 98.40 },
  { "category": "Vlees", "subcategory": "Onbekend", "total_spent": 72.10 }
]
```

### `GET /finances/over-time?period={period}`
Returns spending per period for charting. `period` is one of `day`, `week`, `month` (default), `quarter`, `year`.

**Response**
```json
[
  { "period": "2024-01", "amount": 102.35, "discount": 5.20 },
  { "period": "2024-02", "amount": 88.70, "discount": 3.10 }
]
```

---

## Sustainability

### `GET /sustainability/summary?since={date}&household_ae={n}`
Returns CO₂ sustainability metrics and grade, optionally filtered by date and scaled by household size.

**Query params** *(optional)*
- `since` — ISO date (`YYYY-MM-DD`): only include data on or after this date
- `household_ae` — positive float, adult-equivalent household size (default `1.0`)

**Response**
```json
{
  "grade": "B",
  "pct_above_sustainable": 12.4,
  "top_category": "Vlees",
  "avg_kg_per_ae_per_month": 47.0
}
```

`grade` is `A`–`E` based on EAT-Lancet thresholds (sustainable=42 kg CO₂eq/person/month, Dutch avg=88). `null` when no data. `pct_above_sustainable` is `null` when no data.

### `GET /sustainability/trend?period={period}&since={date}`
Returns CO₂ emissions grouped by period and food category for trend charting.

**Query params**
- `period` — `day`, `week`, `month` (default), `quarter`, `year`
- `since` *(optional)* — ISO date (`YYYY-MM-DD`)

**Response**
```json
[
  { "period": "2024-01", "category": "Vlees", "co2eq": 12.3 },
  { "period": "2024-01", "category": "Zuivel", "co2eq": 4.1 }
]
```

### `GET /sustainability/categories`
Returns all-time CO₂ totals per food category, sorted descending.

**Response**
```json
[
  { "category": "Vlees", "co2eq": 145.2 },
  { "category": "Zuivel", "co2eq": 48.7 }
]
```

### `GET /sustainability/categories/{category}/products?period_type={period}&period_label={label}`
Returns individual products within a food category, sorted by CO₂ descending. Optional period filter scopes to a specific period (e.g. a single month).

**Query params** *(optional, both required together)*
- `period_type` — `day`, `week`, `month`, `quarter`, `year`
- `period_label` — period label matching the format produced by `period_type` (e.g. `2024-01` for month)

**Response**
```json
[
  {
    "description": "Kipfilet",
    "web_title": "AH Kipfilet",
    "co2eq_name": "Kip",
    "quantity": 3.0,
    "amount": 14.97,
    "co2eq_total": 9.6,
    "co2eq_per_kg": 4.57,
    "weight_per_unit_kg": 0.7,
    "web_id": 123456,
    "percentage_of_category": 34.2
  }
]
```

---

## Database

### `POST /database/reset`
Deletes all data from `product_enrichment`, `items`, `receipts`, `order_items`, `orders`, and `products`.

**Response** `204 No Content`
