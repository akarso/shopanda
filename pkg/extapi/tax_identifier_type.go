package extapi

import "sort"

// TaxIdentifierType is a plugin-facing tax/company identifier catalog entry
// (PR-1201). Code must be a snake token (e.g. pl_nip).
//
// Region is grouping metadata for admin UI only. To appear for every EU store,
// pass Countries: EUMemberCountries() — do not rely on Region alone.
type TaxIdentifierType struct {
	Code       string
	Label      string
	Countries  []string // ISO-2; empty = not country-matched by ApplicableTo
	Region     string   // "EU" | "US" | ""
	FormatHint string
}

// euMemberCountries is the ISO 3166-1 alpha-2 set for EU member states (27).
// Keep in sync with internal/domain/store.EUMemberCountries (tested).
var euMemberCountries = map[string]struct{}{
	"AT": {}, "BE": {}, "BG": {}, "HR": {}, "CY": {}, "CZ": {}, "DK": {},
	"EE": {}, "FI": {}, "FR": {}, "DE": {}, "GR": {}, "HU": {}, "IE": {},
	"IT": {}, "LV": {}, "LT": {}, "LU": {}, "MT": {}, "NL": {}, "PL": {},
	"PT": {}, "RO": {}, "SK": {}, "SI": {}, "ES": {}, "SE": {},
}

// EUMemberCountries returns a sorted copy of EU ISO-2 codes so product plugins
// can register a type for every EU store without importing internal/domain/store.
// Note: Greece is ISO GR (EU VAT numbers use the EL prefix separately).
func EUMemberCountries() []string {
	out := make([]string, 0, len(euMemberCountries))
	for c := range euMemberCountries {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
