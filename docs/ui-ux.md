# UI/UX Conventions

## Page headers

- Every page uses `st.header` with a leading emoji: `st.header("🛍️ Purchases")`
- `st.title` is reserved for the login page only
- Subheaders use Title Case; emoji is optional but should be consistent within a page

## Dividers

- Use `st.divider()` to separate major sections on every page
- Exception: `dashboard.py` uses a custom `_section_sep()` helper because the home layout is denser than other pages and the default divider spacing is too large — this is intentional

## Empty / no-data states

| Situation | Widget |
|---|---|
| Data not synced yet | `st.info("No X synced yet.")` |
| Backend call failed | `st.error("Could not load X: {e}")` |
| User-actionable gap (missing link, incomplete data) | `st.warning(...)` |
| Action confirmed (save, link, etc.) | `st.success(...)` |

Wording: sentence case, end with a period. Backend errors include the exception message.

## Period selectors

All period selectors use `st.pills` (not `st.segmented_control`). Two distinct variants exist — use the shared widgets from `widgets.py` for both, with `label_visibility="collapsed"`.

### History-range selector

Used when the user picks how far back to show data (Purchases, Dashboard, Insights).

- Widget: `period_filter_pills(key, default="Last 3 months")` → returns `int | None` (months back; `None` = all time)
- Options order: **old → recent** (widest range first)
  - `All time` → `Last year` → `Last 3 months` → `Last month`

### Granularity selector

Used when the user picks the bucket size for aggregated charts (Finances, Sustainability).

- Widget: `granularity_pills(key, default="Month", options=None)` → returns the label string
- Use `GRANULARITY_FREQ[label]` from `widgets.py` to convert to a pandas/plotly frequency code
- Options order: **coarse → fine** (largest unit first)
  - Default (all five): `Year` → `Quarter` → `Month` → `Week` → `Day`
  - Finances omits `Quarter` (backend does not support it): `Year` → `Month` → `Week` → `Day`

## Charts

- All charts use Plotly (`go` or `px`) — never `st.bar_chart` / `st.line_chart`
- Always pass `template=get_plotly_template()` from `theme.py` for light/dark support
- All `st.plotly_chart` calls use `width="stretch"`
- Standard layout: `height=320`, `margin={"t": 40, "b": 40}`, legend top-right horizontal

### Chart colors

Use named constants from `chart_config.CHART_COLORS` — never hardcode hex values in page files:

| Key | Color | Usage |
|---|---|---|
| `"spent"` | `#0072CE` | Primary spend metric (AH blue) |
| `"discount"` | `#00A878` | Savings / discount series |
| `"moving_avg"` | `#FF6B35` | Trend / moving average line |
| `"co2eq"` | `#D97706` | CO₂eq series |
| `"cost"` | `#00AE4D` | Cost bars in cost/CO₂ comparison chart |
| `"unknown"` | `#888888` | Missing / unknown values (e.g. no nutriscore) |

Category colors, nutriscore colors, and source colors have their own dicts in `chart_config.py`.

## Dataframes

- Always use `st.dataframe(..., width="stretch", hide_index=True)`
- Column names: Title Case, units in parentheses where applicable
  - `"Quantity"` not `"Qty"`
  - `"Amount (€)"` not `"€"`
  - `"CO₂eq (kg)"` not `"CO₂/kg"`
- Use `st.column_config` for number formatting rather than pre-formatting values

## Navigation

- Detail pages are entered by setting a session state key and calling `st.rerun()` or `st.switch_page()`
- Back buttons always appear top-left, labeled `"← Back"` or `"← Back to {Location}"`
- Session state keys for navigation are prefixed with `_` (e.g. `_product_nav_source`)
