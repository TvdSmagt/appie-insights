package enricher

import (
	"database/sql"
	"regexp"
	"sort"
	"strings"

	"appie-insights/backend/schema"
	"appie-insights/backend/weight"
)

const updateWeightSQL = "UPDATE product_enrichment SET weight_kg = ?, weight_source = ? WHERE web_id = ?"

// weightCandidateFilter selects enriched products that still need a weight.
// Embedded in every weight-fill query.
const weightCandidateFilter = `(pe.weight_kg IS NULL OR pe.weight_kg = 0) AND pe.match_method != '` + schema.MatchMethodIgnored + `'`

var packSuffixRE = regexp.MustCompile(`(?i)\s+\d+-pack$`)

func applyDefaultWeight(title, unitSize string, weights []defaultWeight) (float64, bool) {
	if title == "" || len(weights) == 0 {
		return 0, false
	}
	count, ok := weight.ParsePieceCount(unitSize)
	if !ok {
		return 0, false
	}
	lower := strings.ToLower(title)

	sorted := make([]defaultWeight, len(weights))
	copy(sorted, weights)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].matchKey) > len(sorted[j].matchKey)
	})
	for _, w := range sorted {
		if w.matchType == "keyword" && strings.Contains(lower, strings.ToLower(w.matchKey)) {
			return float64(count) * w.weightPerPieceKg, true
		}
	}
	return 0, false
}

// weightCandidate is one product row selected for weight resolution. cols holds
// the query's columns after web_id, in order, with NULLs mapped to "".
type weightCandidate struct {
	webID int
	cols  []string
}

