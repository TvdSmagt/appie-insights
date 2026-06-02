# Architecture Overview — AH Sustainability Tracker

## Design Principles

The backend is the single source of truth. It owns all AH API interaction, all database access, all data processing (sync, enrichment, CO₂ calculation), and exposes the results through an HTTP API. The dashboard is intentionally kept thin — it fetches ready-to-display values from the backend API and renders them. It contains no business logic: presentation-level aggregation (chart grouping, display sums/percentages, rolling averages, reference benchmark lines) is fine, but core business logic — weight resolution, unit-size parsing, enrichment/matching — stays in the backend (the dashboard consumes its resolved `weight_per_unit_kg`, CO₂ factors, etc.).

This separation means the dashboard can be replaced (e.g. a different frontend framework, a CLI, or a mobile app) without touching backend logic.

## Services

| Service     | Language           | Role                                                                                                                                       |
| ----------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `backend`   | Go                 | OAuth login; syncs receipts/orders via the AH API; stores data in SQLite; enriches products with CO₂eq data; exposes HTTP API on port 8001 |
| `dashboard` | Python (Streamlit) | Fetches data from the backend HTTP API and visualises results                                                                              |

The backend is a single binary. Login, sync, enrichment, and the HTTP API all run within the same process.

The SQLite database (`data/groceries.db`) is written exclusively by the backend. The dashboard never accesses it directly.

### Backend internal packages

| Package     | Role                                                                                                       |
| ----------- | ---------------------------------------------------------------------------------------------------------- |
| `schema`    | SQLite table definitions and the canonical column/struct mapping shared across packages                    |
| `store`     | Migrations and all write-path helpers (receipts, orders, products)                                         |
| `syncer`    | Orchestrates a full sync cycle: fetches receipts and orders from AH via `appie-go`, writes through `store` |
| `worker`    | Enrichment worker: processes unenriched products through the CO₂ pipeline; triggered after each sync cycle |
| `enricher`  | CO₂eq pipeline: category matching, weight resolution, `corrections.csv` handling                           |
| `weight`    | Per-unit weight parsing (`ParseKg`, `ParsePieceCount`) and the source-priority resolution policy           |
| `status`    | Tracks live sync/enrichment progress exposed via `GET /status`                                             |
| `analytics` | Read-only query layer and all analytics HTTP handlers (receipts, orders, products, sustainability, search) |

---

## Data Flow

```
AH API
  ↑↓
appie-go (client library)
  ↑↓
backend/syncer ──writes──→ SQLite (data/groceries.db)
                               ↑
backend/worker ──reads/writes──┘  (enriches via backend/enricher)
                               ↑
                       backend HTTP API :8001
                               ↑
                       dashboard (read + write via HTTP only)
```

1. **Sync** (`backend/syncer`) fetches receipt summaries, individual receipt items (including both POS ID and web ID from the AH API), online orders, and product metadata via `appie-go`. Receipt items carry both `product_id` (POS terminal ID) and `web_id` (AH webshop ID). Both are stored in the `items` table; `products.pos_id` is also updated so the reverse lookup is always available.
2. **Enrich** (`backend/worker` + `backend/enricher`) runs automatically after each sync cycle completes. It processes all products that don't yet have a `product_enrichment` row through the CO₂eq pipeline. Each product is enriched exactly once regardless of how many times it appears across receipts and orders. Enrichment can also be triggered independently via `POST /enrich`.
3. **HTTP API** (`backend/analytics` + root handlers, port 8001) is the only interface the dashboard uses. It exposes pre-computed, join-resolved data so the dashboard does no SQL or business logic of its own.
4. **Dashboard** calls the HTTP API to fetch data and to write corrections. It does not access the database directly.

Both sync and enrich run within the same `backend` binary and process. Sync is triggered on startup and via `POST /sync`; enrichment runs automatically after each sync completes and can also be triggered via `POST /enrich`.

---

## POS and Web ID Handling

Receipt items from the AH API carry both a POS terminal product ID (`ProductID`) and an AH webshop product ID (`WebshopID`). `GetReceipt` internally calls `ConvertPOSIDs` to populate both fields before returning. Sync stores both directly on each `items` row and also records `products.pos_id` so the reverse lookup (web_id → POS ID) is available without a join.

Products that arrive exclusively through online orders (no in-store receipt) will have a `NULL` `pos_id` — this is expected and correct.

---

## CO₂ Enrichment Pipeline

The worker polls continuously and processes all `web_id`s referenced by either `items` or `order_items` that don't yet have a `product_enrichment` row, then processes each one:

### Step 1: Apply corrections (early exit)
`corrections.csv` is checked first. Corrections are keyed by `web_id` and override all automatic matching. Actions:
- `ignore` — marks the product as non-food/irrelevant; excluded from CO₂ totals.
- `set_category` — forces a specific CO₂eq entry, `co2eq_per_kg`, and optional `weight_kg`.

### Step 2: Match CO₂eq category
Using the product's AH subcategory from the `products` table:

1. **Vegan override** (`subcategory_vegan`) — if the product has a vegan property icon, a `"<subcategory> (vegan)"` key is checked in `ah_subcategory_map.csv` first. If found with a CO₂eq value, it wins.
2. **Subcategory direct** (`subcategory_direct`) — the AH subcategory is looked up directly in `ah_subcategory_map.csv`. If matched with a CO₂eq value, the result is used. If the subcategory is present in the map but has no CO₂eq value, the product is marked `non_food`.
3. **Non-food category fallback** (`non_food`) — if the subcategory is not in the map at all, the AH top-level category is checked against a hardcoded list of known non-food categories (e.g. `drogisterij`, `huishouden`). Matches are marked `non_food`.
4. Products whose subcategory is not in the map and whose top-level category is not a known non-food category are marked `unmatched`. Products with no subcategory and no title are marked `no_metadata`.

