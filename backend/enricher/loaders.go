package enricher

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func dataPath(dir, name string) string {
	return filepath.Join(dir, name)
}

func csvIndex(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, h := range header {
		m[h] = i
	}
	return m
}

func csvGet(row []string, idx map[string]int, key string) string {
	if i, ok := idx[key]; ok && i < len(row) {
		return row[i]
	}
	return ""
}

func LoadCO2EqCategories(dir string) ([]CO2Entry, error) {
	path := dataPath(dir, "co2eq_categories.csv")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open co2eq_categories.csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := csvIndex(header)

	var entries []CO2Entry
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read co2eq_categories.csv: %w", err)
		}
		co2, _ := strconv.ParseFloat(csvGet(row, idx, "co2eq_per_kg"), 64)
		entries = append(entries, CO2Entry{
			Name:        csvGet(row, idx, "name"),
			Category:    csvGet(row, idx, "category"),
			Subcategory: csvGet(row, idx, "subcategory"),
			CO2PerKg:    co2,
			Source:      csvGet(row, idx, "source"),
			Notes:       csvGet(row, idx, "notes"),
		})
	}
	return entries, nil
}

func loadAHSubcategoryMap(dir string) ([]ahSubcategoryEntry, error) {
	path := dataPath(dir, "ah_subcategory_map.csv")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open ah_subcategory_map.csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	// Some AH subcategory names contain unquoted commas (e.g.
	// "koken, tafelen, vrije tijd"), so rows can have more fields than the
	// header. FieldsPerRecord=-1 disables the column-count check; the overflow
	// is rejoined into the first column below. This assumes the extra commas
	// only ever occur in the first column (ah_subcategory) — true for the
	// current file, but fragile if later columns gain commas.
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	nCols := len(header)
	idx := csvIndex(header)

	var entries []ahSubcategoryEntry
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read ah_subcategory_map.csv: %w", err)
		}
		// Rejoin a comma-split first column (see FieldsPerRecord note above).
		if len(row) > nCols {
			extra := len(row) - nCols
			row = append([]string{strings.Join(row[:extra+1], ",")}, row[extra+1:]...)
		}
		entries = append(entries, ahSubcategoryEntry{
			ahSubcategory:  csvGet(row, idx, "ah_subcategory"),
			co2Category:    csvGet(row, idx, "co2eq_category"),
			co2Subcategory: csvGet(row, idx, "co2eq_subcategory"),
			co2PerKg:       parseOptFloat(csvGet(row, idx, "co2eq_per_kg")),
		})
	}
	return entries, nil
}

func loadDefaultWeights(dir string) ([]defaultWeight, error) {
	path := dataPath(dir, "default_weights.csv")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open default_weights.csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := csvIndex(header)

	var entries []defaultWeight
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read default_weights.csv: %w", err)
		}
		w, _ := strconv.ParseFloat(csvGet(row, idx, "weight_per_piece_kg"), 64)
		entries = append(entries, defaultWeight{
			matchKey:         csvGet(row, idx, "match_key"),
			matchType:        csvGet(row, idx, "match_type"),
			weightPerPieceKg: w,
		})
	}
	return entries, nil
}

func LoadCorrections(dir string) ([]Correction, error) {
	path := dataPath(dir, "corrections.csv")
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open corrections.csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := csvIndex(header)

	var entries []Correction
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read corrections.csv: %w", err)
		}
		webID, _ := strconv.Atoi(csvGet(row, idx, "web_id"))
		c := Correction{
			WebID:       webID,
			Action:      csvGet(row, idx, "action"),
			CO2Category: csvGet(row, idx, "co2eq_category"),
			CO2Name:     csvGet(row, idx, "co2eq_name"),
			Notes:       csvGet(row, idx, "notes"),
		}
		c.CO2PerKg = parseOptFloat(csvGet(row, idx, "co2eq_per_kg"))
		c.WeightKg = parseOptFloat(csvGet(row, idx, "weight_kg"))
		entries = append(entries, c)
	}
	return entries, nil
}

func parseOptFloat(s string) *float64 {
	if s == "" {
		return nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return &f
	}
	return nil
}

func SaveCorrections(dir string, entries []Correction) error {
	path := dataPath(dir, "corrections.csv")
	tmp := path + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	w := csv.NewWriter(f)
	fields := []string{"web_id", "action", "co2eq_per_kg", "weight_kg", "co2eq_category", "co2eq_name", "notes"}
	if err := w.Write(fields); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}

	sorted := make([]Correction, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].WebID < sorted[j].WebID })

	for _, c := range sorted {
		co2str := ""
		if c.CO2PerKg != nil {
			co2str = strconv.FormatFloat(*c.CO2PerKg, 'f', -1, 64)
		}
		wstr := ""
		if c.WeightKg != nil {
			wstr = strconv.FormatFloat(*c.WeightKg, 'f', -1, 64)
		}
		if err := w.Write([]string{
			strconv.Itoa(c.WebID), c.Action, co2str, wstr, c.CO2Category, c.CO2Name, c.Notes,
		}); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
