package weight

import "strings"

// Weight sources, in canonical priority order. Each enriched product's
// product_enrichment.weight_source records which one produced its weight_kg.
const (
	SourceCorrection  = "correction"   // manual entry in corrections.csv (authoritative)
	SourceUnitSize    = "unit_size"    // parsed from products.unit_size ("200 g")
	SourceNetContent  = "net_content"  // parsed from products.net_content (total package weight)
	SourceMultipack   = "multipack"    // base product of an "N-pack" × its unit weight
	SourceDefault     = "default"      // per-piece estimate from default_weights.csv
	SourceServingSize = "serving_size" // serving_size × piece count (last resort)
)

// SourcePriority is the single source of truth for weight-source ordering: most
// reliable first. Enrichment fills weights in this order and keeps the first that
// yields a value (see enricher.resolveWeights); the product-page breakdown lists
// candidates in this order (see analytics.buildWeightBreakdown). Changing the
// policy means changing this slice. See docs/weight-resolution.md for rationale.
var SourcePriority = []string{
	SourceCorrection,
	SourceUnitSize,
	SourceNetContent,
	SourceMultipack,
	SourceDefault,
	SourceServingSize,
}

// EffectiveKg resolves a per-unit weight in kg for query-time use. It returns the
// stored enrichment weight when present; otherwise it parses unitSize via ParseKg
// as a fallback (covering rows enriched before a weight could be set). It returns
// nil when neither yields a weight — there is deliberately no 1 kg/unit default,
// so weightless products surface in the missing-weight list rather than being
// counted at a guess.
func EffectiveKg(stored *float64, unitSize string) *float64 {
	if stored != nil {
		return stored
	}
	if kg, ok := ParseKg(unitSize); ok {
		return &kg
	}
	return nil
}

// ServingSizeKg derives a package weight from a serving size and the unit_size's
// piece count. It is the weakest signal and applies only to explicit multi-count
// packs ("6 stuks"): a single ambiguous unit ("per stuk", "per bosje") can hold
// many servings — a bread loaf is not one slice — so serving_size × 1 would badly
// under-weigh it and is rejected. Returns ok=false unless unitSize is an explicit
// "N stuks" count and servingSize parses to a weight.
func ServingSizeKg(unitSize, servingSize string) (float64, bool) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(unitSize)), "per ") {
		return 0, false // single ambiguous unit — serving size ≠ product weight
	}
	count, ok := ParsePieceCount(unitSize)
	if !ok {
		return 0, false // require an explicit "N stuks" count
	}
	servKg, ok := ParseKg(servingSize)
	if !ok {
		return 0, false
	}
	return float64(count) * servKg, true
}
