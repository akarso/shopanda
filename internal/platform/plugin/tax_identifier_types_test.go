package plugin_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestApp_RegisterTaxIdentifierType(t *testing.T) {
	app := &plugin.App{}
	reg := app.TaxIdentifierTypes()
	got := codes(reg.ApplicableTo("PL"))
	want := []string{"eu_vat", "pl_nip", store.TaxIdentifierTypeGeneric}
	if !equal(got, want) {
		t.Fatalf("core PL = %v, want %v", got, want)
	}

	custom, err := store.NewTaxIdentifierType("acme_vat", "Acme VAT", []string{"PL"}, store.TaxIdentifierRegionEU, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterTaxIdentifierType(custom); err != nil {
		t.Fatal(err)
	}
	if !has(app.TaxIdentifierTypes().ApplicableTo("PL"), "acme_vat") {
		t.Fatal("plugin type missing")
	}

	err = app.RegisterTaxIdentifierType(store.TaxIdentifierType{Code: "PL_NIP", Label: "Dup"})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate pl_nip = %v", err)
	}

	app.FreezeTaxIdentifierTypes()
	err = app.RegisterTaxIdentifierType(store.TaxIdentifierType{Code: "late_type", Label: "Late"})
	if err == nil || !strings.Contains(err.Error(), "frozen") {
		t.Fatalf("after freeze = %v", err)
	}
}

func TestApp_TaxIdentifierTypes_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	var app *plugin.App
	_ = app.TaxIdentifierTypes()
}

func codes(types []store.TaxIdentifierType) []string {
	out := make([]string, len(types))
	for i, tt := range types {
		out[i] = tt.Code
	}
	return out
}

func has(types []store.TaxIdentifierType, code string) bool {
	for _, tt := range types {
		if tt.Code == code {
			return true
		}
	}
	return false
}

func equal(a, b []string) bool {
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
