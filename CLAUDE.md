# Agents

## Running tests

```bash
make test-fast          # fast unit tests only (no model download)
make test-unit          # all unit tests, including slower tests that use the LLM
make test-integration   # integration tests (requires AH credentials)
make test-all           # everything: unit + integration
```

Integration tests require real AH credentials. They are loaded automatically (in priority order) from `AH_ACCESS_TOKEN` / `AH_REFRESH_TOKEN` env vars, then from `~/.config/appie/appie.json` (XDG path used by a local backend run), then from `config/appie.json` (Docker volume mount path). If none of these exist (e.g. running outside Docker without the `appie-config` named volume), run `make test-login` once to perform the AH OAuth flow and write a token to `~/.config/appie/appie.json`. The tests fetch up to 3 receipts and 3 orders from the live API into a temporary DB; override with `SYNC_MAX_RECEIPTS=N SYNC_MAX_ORDERS=N`.

To keep the DB after a run for manual inspection:
```bash
make test-integration KEEP_DB=/tmp/ah_test.db
```

**Tests that pass on the base branch must keep passing.** Assume the suite was green before your change: if a test fails after it, the failure is caused by your change (or is a real regression it exposed) — fix the code, or update the test only when the behaviour change is intended and correct. Never skip, disable, comment out, or loosen a test just to get a green run.

# Architecture

The backend is responsible for all business logic and the core CO₂/weight/financial calculations. API responses should contain ready-to-display, join-resolved values. The Streamlit dashboard is a thin presentation layer: it formats and renders what the backend returns and may perform presentation-level aggregation (chart grouping, sums/percentages for display, rolling averages, reference benchmark lines). It must not reimplement core business logic — it must not resolve product weights, parse unit sizes, or apply enrichment/matching rules; those belong in the backend (the dashboard consumes the backend's resolved `weight_per_unit_kg`, CO₂ factors, etc.).

# Streamlit

- Use `width="stretch"` instead of `use_container_width=True`, and `width="content"` instead of `use_container_width=False`. The `use_container_width` parameter is deprecated and is supposed to be removed after 2025-12-31.


# Documentation

Use these docs for reference when working on the codebase:

- `docs/architecture-overview.md` — high-level architecture and data flow
- `docs/api.md` — backend HTTP API spec
- `docs/weight-resolution.md` — how per-unit product weights are resolved (source priority order)

Update these docs when changes are made to the architecture or API. 