package catalog_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestAttributeRegistry_PropertyFilters(t *testing.T) {
	reg := catalog.NewAttributeRegistry()
	reg.RegisterAttribute(catalog.Attribute{Code: "color", Label: "Color", Type: catalog.AttributeTypeSelect, UseInAdvancedSearch: true, UseInLayeredNav: true})
	reg.RegisterAttribute(catalog.Attribute{Code: "weight", Label: "Weight", Type: catalog.AttributeTypeNumber, UseInPromoRules: true})
	reg.RegisterAttribute(catalog.Attribute{Code: "note", Label: "Note", Type: catalog.AttributeTypeText})

	if got := reg.AttributesForAdvancedSearch(); len(got) != 1 || got[0].Code != "color" {
		t.Fatalf("AttributesForAdvancedSearch = %+v", got)
	}
	if got := reg.AttributesForLayeredNav(); len(got) != 1 || got[0].Code != "color" {
		t.Fatalf("AttributesForLayeredNav = %+v", got)
	}
	if got := reg.AttributesForPromoRules(); len(got) != 1 || got[0].Code != "weight" {
		t.Fatalf("AttributesForPromoRules = %+v", got)
	}
}
