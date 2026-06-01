package enricher

import (
	"database/sql"
	"strings"
)

// CorrectionStatus annotates a Correction with redundancy information.
type CorrectionStatus struct {
	Correction
	// Redundant is true when action="set_category" and the subcategory map
	// would now yield the same category and name without the correction.
	Redundant  bool   `json:"redundant"`
	AutoMethod string `json:"auto_method,omitempty"`
}

// CheckRedundancy annotates corrections with whether each can now be derived
// automatically from the product's AH subcategory map, making the correction
// redundant.
func CheckRedundancy(dataDir string, db *sql.DB, corrections []Correction) ([]CorrectionStatus, error) {
	entries, err := loadAHSubcategoryMap(dataDir)
	if err != nil {
		return nil, err
	}
	lookup := buildAHSubcategoryLookup(entries)

	out := make([]CorrectionStatus, len(corrections))
	for i, c := range corrections {
		cs := CorrectionStatus{Correction: c}
		if c.Action == "set_category" {
			auto := matchCO2Category(c.WebID, db, lookup)
			cs.AutoMethod = auto.method
			autoCategory := ""
			if auto.category != nil {
				autoCategory = *auto.category
			}
			autoName := ""
			if auto.name != nil {
				autoName = *auto.name
			}
			cs.Redundant = strings.EqualFold(autoCategory, c.CO2Category) &&
				strings.EqualFold(autoName, c.CO2Name)
		}
		out[i] = cs
	}
	return out, nil
}
