package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Tax identifier type regions (grouping for admin "applicable" lists).
const (
	TaxIdentifierRegionEU = "EU"
	TaxIdentifierRegionUS = "US"
)

// TaxIdentifierType describes one registry entry (NIP, EU VAT, EIN, …).
// Code is a snake token matching TaxIdentifier.Type.
// Region is admin grouping only — ApplicableTo matches Countries, not Region.
// Empty Countries means the type is not country-applicable (except
// TaxIdentifierTypeGeneric, which ApplicableTo always appends last); admin
// "Add other" uses All() minus ApplicableTo.
type TaxIdentifierType struct {
	Code       string
	Label      string
	Countries  []string // ISO-2; empty = not country-matched (see ApplicableTo)
	Region     string   // TaxIdentifierRegionEU | TaxIdentifierRegionUS | ""
	FormatHint string
}

// NewTaxIdentifierType validates and normalizes a type definition.
func NewTaxIdentifierType(code, label string, countries []string, region, formatHint string) (TaxIdentifierType, error) {
	codeNorm, err := normalizeSingleLine(code, MaxTaxTypeLen, "type code")
	if err != nil {
		return TaxIdentifierType{}, fmt.Errorf("store tax identifier type: %w", err)
	}
	codeNorm = strings.ToLower(codeNorm)
	if codeNorm == "" {
		return TaxIdentifierType{}, errors.New("store tax identifier type: code must not be empty")
	}
	if !taxTypePattern.MatchString(codeNorm) {
		return TaxIdentifierType{}, errors.New("store tax identifier type: code must be a snake token (e.g. pl_nip)")
	}
	labelNorm, err := normalizeSingleLine(label, MaxLegalNameLen, "label")
	if err != nil {
		return TaxIdentifierType{}, fmt.Errorf("store tax identifier type: %w", err)
	}
	if labelNorm == "" {
		return TaxIdentifierType{}, errors.New("store tax identifier type: label must not be empty")
	}
	hint, err := normalizeSingleLine(formatHint, MaxTaxValueLen, "format hint")
	if err != nil {
		return TaxIdentifierType{}, fmt.Errorf("store tax identifier type: %w", err)
	}
	region = strings.ToUpper(strings.TrimSpace(region))
	switch region {
	case "", TaxIdentifierRegionEU, TaxIdentifierRegionUS:
	default:
		return TaxIdentifierType{}, fmt.Errorf("store tax identifier type: invalid region %q", region)
	}
	normCountries := make([]string, 0, len(countries))
	seen := make(map[string]struct{}, len(countries))
	for _, c := range countries {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		cc, err := NormalizeCountry(c)
		if err != nil {
			return TaxIdentifierType{}, fmt.Errorf("store tax identifier type: %w", err)
		}
		if _, ok := seen[cc]; ok {
			continue
		}
		seen[cc] = struct{}{}
		normCountries = append(normCountries, cc)
	}
	return TaxIdentifierType{
		Code:       codeNorm,
		Label:      labelNorm,
		Countries:  normCountries,
		Region:     region,
		FormatHint: hint,
	}, nil
}

// euMemberCountries is the ISO 3166-1 alpha-2 set for EU member states (27).
var euMemberCountries = map[string]struct{}{
	"AT": {}, "BE": {}, "BG": {}, "HR": {}, "CY": {}, "CZ": {}, "DK": {},
	"EE": {}, "FI": {}, "FR": {}, "DE": {}, "GR": {}, "HU": {}, "IE": {},
	"IT": {}, "LV": {}, "LT": {}, "LU": {}, "MT": {}, "NL": {}, "PL": {},
	"PT": {}, "RO": {}, "SK": {}, "SI": {}, "ES": {}, "SE": {},
}

// EUMemberCountries returns a sorted copy of EU ISO-2 codes (for catalog seed).
func EUMemberCountries() []string {
	out := make([]string, 0, len(euMemberCountries))
	for c := range euMemberCountries {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// IsEUCountry reports whether code is an EU member ISO-2 (after normalize).
func IsEUCountry(code string) bool {
	cc, err := NormalizeCountry(code)
	if err != nil {
		return false
	}
	_, ok := euMemberCountries[cc]
	return ok
}
