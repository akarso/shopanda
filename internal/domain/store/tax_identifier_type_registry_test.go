package store_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/store"
)

func codesOf(types []store.TaxIdentifierType) []string {
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = t.Code
	}
	return out
}

func hasCode(types []store.TaxIdentifierType, code string) bool {
	for _, t := range types {
		if t.Code == code {
			return true
		}
	}
	return false
}

func TestDefaultTaxIdentifierTypeRegistry_ApplicableTo(t *testing.T) {
	r := store.DefaultTaxIdentifierTypeRegistry()

	pl := r.ApplicableTo("pl")
	if !hasCode(pl, "pl_nip") || !hasCode(pl, "eu_vat") {
		t.Fatalf("PL applicable = %v, want pl_nip + eu_vat", codesOf(pl))
	}
	if !hasCode(pl, "generic") {
		t.Fatalf("PL applicable missing generic: %v", codesOf(pl))
	}

	us := r.ApplicableTo("US")
	if !hasCode(us, "us_ein") {
		t.Fatalf("US applicable = %v, want us_ein", codesOf(us))
	}
	if hasCode(us, "pl_nip") {
		t.Fatalf("US must not see pl_nip: %v", codesOf(us))
	}

	xx := r.ApplicableTo("XX") // valid ISO letters, not in catalog countries
	if len(xx) != 1 || xx[0].Code != "generic" {
		t.Fatalf("unknown country applicable = %v, want [generic]", codesOf(xx))
	}

	bad := r.ApplicableTo("12")
	if len(bad) != 1 || bad[0].Code != "generic" {
		t.Fatalf("invalid country applicable = %v, want [generic]", codesOf(bad))
	}
}

func TestTaxIdentifierTypeRegistry_RegisterDuplicate(t *testing.T) {
	r := store.NewTaxIdentifierTypeRegistry()
	t1, err := store.NewTaxIdentifierType("custom_vat", "Custom", []string{"PL"}, store.TaxIdentifierRegionEU, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Register(t1); err != nil {
		t.Fatal(err)
	}
	err = r.Register(t1)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestTaxIdentifierTypeRegistry_AllOrderAndCopy(t *testing.T) {
	r := store.DefaultTaxIdentifierTypeRegistry()
	all := r.All()
	if len(all) < 14 {
		t.Fatalf("core catalog len = %d", len(all))
	}
	if all[0].Code != "eu_vat" || all[len(all)-1].Code != "generic" {
		t.Fatalf("order: first=%q last=%q", all[0].Code, all[len(all)-1].Code)
	}
	all[0].Countries[0] = "ZZ"
	again, ok := r.Get("eu_vat")
	if !ok || again.Countries[0] == "ZZ" {
		t.Fatal("All/Get must return copies")
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
