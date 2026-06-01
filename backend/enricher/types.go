package enricher

// CO2Entry is a single row from co2eq_categories.csv.
type CO2Entry struct {
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Subcategory string  `json:"subcategory"`
	CO2PerKg    float64 `json:"co2eq_per_kg"`
	Source      string  `json:"source"`
	Notes       string  `json:"notes"`
}

// Correction is a manual override from corrections.csv.
type Correction struct {
	WebID       int      `json:"web_id"`
	Action      string   `json:"action"`
	CO2PerKg    *float64 `json:"co2eq_per_kg"`
	WeightKg    *float64 `json:"weight_kg"`
	CO2Category string   `json:"co2eq_category"`
	CO2Name     string   `json:"co2eq_name"`
	Notes       string   `json:"notes"`
}

type ahSubcategoryEntry struct {
	ahSubcategory  string
	co2Category    string
	co2Subcategory string
	co2PerKg       *float64
}

type defaultWeight struct {
	matchKey         string
	matchType        string
	weightPerPieceKg float64
}

// matchResult holds the outcome of a CO2eq category match attempt.
type matchResult struct {
	name     *string
	category *string
	co2PerKg *float64
	method   string
}

func noMatch(method string) matchResult { return matchResult{method: method} }

func f64Ptr(f float64) *float64 { return &f }

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
