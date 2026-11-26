package store

import (
	"fmt"
	"sync"
)

// TaxIdentifierTypeRegistry is an in-process catalog of tax/company ID types.
// Register fails closed on duplicate codes. Lookup order is registration order.
type TaxIdentifierTypeRegistry struct {
	mu     sync.RWMutex
	order  []string
	byCode map[string]TaxIdentifierType
}

// NewTaxIdentifierTypeRegistry returns an empty registry.
func NewTaxIdentifierTypeRegistry() *TaxIdentifierTypeRegistry {
	return &TaxIdentifierTypeRegistry{
		byCode: make(map[string]TaxIdentifierType),
	}
}

// Register adds a type. Duplicate Code returns an error (fail closed).
func (r *TaxIdentifierTypeRegistry) Register(t TaxIdentifierType) error {
	if r == nil {
		return fmt.Errorf("store tax identifier type registry: nil registry")
	}
	if t.Code == "" {
		return fmt.Errorf("store tax identifier type registry: empty code")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byCode[t.Code]; exists {
		return fmt.Errorf("store tax identifier type registry: duplicate code %q", t.Code)
	}
	// Defensive copy of Countries so callers cannot mutate the stored slice.
	cp := t
	if t.Countries != nil {
		cp.Countries = append([]string(nil), t.Countries...)
	}
	r.byCode[t.Code] = cp
	r.order = append(r.order, t.Code)
	return nil
}

// Get returns a type by code, or false when unknown.
func (r *TaxIdentifierTypeRegistry) Get(code string) (TaxIdentifierType, bool) {
	if r == nil {
		return TaxIdentifierType{}, false
	}
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

// ApplicableTo returns types for a store country: country match, region match
// (EU members see Region=EU types; US sees Region=US), plus generic.
// Invalid/empty country yields only generic (when registered).
func (r *TaxIdentifierTypeRegistry) ApplicableTo(country string) []TaxIdentifierType {
	if r == nil {
		return nil
	}
	cc, ccErr := NormalizeCountry(country)
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]TaxIdentifierType, 0, len(r.order))
	seen := make(map[string]struct{}, len(r.order))
	add := func(t TaxIdentifierType) {
		if _, ok := seen[t.Code]; ok {
			return
		}
		seen[t.Code] = struct{}{}
		out = append(out, copyTaxIdentifierType(t))
	}

	for _, code := range r.order {
		t := r.byCode[code]
		if t.Code == "generic" {
			continue // always appended last
		}
		if ccErr != nil {
			continue
		}
		if countryIn(t.Countries, cc) {
			add(t)
			continue
		}
		if t.Region == TaxIdentifierRegionEU && IsEUCountry(cc) {
			add(t)
			continue
		}
		if t.Region == TaxIdentifierRegionUS && cc == "US" {
			add(t)
		}
	}
	if g, ok := r.byCode["generic"]; ok {
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

// RegisterCoreTaxIdentifierTypes seeds the v1 catalog from STORE_IDENTITY §3.1.
func RegisterCoreTaxIdentifierTypes(r *TaxIdentifierTypeRegistry) error {
	if r == nil {
		return fmt.Errorf("store tax identifier type registry: nil registry")
	}
	eu := EUMemberCountries()
	defs := []struct {
		code, label string
		countries   []string
		region      string
		hint        string
	}{
		{"eu_vat", "EU VAT", eu, TaxIdentifierRegionEU, "EU123456789"},
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
		{"generic", "Tax ID", nil, "", ""},
	}
	for _, d := range defs {
		t, err := NewTaxIdentifierType(d.code, d.label, d.countries, d.region, d.hint)
		if err != nil {
			return err
		}
		if err := r.Register(t); err != nil {
			return err
		}
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
