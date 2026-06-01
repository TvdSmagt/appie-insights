# Weight Resolution

Every CO₂ figure is `co2eq_per_kg × quantity × weight_per_unit_kg`, so each product
needs a **per-unit weight in kilograms**. AH does not expose a single reliable
weight field, so the backend derives one from several signals. This document is
the source of truth for **which signals are used and in what order**.

## Priority order

`weight_kg` is resolved during enrichment by `resolveWeights`
([backend/enricher/enricher.go](../backend/enricher/enricher.go)). It tries each
source in `weight.SourcePriority` order and keeps the **first** one that yields a
weight; `product_enrichment.weight_source` records which source won.

| # | `weight_source` | Where it comes from | Notes |
| - | --------------- | ------------------- | ----- |
| 1 | `correction`    | A manual entry in `corrections.csv` (`weight_kg`). | Authoritative — always wins when present. |
| 2 | `unit_size`     | Parsed from `products.unit_size`, e.g. `"200 g"` → 0.2, `"0,5 l"` → 0.5, `"2 x 250 ml"` → 0.5, `"los per 500 g"` → 0.5. | The common case; covers most products. |
| 3 | `net_content`   | Parsed from `products.net_content` (total package weight). | Used for piece-based products that carry a net content. |
| 4 | `multipack`     | The base product of an "N-pack" title (e.g. "… 6-pack") × its unit weight. | |
| 5 | `default`       | A per-piece estimate from `default_weights.csv`, matched by title keyword. | Rough estimate for count-based products. |
| 6 | `serving_size`  | `products.serving_size` × piece count, **explicit multi-count packs only** (`"6 stuks"`, never `"per stuk"`). | **Last resort.** See rationale below. |

Liquids are treated as 1 l = 1 kg. Both the parsers and the policy live in the
[backend/weight](../backend/weight) package: parsing in
[weight.go](../backend/weight/weight.go) (`ParseKg`, `ParsePieceCount`) and the
policy in [resolution.go](../backend/weight/resolution.go) — the `SourcePriority`
ordering, the per-source `ServingSizeKg` rule, and the query-time `EffectiveKg`
fallback. The enricher and the analytics layer both consume these, so the order
and the rules have a single definition.

## Why this order

The list runs **most reliable first**. A manual correction is a deliberate human
decision; an actual measured quantity (`unit_size`, `net_content`) beats a derived
or estimated one; rough keyword estimates (`default`) sit near the bottom.

`serving_size` is **deliberately last** because it is the weakest signal: it is a
*per-serving* amount, not the package weight. For a single `"per stuk"` item it is
meaningless (a bread loaf is not one slice), so it is restricted to explicit
multi-count packs where `serving × count` is plausible, and it only applies when
**no other source produced a weight at all**. In practice this catches a handful of
products (e.g. multi-piece snack packs); everything else is weighed by a stronger
source first.

## No fallback weight

If **no** source yields a weight, the product is left **without** one. It is:

- **excluded from CO₂ totals** (a missing weight contributes no CO₂ rather than a
  guessed amount), and
- **listed in the missing-weight list** (`GET /corrections/missing-weight`, shown on
  the dashboard Data Quality page) so a weight can be added manually.

There is deliberately **no 1 kg/unit default** — a wrong weight is worse than a
known-missing one.

## Query-time fallback

At read time, `weight.EffectiveKg`
([backend/weight/resolution.go](../backend/weight/resolution.go)) returns the stored
`weight_kg` when present, otherwise parses `products.unit_size` live (same
`weight.ParseKg`), otherwise `nil`. This covers rows enriched before a weight could
be set; it applies no other source and no default.

## Transparency on the product page

`GET /products/{web_id}` returns `weight_source` plus a `weight_breakdown` array —
the candidate weight from every source in priority order, with the one actually used
marked `active`. The dashboard renders this as a compact "Weight by source" table so
it is visible why a product weighs what it does. See [api.md](api.md).

## Changing the policy

The order is defined in one place: `weight.SourcePriority`
([backend/weight/resolution.go](../backend/weight/resolution.go)). Both consumers
iterate it, so they cannot drift:

- **Enrichment** — `resolveWeights`
  ([backend/enricher/enricher.go](../backend/enricher/enricher.go)) maps each source
  to a `fill…Weights` function in
  [backend/enricher/weight.go](../backend/enricher/weight.go) that stamps its
  `weight_source`.
- **Product-page breakdown** — `buildWeightBreakdown`
  ([backend/analytics/queries.go](../backend/analytics/queries.go)) lists the
  candidate weights in the same order.

To add, remove, or reorder a source, edit `SourcePriority` (and, for a new source,
add its `Source…` constant and a fill function), then update this document.
