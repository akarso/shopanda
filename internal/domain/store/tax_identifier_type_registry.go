package store

import (
	"fmt"
	"strings"
	"sync"
)

// TaxIdentifierTypeGeneric is the catch-all "Other" catalog code.
const TaxIdentifierTypeGeneric = "generic"

// TaxIdentifierTypeRegistry is an in-process catalog of tax/company ID types.
// Register fails closed on duplicate codes and after Freeze. Lookup order is
// registration order.
type TaxIdentifierTypeRegistry struct {
	mu     sync.RWMutex
	frozen bool
	order  []string
	byCode map[string]TaxIdentifierType
}

// NewTaxIdentifierTypeRegistry returns an empty registry.
func NewTaxIdentifierTypeRegistry() *TaxIdentifierTypeRegistry {
	return &TaxIdentifierTypeRegistry{
		byCode: make(map[string]TaxIdentifierType),
	}
}

// Freeze seals the registry so further Register calls fail closed.
func (r *TaxIdentifierTypeRegistry) Freeze() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frozen = true
}

// Frozen reports whether Register is sealed.
func (r *TaxIdentifierTypeRegistry) Frozen() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.frozen
}

// Register validates/normalizes t (same rules as NewTaxIdentifierType), then
// adds it. Duplicate Code (after normalize) and post-Freeze calls fail closed.
func (r *TaxIdentifierTypeRegistry) Register(t TaxIdentifierType) error {
	if r == nil {
		return fmt.Errorf("store tax identifier type registry: nil registry")
	}
	normalized, err := NewTaxIdentifierType(t.Code, t.Label, t.Countries, t.Region, t.FormatHint)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("store tax identifier type registry: frozen")
	}
	if _, exists := r.byCode[normalized.Code]; exists {
		return fmt.Errorf("store tax identifier type registry: duplicate code %q", normalized.Code)
	}
	r.byCode[normalized.Code] = normalized
	r.order = append(r.order, normalized.Code)
	return nil
}

// Get returns a type by code (trim + lower), or false when unknown.
func (r *TaxIdentifierTypeRegistry) Get(code string) (TaxIdentifierType, bool) {
	if r == nil {
		return TaxIdentifierType{}, false
	}
	code = strings.ToLower(strings.TrimSpace(code))
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byCode[code]
	if !ok {
		return TaxIdentifierType{}, false
	}
	return copyTaxIdentifierType(t), true
}

// All returns every registered type in registration order.
func (r *TaxIdentifierTypeRegistry) All() []TaxIdentifierType {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]TaxIdentifierType, 0, len(r.order))
	for _, code := range r.order {
		out = append(out, copyTaxIdentifierType(r.byCode[code]))
	}
	return out
}

// ApplicableTo returns types whose Countries list contains the normalized
// store country, then generic last. Region is grouping metadata for admin UI
// only — it does not widen membership. Invalid/empty country yields only
// generic (when registered). Empty-Countries plugin rows are omitted here;
// admin "Add other" uses All() minus this set.
func (r *TaxIdentifierTypeRegistry) ApplicableTo(country string) []TaxIdentifierType {
	if r == nil {
		return nil
	}
	cc, ccErr := NormalizeCountry(country)
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]TaxIdentifierType, 0, 4)
	seen := make(map[string]struct{}, len(r.order))
	add := func(t TaxIdentifierType) {
		if _, ok := seen[t.Code]; ok {
			return
		}
		seen[t.Code] = struct{}{}
		out = append(out, copyTaxIdentifierType(t))
	}

	if ccErr == nil {
		for _, code := range r.order {
			t := r.byCode[code]
			if t.Code == TaxIdentifierTypeGeneric {
				continue
			}
			if countryIn(t.Countries, cc) {
				add(t)
			}
		}
	}
	if g, ok := r.byCode[TaxIdentifierTypeGeneric]; ok {
		add(g)
	}
	return out
}

func countryIn(list []string, cc string) bool {
	for _, c := range list {
		if c == cc {
			return true
		}
	}
	return false
}

