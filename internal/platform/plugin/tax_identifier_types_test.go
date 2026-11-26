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
	if !hasType(reg.ApplicableTo("PL"), "pl_nip") {
		t.Fatal("core catalog should be seeded")
	}

	custom, err := store.NewTaxIdentifierType("acme_vat", "Acme VAT", []string{"PL"}, store.TaxIdentifierRegionEU, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterTaxIdentifierType(custom); err != nil {
		t.Fatal(err)
	}
	if !hasType(app.TaxIdentifierTypes().ApplicableTo("PL"), "acme_vat") {
		t.Fatal("plugin type missing")
	}
	err = app.RegisterTaxIdentifierType(custom)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate = %v", err)
	}
}

func hasType(types []store.TaxIdentifierType, code string) bool {
	for _, tt := range types {
		if tt.Code == code {
			return true
		}
	}
	return false
}
