package legal_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/legal"
)

func TestGpsrEnabled_DefaultFalseWhenMissing(t *testing.T) {
	ok, err := legal.GpsrEnabled(context.Background(), stubConfigGetter{}, "store-1")
	if err != nil {
		t.Fatalf("GpsrEnabled: %v", err)
	}
	if ok {
		t.Fatal("expected default disabled")
	}
}

func TestParseGpsrFromProduct(t *testing.T) {
	info := legal.ParseGpsrFromProduct(map[string]interface{}{
		legal.AttrGpsrManufacturerName:    "Acme GmbH",
		legal.AttrGpsrManufacturerContact: "safety@acme.example",
		legal.AttrGpsrProductIdentifier:   "4012345678901",
		legal.AttrGpsrSafetyWarnings:      "Keep away from heat sources.",
		legal.AttrGpsrAgeRestriction:      "3_plus",
		legal.AttrGpsrConformityMark:      "ce",
	}).WithStoreManufacturer("Store Mfg", "store@example.com")

	if info.ManufacturerName != "Acme GmbH" {
		t.Fatalf("manufacturer = %q", info.ManufacturerName)
	}
	if info.AgeRestrictionLabel != "Not suitable for children under 3 years" {
		t.Fatalf("age label = %q", info.AgeRestrictionLabel)
	}
	if !info.HasDisclosure() {
		t.Fatal("expected disclosure")
	}
}

func TestGpsrInfo_WithStoreManufacturer(t *testing.T) {
	info := legal.ParseGpsrFromProduct(map[string]interface{}{
		legal.AttrGpsrSafetyWarnings: "Choking hazard.",
	}).WithStoreManufacturer("Default Mfg", "eu@default.example")
	if info.ManufacturerName != "Default Mfg" {
		t.Fatalf("manufacturer = %q", info.ManufacturerName)
	}
	if info.ManufacturerContact != "eu@default.example" {
		t.Fatalf("contact = %q", info.ManufacturerContact)
	}
}

func TestGpsrInfo_HasDisclosure_Empty(t *testing.T) {
	var empty legal.GpsrInfo
	if empty.HasDisclosure() {
		t.Fatal("empty gpsr info should not disclose")
	}
}