func copyTaxIdentifierType(t TaxIdentifierType) TaxIdentifierType {
	cp := t
	if t.Countries != nil {
		cp.Countries = append([]string(nil), t.Countries...)
	}
	return cp
}

func coreTaxIdentifierTypeDefs() ([]TaxIdentifierType, error) {
	eu := EUMemberCountries()
	defs := []struct {
		code, label string
		countries   []string
		region      string
		hint        string
	}{
		// Format hint uses a member prefix example (DE); real numbers use the
		// seller's member prefix (PL…, DE…, EL… for Greece — store country stays ISO GR).
		{"eu_vat", "EU VAT", eu, TaxIdentifierRegionEU, "DE123456789"},
		{"pl_nip", "NIP", []string{"PL"}, TaxIdentifierRegionEU, "1234567890"},
		{"de_ustidnr", "USt-IdNr.", []string{"DE"}, TaxIdentifierRegionEU, "DE123456789"},
		{"de_steuernummer", "Steuernummer", []string{"DE"}, TaxIdentifierRegionEU, ""},
		{"fr_siret", "SIRET", []string{"FR"}, TaxIdentifierRegionEU, "12345678901234"},
		{"fr_tva", "N° TVA", []string{"FR"}, TaxIdentifierRegionEU, "FR12345678901"},
		{"it_partita_iva", "Partita IVA", []string{"IT"}, TaxIdentifierRegionEU, "12345678901"},
		{"es_nif", "NIF / CIF", []string{"ES"}, TaxIdentifierRegionEU, ""},
		{"nl_kvk", "KVK", []string{"NL"}, TaxIdentifierRegionEU, ""},
		{"nl_btw", "BTW", []string{"NL"}, TaxIdentifierRegionEU, "NL123456789B01"},
		{"uk_vat", "VAT", []string{"GB"}, "", "GB123456789"},
		{"uk_crn", "Company number", []string{"GB"}, "", ""},
		{"us_ein", "EIN", []string{"US"}, TaxIdentifierRegionUS, "12-3456789"},
		{TaxIdentifierTypeGeneric, "Tax ID", nil, "", ""},
	}
	out := make([]TaxIdentifierType, 0, len(defs))
	for _, d := range defs {
		t, err := NewTaxIdentifierType(d.code, d.label, d.countries, d.region, d.hint)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// CoreTaxIdentifierTypeCodes is the §3.1 catalog order (for tests / docs sync).
func CoreTaxIdentifierTypeCodes() []string {
	return []string{
		"eu_vat", "pl_nip", "de_ustidnr", "de_steuernummer",
		"fr_siret", "fr_tva", "it_partita_iva", "es_nif",
		"nl_kvk", "nl_btw", "uk_vat", "uk_crn", "us_ein",
		TaxIdentifierTypeGeneric,
	}
}

// RegisterCoreTaxIdentifierTypes seeds the v1 catalog atomically. On failure
// the registry is left unchanged (no half-seed).
func RegisterCoreTaxIdentifierTypes(r *TaxIdentifierTypeRegistry) error {
	if r == nil {
		return fmt.Errorf("store tax identifier type registry: nil registry")
	}
	types, err := coreTaxIdentifierTypeDefs()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("store tax identifier type registry: frozen")
	}
	for _, t := range types {
		if _, exists := r.byCode[t.Code]; exists {
			return fmt.Errorf("store tax identifier type registry: duplicate code %q", t.Code)
		}
	}
	for _, t := range types {
		r.byCode[t.Code] = copyTaxIdentifierType(t)
		r.order = append(r.order, t.Code)
	}
	return nil
}

// DefaultTaxIdentifierTypeRegistry returns a registry seeded with the core catalog.
func DefaultTaxIdentifierTypeRegistry() *TaxIdentifierTypeRegistry {
	r := NewTaxIdentifierTypeRegistry()
	if err := RegisterCoreTaxIdentifierTypes(r); err != nil {
		panic("store: core tax identifier types: " + err.Error())
	}
	return r
}