### Step 3: Resolve per-unit weight
`resolveWeights` (in `backend/enricher`, using the `backend/weight` package) derives a per-unit weight in kilograms by trying each source in `weight.SourcePriority` order — `correction` → `unit_size` → `net_content` → `multipack` → `default` → `serving_size` — and keeping the first that yields a weight. `default_weights.csv` supplies the `default` source (per-piece estimates for count-based products, matched by title keyword). Products that resolve to no weight are excluded from CO₂ totals; there is no 1 kg/unit fallback. See [weight-resolution.md](weight-resolution.md) for the full policy.

---

## Backend HTTP API

The backend exposes an HTTP server on port 8001. The dashboard communicates with it exclusively via this API — it never imports backend code directly.

See [api.md](api.md) for the full endpoint reference. High-level groupings:

| Group             | Endpoints                                                                                                                                                                                                             |
| ----------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Auth & login      | `GET /auth/status`, `POST /login/start`                                                                                                                                                                               |
| Sync              | `POST /sync`, `GET /sync/status`                                                                                                                                                                                      |
| Enrichment worker | `POST /enrich`, `GET /status`                                                                                                                                                                                         |
| CO₂ categories    | `GET /categories`                                                                                                                                                                                                     |
| Corrections       | `GET /corrections`, `POST /corrections`, `GET /corrections/missing-category`, `GET /corrections/missing-weight`                                                                                                       |
| Receipts          | `GET /receipts`, `GET /receipts/{id}`                                                                                                                                                                                 |
| Orders            | `GET /orders`, `GET /orders/{id}`                                                                                                                                                                                     |
| Items             | `GET /items`                                                                                                                                                                                                          |
| Products          | `GET /products`, `GET /products/{web_id}`, `GET /products/{web_id}/purchases`, `GET /products/stats`, `GET /products/issues`, `GET /products/nutriscores`, `POST /products/{web_id}/fetch`, `POST /products/link-pos` |
| Sustainability    | `GET /sustainability/summary`, `GET /sustainability/trend`, `GET /sustainability/categories`, `GET /sustainability/categories/{category}/products`                                                                   |
| Finances          | `GET /finances/summary`                                                                                                                                                                                               |
| POS lookup        | `GET /pos/{pos_id}`                                                                                                                                                                                                   |
| Search            | `GET /search`                                                                                                                                                                                                         |
| Enrichment mgmt   | `GET /enrichment/count`, `GET /enrichment/pending`, `DELETE /enrichment`, `DELETE /enrichment/{web_id}`                                                                                                               |
| Database          | `POST /database/reset`                                                                                                                                                                                                |

---

## CSV Configuration Files

| File                     | Purpose                                                    |
| ------------------------ | ---------------------------------------------------------- |
| `co2eq_categories.csv`   | CO₂eq values per food item (kg CO₂/kg)                     |
| `ah_subcategory_map.csv` | AH subcategory → CO₂eq value (primary matching path)       |
| `corrections.csv`        | Manual overrides keyed by `web_id`: ignore or set_category |
| `default_weights.csv`    | Per-piece weight estimates for count-based products        |

These live in `backend/data/` and are read from `ENRICHMENT_DATA_DIR` at run time. They are also **embedded into the backend binary** (`backend/embed.go`): when a file is absent under `ENRICHMENT_DATA_DIR`, the loader falls back to the embedded copy (see `enricher.openData`). On-disk files always take precedence, so users can still override individual CSVs without rebuilding. The embedded copies are what make the standalone single-file executable self-contained (see [`packaging/`](../packaging/README.md)) — no data files travel alongside it.

---

## Database Schema

### Core tables (written by sync)

```
receipts      (transaction_id PK, date, total_amount, synced_at)
items         (id PK, receipt_id FK, description, quantity, amount, unit_price, product_id, web_id)
orders        (order_id PK, delivery_date, delivery_method, total_price, ...)
order_items   (id PK, order_id FK, web_id, title, category, quantity, unit_price, ...)
products      (web_id PK, pos_id, title, brand, ah_category, unit_size, ...)
```

`items.web_id` is populated by sync from the AH API for every receipt item that has a known webshop product. Items purchased in-store that the AH API cannot resolve will have a `NULL` web_id and are excluded from CO₂ calculations.

`products.pos_id` records the in-store POS terminal ID for a product. It is set when a receipt item carrying both `product_id` and `web_id` is synced. Products known only from online orders will have a `NULL` `pos_id`.

### Enrichment table (written by enrich worker)

```
product_enrichment  (web_id PK, co2eq_category, co2eq_name, co2eq_per_kg,
                     match_method, weight_kg, weight_source)
```

One row per unique AH web product. Receipt items and order items both reference this
table directly through `items.web_id` / `order_items.web_id`.

---

## CO₂ Calculation

```
co2eq_total = co2eq_per_kg × quantity × weight_per_unit_kg
```

`weight_per_unit_kg` is resolved during enrichment from several signals in a fixed
priority order (`correction` → `unit_size` → `net_content` → `multipack` → `default`
→ `serving_size`), recorded in `product_enrichment.weight_source`. Products that
resolve to no weight are excluded from CO₂ totals and surface in the missing-weight
list — there is deliberately no 1 kg/unit fallback. See
[weight-resolution.md](weight-resolution.md) for the full policy and rationale.

Reference benchmarks (trend chart):
- Dutch average: 88 kg CO₂eq / person / month
- Sustainable target (EAT-Lancet): 42 kg CO₂eq / person / month
