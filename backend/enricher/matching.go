package enricher

import (
	"database/sql"
	"encoding/json"
	"strings"

	"appie-insights/backend/schema"
)

func buildAHSubcategoryLookup(entries []ahSubcategoryEntry) map[string]ahSubcategoryEntry {
	m := make(map[string]ahSubcategoryEntry, len(entries))
	for _, e := range entries {
		m[strings.ToLower(e.ahSubcategory)] = e
	}
	return m
}

// ahNonFoodCategories lists AH top-level categories that never contain food.
// Products in these categories are marked non_food even if their subcategory is
// not in the subcategory map.
var ahNonFoodCategories = map[string]bool{
	"drogisterij":                true,
	"huishouden":                 true,
	"koken, tafelen, vrije tijd": true,
}

func matchCO2Category(webID int, db *sql.DB, ahSubcatLookup map[string]ahSubcategoryEntry) matchResult {
	var title, ahCategory, ahSubcategory, propertyIcons sql.NullString
	err := db.QueryRow(
		"SELECT title, ah_category, ah_subcategory, property_icons FROM products WHERE web_id = ?", webID,
	).Scan(&title, &ahCategory, &ahSubcategory, &propertyIcons)
	if err != nil {
		return noMatch(schema.MatchMethodNoProduct)
	}
	if title.String == "" && ahSubcategory.String == "" {
		return noMatch(schema.MatchMethodNoMetadata)
	}

	if ahSubcategory.String != "" {
		if r, ok := matchBySubcategory(ahSubcategory.String, propertyIcons.String, ahSubcatLookup); ok {
			return r
		}
	}

	// Subcategory not in map — fall back to AH top-level category to detect non-food.
	if ahNonFoodCategories[strings.ToLower(ahCategory.String)] {
		return noMatch(schema.MatchMethodNonFood)
	}
	return noMatch(schema.MatchMethodUnmatched)
}

// matchBySubcategory tries to match using the AH subcategory lookup, including a
// vegan-specific variant for vegan-icon products. Returns (result, true) when the
// subcategory is found in the lookup (even if it maps to non_food); (zero, false)
// when the subcategory is not in the map at all.
func matchBySubcategory(ahSubcategory, propertyIcons string, lookup map[string]ahSubcategoryEntry) (matchResult, bool) {
	if hasVeganIcon(propertyIcons) {
		veganKey := strings.ToLower(ahSubcategory) + " (vegan)"
		if sub, ok := lookup[veganKey]; ok && sub.co2PerKg != nil {
			return matchResult{
				name:     nilIfEmpty(sub.co2Subcategory),
				category: nilIfEmpty(sub.co2Category),
				co2PerKg: sub.co2PerKg,
				method:   schema.MatchMethodSubcategoryVegan,
			}, true
		}
	}
	sub, ok := lookup[strings.ToLower(ahSubcategory)]
	if !ok {
		return matchResult{}, false
	}
	if sub.co2PerKg != nil {
		return matchResult{
			name:     nilIfEmpty(sub.co2Subcategory),
			category: nilIfEmpty(sub.co2Category),
			co2PerKg: sub.co2PerKg,
			method:   schema.MatchMethodSubcategoryDirect,
		}, true
	}
	// In the map but no CO₂ factor → explicitly excluded (non-food, supplement, etc.)
	return noMatch(schema.MatchMethodNonFood), true
}

func hasVeganIcon(iconsJSON string) bool {
	if iconsJSON == "" {
		return false
	}
	var icons []string
	if err := json.Unmarshal([]byte(iconsJSON), &icons); err != nil {
		return false
	}
	for _, icon := range icons {
		if icon == "vegan" {
			return true
		}
	}
	return false
}
