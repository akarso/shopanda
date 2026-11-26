package pluginsdk_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

func TestHost_RegisterTaxIdentifierType(t *testing.T) {
	app := &plugin.App{}
	h := pluginsdk.AsHostFor(app, "acme")

	err := h.RegisterTaxIdentifierType(extapi.TaxIdentifierType{
		Code: "acme_id", Label: "Acme ID", Countries: []string{"PL"}, Region: "EU",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := app.TaxIdentifierTypes().Get("acme_id"); !ok {
		t.Fatal("missing acme_id")
	}

	err = h.RegisterTaxIdentifierType(extapi.TaxIdentifierType{
		Code: "bad code", Label: "X",
	})
	if err == nil {
		t.Fatal("expected bad code")
	}

	err = h.RegisterTaxIdentifierType(extapi.TaxIdentifierType{
		Code: "ok_type", Label: "X", Region: "APAC",
	})
	if err == nil || !strings.Contains(err.Error(), "region") {
		t.Fatalf("bad region = %v", err)
	}

	err = h.RegisterTaxIdentifierType(extapi.TaxIdentifierType{
		Code: "PL_NIP", Label: "Dup NIP", Countries: []string{"PL"},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate pl_nip = %v", err)
	}

	err = h.RegisterTaxIdentifierType(extapi.TaxIdentifierType{
		Code: "eu_wide", Label: "EU Wide", Countries: extapi.EUMemberCountries(), Region: "EU",
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := app.TaxIdentifierTypes()
	if _, ok := reg.Get("eu_wide"); !ok {
		t.Fatal("eu_wide missing")
	}
	if !hasTaxCode(reg.ApplicableTo("FR"), "eu_wide") {
		t.Fatal("eu_wide must apply in FR")
	}
	if hasTaxCode(reg.ApplicableTo("US"), "eu_wide") {
		t.Fatal("eu_wide must not apply in US")
	}
}

func hasTaxCode(types []store.TaxIdentifierType, code string) bool {
	for _, tt := range types {
		if tt.Code == code {
			return true
		}
	}
	return false
}
