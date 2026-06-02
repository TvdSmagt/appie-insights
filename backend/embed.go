package main

import (
	"embed"
	"io/fs"

	"appie-insights/backend/enricher"
)

// embeddedData bakes the canonical enrichment CSVs into the binary so a
// self-contained build needs no files shipped alongside it. On-disk copies
// under ENRICHMENT_DATA_DIR still take precedence (see enricher.openData), so
// users can override individual files without rebuilding.
//
//go:embed data/*.csv
var embeddedData embed.FS

func init() {
	// Expose the CSVs at their base names (e.g. "co2eq_categories.csv") to
	// match how the loaders look them up.
	sub, err := fs.Sub(embeddedData, "data")
	if err != nil {
		panic("embed: " + err.Error())
	}
	enricher.EmbeddedData = sub
}
