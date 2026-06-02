# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.0] - 2026-06-02

### Added

- **Standalone single-file executables** — the app can be packaged as one
  self-contained executable per OS (Windows, macOS, Linux) bundling the Go
  backend and the Streamlit dashboard, runnable with no install or
  dependencies. On first launch it unpacks to a per-user directory, starts both
  services on free local ports, and opens the dashboard. See `packaging/`.
- **Expanded category coverage** — added 95 previously unmapped AH
  subcategories to `ah_subcategory_map.csv`, so more products are classified
  directly instead of falling through to weaker matching.
- **Self-contained backend** — the enrichment CSVs are embedded into the
  backend binary; on-disk copies under `ENRICHMENT_DATA_DIR` still take
  precedence, so any CSV can be overridden without rebuilding.
- **Build tooling** — `packaging/scripts/` builds the executable on each OS
  (with a Docker wrapper for Linux), and a GitHub Actions workflow builds all
  three on native runners and attaches them to the GitHub Release on a `v*` tag.

### Changed

- **Corrections fallback** — with no `corrections.csv` on disk, the backend now
  serves the curated corrections embedded in the binary instead of an empty
  list. A `corrections.csv` on disk (including an empty one) still overrides the
  embedded baseline, so existing setups are unaffected.

### Notes

- The executables are large (~161 MB), as they embed a full Python runtime, and
  are distributed via GitHub Release assets rather than committed to the repo.
  Being unsigned, they trigger a first-launch warning on macOS and Windows.
- The Docker (`./run.sh`) and local (`./run-local.sh`) workflows are unchanged.

## [1.0.0] - 2026-05-30

### Added

#### Backend (Go)

- **OAuth login** — AH OAuth flow matching the official mobile app; token stored in a Docker named volume (`appie-config`) at `config/appie.json`
- **Receipt sync** — fetches up to 199 in-store receipts from the AH API, including per-item POS and web IDs
- **Order sync** — fetches up to 25 online orders (including open/future orders) with full item detail
- **Product metadata** — stores title, brand, AH category, subcategory, unit size, and Nutri-Score per product
- **CO₂ enrichment pipeline** — classifies every unique product exactly once; runs automatically after each sync cycle and can also be triggered independently:
  - Manual corrections via `corrections.csv` (highest priority; supports `ignore` and `set_category` actions)
  - AH subcategory direct lookup via `ah_subcategory_map.csv`
  - Per-piece weight resolution via `default_weights.csv` for count-based products
- **CO₂ calculation** — `co2eq_total = co2eq_per_kg × quantity × weight_per_unit_kg`; per-unit weight resolved by a dedicated `weight` package following a documented "most reliable first" source-priority order (`correction` → `unit_size` → `net_content` → `multipack` → `default` → `serving_size`), with the winning source recorded in `product_enrichment.weight_source`. Products that resolve to no weight are excluded from CO₂ totals and surfaced in a missing-weight list — there is deliberately no 1 kg/unit fallback. See `docs/weight-resolution.md`
- **HTTP API** — REST API on port 8001 covering auth, sync, enrichment, receipts, orders, products, corrections, search, and database management; analytics endpoints (`/products/stats`, `/products/nutriscores`, `/finances/summary`) accept an optional `?since=YYYY-MM-DD` query parameter to scope results to a date range
- **Sustainability analytics endpoints** — CO₂ aggregation computed entirely in the backend: `/sustainability/summary` (grade, % above sustainable target, top category, scaled by `household_ae`), `/sustainability/trend` (emissions grouped by `day`/`week`/`month`/`quarter`/`year` and category), `/sustainability/categories`, and per-category product drilldown
- **Enrichment status tracking** — dedicated status tracker exposes live progress (running/idle, counts, last run) for sync and enrichment
- **SQLite persistence** — all data stored in a local `data/groceries.db` (bind-mounted from the host); schema migrations applied automatically on startup
- **POS↔web ID linking** — receipt items carry both POS terminal ID and webshop ID; reverse lookup available via `/pos/{pos_id}`
- **Settings API** — database reset endpoint for switching AH accounts

#### Dashboard (Python / Streamlit)

- **Home** — overview of purchase counts, spending, Nutri-Score distribution, and top products; period filter pills (All time / Last month / Last 3 months) scope all stats on the page
- **Sustainability** — CO₂eq footprint trend (weekly/monthly/quarterly) with configurable moving-average smoothing, Dutch average (88 kg/person/month) and EAT-Lancet sustainable target (42 kg/person/month) reference lines, interactive category breakdown, and per-category drilldown; all aggregation is performed by the backend, with the dashboard rendering the resolved values; period filter pills scope the trend and score
- **Purchases** — single page combining in-store receipts and online orders in a unified list; clicking a row opens the per-receipt or per-order detail view with product-level CO₂eq breakdown
- **Finances** — spending over time, per-category breakdown, and top discounts across receipts
- **Insights** — top products ranked by times bought, total spent, and total weight; period filter pills scope the ranking
- **Nutri-Score** — Nutri-Score distribution across purchased products with per-score drilldown
- **Products** — product detail page with correction UI: manually override or ignore the CO₂ classification for any product; corrections are persisted via the backend API and applied on the next enrichment run
- **Search** — full-text search across products, receipt items, and order items
- **Data Quality** — surfaces data issues (receipt items not linked to a web product, missing product data, missing weights, and food subcategories not yet mapped to a CO₂ category) alongside an overview of all active manual corrections
- **Settings** — trigger a manual sync, trigger re-enrichment, reset the database, and view enrichment status
- **Login flow** — detects expired or missing tokens and prompts the OAuth login from within the dashboard
- **Light/dark theme** — theme toggle that switches instantly and persists across the dashboard, applied on startup

#### Infrastructure

- **Docker Compose** — single `./run.sh` command to build and start all services
- **Multi-stage Dockerfiles** — minimal Alpine (backend) and Python slim (dashboard) images, both running as non-root users
- **CSV configuration** — `co2eq_categories.csv`, `ah_subcategory_map.csv`, `corrections.csv`, and `default_weights.csv` baked into the backend image and editable without rebuilding

### Known issues

- **Data History Limits**: the AH API limits receipt retrieval to the most recent 199 receipts and order retrieval to the most recent 25 orders (including open/future orders). Data beyond these limits is not accessible through the API. The dashboard surfaces this limitation in the Data Quality page.
- **Token Expiry**: AH OAuth tokens expire periodically. When that happens the dashboard will show the login screen again. Completing the login restores access without affecting existing data.
- **Single Account Only**: the database does not track which AH account data belongs to. If you log out and log in with a different account, the new data will be merged into the existing database rather than kept separate. 
- **Missing Data**: some products have webids that are not available through the API and thus we cannot obtain the required information. This mostly affects products which are purchased in-store by kg, but can be ordered as fixed size. 