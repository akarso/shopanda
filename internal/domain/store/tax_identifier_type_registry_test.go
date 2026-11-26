package store_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/pkg/extapi"
)

func codesOf(types []store.TaxIdentifierType) []string {
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = t.Code
	}
	return out
}

func TestDefaultTaxIdentifierTypeRegistry_ApplicableTo(t *testing.T) {
	r := store.DefaultTaxIdentifierTypeRegistry()

	tests := []struct {
		country string
		want    []string
	}{
		{"PL", []string{"eu_vat", "pl_nip", store.TaxIdentifierTypeGeneric}},
		{"pl", []string{"eu_vat", "pl_nip", store.TaxIdentifierTypeGeneric}},
		{"DE", []string{"eu_vat", "de_ustidnr", "de_steuernummer", store.TaxIdentifierTypeGeneric}},
		{"FR", []string{"eu_vat", "fr_siret", "fr_tva", store.TaxIdentifierTypeGeneric}},
		{"GB", []string{"uk_vat", "uk_crn", store.TaxIdentifierTypeGeneric}},
		{"UK", []string{"uk_vat", "uk_crn", store.TaxIdentifierTypeGeneric}}, // UK→GB
		{"US", []string{"us_ein", store.TaxIdentifierTypeGeneric}},
		{"XX", []string{store.TaxIdentifierTypeGeneric}},
		{"", []string{store.TaxIdentifierTypeGeneric}},
		{"12", []string{store.TaxIdentifierTypeGeneric}},
	}
	for _, tt := range tests {
		t.Run(tt.country, func(t *testing.T) {
			got := codesOf(r.ApplicableTo(tt.country))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ApplicableTo(%q) = %v, want %v", tt.country, got, tt.want)
			}
		})
	}
}

func TestDefaultTaxIdentifierTypeRegistry_CoreCodes(t *testing.T) {
	r := store.DefaultTaxIdentifierTypeRegistry()
	got := codesOf(r.All())
	want := store.CoreTaxIdentifierTypeCodes()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("All codes = %v, want %v", got, want)
	}
}

func TestTaxIdentifierTypeRegistry_GetCanonical(t *testing.T) {
	r := store.DefaultTaxIdentifierTypeRegistry()
	t1, ok := r.Get("PL_NIP")
	if !ok || t1.Code != "pl_nip" {
		t.Fatalf("Get(PL_NIP) = (%v, %v)", t1, ok)
	}
	t2, ok := r.Get("  Eu_Vat ")
	if !ok || t2.Code != "eu_vat" {
		t.Fatalf("Get(Eu_Vat) = (%v, %v)", t2, ok)
	}
}

func TestTaxIdentifierTypeRegistry_RegisterNormalizesBeforeDuplicate(t *testing.T) {
	r := store.DefaultTaxIdentifierTypeRegistry()
	err := r.Register(store.TaxIdentifierType{
		Code: "EU_VAT", Label: "Dup", Countries: []string{"pl"}, Region: "eu",
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate of eu_vat, got %v", err)
	}
}

func TestTaxIdentifierTypeRegistry_RegisterRawStructNormalized(t *testing.T) {
	r := store.NewTaxIdentifierTypeRegistry()
	err := r.Register(store.TaxIdentifierType{
		Code: "Custom_VAT", Label: " Custom ", Countries: []string{"pl"}, Region: "eu",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := r.Get("custom_vat")
	if !ok || got.Code != "custom_vat" || got.Countries[0] != "PL" || got.Region != store.TaxIdentifierRegionEU {
		t.Fatalf("normalized = %+v", got)
	}
	if !codesEqual(codesOf(r.ApplicableTo("PL")), []string{"custom_vat"}) {
		// no generic seeded
		t.Fatalf("ApplicableTo PL = %v", codesOf(r.ApplicableTo("PL")))
	}
}

func codesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTaxIdentifierTypeRegistry_Freeze(t *testing.T) {
	r := store.NewTaxIdentifierTypeRegistry()
	r.Freeze()
	err := r.Register(store.TaxIdentifierType{Code: "x", Label: "X"})
	if err == nil || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("frozen register = %v", err)
	}
	// Bad payload after freeze must still surface as frozen, not validation.
	err = r.Register(store.TaxIdentifierType{Code: "bad code", Label: ""})
	if err == nil || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("frozen+invalid = %v, want frozen", err)
	}
}

func TestTaxIdentifierTypeRegistry_AllOrderAndCopy(t *testing.T) {
	r := store.DefaultTaxIdentifierTypeRegistry()
	all := r.All()
	all[0].Countries[0] = "ZZ"
	again, ok := r.Get("eu_vat")
	if !ok || again.Countries[0] == "ZZ" {
		t.Fatal("All/Get must return copies")
	}
}

func TestEUMemberCountries_MatchExtapi(t *testing.T) {
	storeEU := store.EUMemberCountries()
	extEU := extapi.EUMemberCountries()
	if !reflect.DeepEqual(storeEU, extEU) {
		t.Fatalf("store vs extapi EU lists diverge:\nstore=%v\nextapi=%v", storeEU, extEU)
	}
	if len(storeEU) != 27 {
		t.Fatalf("EU member count = %d, want 27", len(storeEU))
	}
	required := []string{"PL", "DE", "FR", "GR", "IT", "ES", "NL"}
	set := make(map[string]struct{}, len(storeEU))
	for _, c := range storeEU {
		set[c] = struct{}{}
	}
	for _, c := range required {
		if _, ok := set[c]; !ok {
			t.Fatalf("missing EU member %q", c)
		}
	}
	for _, bad := range []string{"UK", "EL", "GB", "US"} {
		if _, ok := set[bad]; ok {
			t.Fatalf("%q must not be in EU member list", bad)
		}
	}
}

func TestNewTaxIdentifierType_Validation(t *testing.T) {
	if _, err := store.NewTaxIdentifierType("PL NIP", "NIP", nil, "", ""); err == nil {
		t.Fatal("expected bad code")
	}
	if _, err := store.NewTaxIdentifierType("ok", "", nil, "", ""); err == nil {
		t.Fatal("expected empty label")
	}
	if _, err := store.NewTaxIdentifierType("ok", "L", []string{"P"}, "", ""); err == nil {
		t.Fatal("expected bad country")
	}
	if _, err := store.NewTaxIdentifierType("ok", "L", nil, "APAC", ""); err == nil {
		t.Fatal("expected bad region")
	}
}

func TestIsEUCountry(t *testing.T) {
	if !store.IsEUCountry("pl") || store.IsEUCountry("US") || store.IsEUCountry("GB") {
		t.Fatal("EU membership mismatch")
	}
}