// fillWeights runs query, then for each row invokes resolve to compute a weight,
// writing every non-skipped result to product_enrichment.weight_kg and stamping
// weight_source with source. query must select web_id first; nCols is the number
// of trailing string columns passed to resolve. Rows are fully read and closed
// before any write/resolve runs, so resolve may itself query the DB (SQLite
// holds a single connection).
func fillWeights(db *sql.DB, query string, nCols int, source string, resolve func(c weightCandidate) (float64, bool)) (int, error) {
	candidates, err := collectWeightCandidates(db, query, nCols)
	if err != nil {
		return 0, err
	}
	var updated int
	for _, c := range candidates {
		w, ok := resolve(c)
		if !ok {
			continue
		}
		if _, err := db.Exec(updateWeightSQL, w, source, c.webID); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func collectWeightCandidates(db *sql.DB, query string, nCols int) ([]weightCandidate, error) {
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []weightCandidate
	for rows.Next() {
		var webID int
		strs := make([]sql.NullString, nCols)
		dest := make([]any, nCols+1)
		dest[0] = &webID
		for i := range strs {
			dest[i+1] = &strs[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		cols := make([]string, nCols)
		for i, s := range strs {
			cols[i] = s.String
		}
		out = append(out, weightCandidate{webID: webID, cols: cols})
	}
	return out, rows.Err()
}

// fillUnitSizeWeights derives weight_kg directly from unit_size for products
// whose unit_size encodes a quantity (e.g. "0,5 l", "200 g", "2 x 250 ml").
// Piece-based products (per stuk, stuks) are handled by fillServingSizeWeights
// and fillNetContentWeights instead and are excluded here.
func fillUnitSizeWeights(db *sql.DB) (int, error) {
	return fillWeights(db, `
		SELECT p.web_id, p.unit_size
		FROM products p
		JOIN product_enrichment pe ON pe.web_id = p.web_id
		WHERE `+weightCandidateFilter+`
		  AND p.unit_size IS NOT NULL
		  AND p.unit_size != ''
		  AND LOWER(TRIM(p.unit_size)) NOT LIKE '%stuk%'
		  AND LOWER(TRIM(p.unit_size)) NOT LIKE 'per %'`, 1, weight.SourceUnitSize,
		func(c weightCandidate) (float64, bool) {
			return weight.ParseKg(c.cols[0])
		})
}

// fillNetContentWeights derives weight_kg from net_content for piece-based
// products (e.g. unit_size="25 stuks", net_content="50.0 Gram" → weight_kg=0.050).
// net_content is the total package weight, so it maps directly to weight_kg with
// no division — consistent with how unit_size weights like "500 g" are used.
// net_content is populated by the syncer's backfillNetContents step.
func fillNetContentWeights(db *sql.DB) (int, error) {
	return fillWeights(db, `
		SELECT p.web_id, p.net_content
		FROM products p
		JOIN product_enrichment pe ON pe.web_id = p.web_id
		WHERE `+weightCandidateFilter+`
		  AND p.net_content IS NOT NULL
		  AND p.net_content != ''`, 1, weight.SourceNetContent,
		func(c weightCandidate) (float64, bool) {
			return weight.ParseKg(c.cols[0])
		})
}

// fillServingSizeWeights derives weight_kg from serving_size for explicit
// multi-count packs (e.g. unit_size="6 stuks", serving_size="70 gram" →
// weight_kg=0.420). It deliberately skips ambiguous single units like
// "per stuk"/"per bosje": a single unit can hold many servings (a bread loaf is
// not one slice), so serving_size × 1 would badly under-weigh it. Those products
// fall through to default weights or the missing-weight list instead.
// serving_size is populated by the syncer's backfillServingSizes step.
func fillServingSizeWeights(db *sql.DB) (int, error) {
	return fillWeights(db, `
		SELECT p.web_id, p.unit_size, p.serving_size
		FROM products p
		JOIN product_enrichment pe ON pe.web_id = p.web_id
		WHERE `+weightCandidateFilter+`
		  AND p.serving_size IS NOT NULL
		  AND p.serving_size != ''`, 2, weight.SourceServingSize,
		func(c weightCandidate) (float64, bool) {
			return weight.ServingSizeKg(c.cols[0], c.cols[1])
		})
}

func fillMultipackWeights(db *sql.DB) (int, error) {
	return fillWeights(db, `
		SELECT p.web_id, p.title, p.unit_size
		FROM products p
		JOIN product_enrichment pe ON pe.web_id = p.web_id
		WHERE `+weightCandidateFilter+`
		  AND LOWER(p.title) LIKE '%-pack'
		  AND LOWER(p.unit_size) LIKE '%stuk%'`, 2, weight.SourceMultipack,
		func(c weightCandidate) (float64, bool) {
			return multipackWeight(db, c.webID, c.cols[0], c.cols[1])
		})
}

func fillDefaultWeights(db *sql.DB, weights []defaultWeight) (int, error) {
	if len(weights) == 0 {
		return 0, nil
	}
	return fillWeights(db, `
		SELECT p.web_id, p.title, p.unit_size
		FROM products p
		JOIN product_enrichment pe ON pe.web_id = p.web_id
		WHERE `+weightCandidateFilter, 2, weight.SourceDefault,
		func(c weightCandidate) (float64, bool) {
			return applyDefaultWeight(c.cols[0], c.cols[1], weights)
		})
}

func multipackWeight(db *sql.DB, webID int, title, unitSize string) (float64, bool) {
	count, ok := weight.ParsePieceCount(unitSize)
	if !ok {
		return 0, false
	}
	baseTitle := packSuffixRE.ReplaceAllString(title, "")
	if baseTitle == title {
		return 0, false
	}
	rows, err := db.Query(
		"SELECT unit_size FROM products WHERE title = ? AND web_id != ?", baseTitle, webID,
	)
	if err != nil {
		return 0, false
	}
	defer rows.Close()
	for rows.Next() {
		var s sql.NullString
		if err := rows.Scan(&s); err != nil {
			continue
		}
		if w, ok := weight.ParseKg(s.String); ok {
			return float64(count) * w, true
		}
	}
	return 0, false
}
