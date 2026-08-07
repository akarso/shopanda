package legal_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/legal"
)

func TestEprEnabled_DefaultFalseWhenMissing(t *testing.T) {
	ok, err := legal.EprEnabled(context.Background(), stubConfigGetter{}, "store-1")
	if err != nil {
		t.Fatalf("EprEnabled: %v", err)
	}
	if ok {
		t.Fatal("expected default disabled")
	}
}

func TestStoreSchemeRegistrationID_StoreScope(t *testing.T) {
	repo := stubConfigGetter{
		legal.ScopedConfigKey("store-de", legal.EprSchemeRegistrationConfigKey): "DE-LUCID-123",
	}
	got, err := legal.StoreSchemeRegistrationID(context.Background(), repo, "store-de")
	if err != nil {
		t.Fatalf("StoreSchemeRegistrationID: %v", err)
	}
	if got != "DE-LUCID-123" {
		t.Fatalf("got %q", got)
	}
}

func TestParseEprPackaging_VariantWeightOverridesProduct(t *testing.T) {
	info := legal.ParseEprPackaging(
		map[string]interface{}{
			legal.AttrEprPackagingMaterial:  "plastic",
			legal.AttrEprPackagingWeightG:   100,
			legal.AttrEprRecyclable:         true,
			legal.AttrEprRecycledContentPct: 30,
		},
		map[string]interface{}{
			legal.AttrEprPackagingWeightG: 250,
		},
	).WithStoreSchemeID("PL-SCHEME-1")
	if info.WeightG != 250 {
		t.Fatalf("weight = %v, want 250", info.WeightG)
	}
	if info.MaterialLabel != "Plastic" {
		t.Fatalf("material label = %q", info.MaterialLabel)
	}
	if !info.Recyclable {
		t.Fatal("expected recyclable")
	}
	if info.SchemeRegistrationID != "PL-SCHEME-1" {
		t.Fatalf("scheme = %q", info.SchemeRegistrationID)
	}
	if !info.HasData() {
		t.Fatal("expected HasData")
	}
}

func TestParseEprPackaging_ProductSchemeOverridesStore(t *testing.T) {
	info := legal.ParseEprPackaging(
		map[string]interface{}{
			legal.AttrEprSchemeRegistrationID: "SKU-SCHEME-9",
		},
		nil,
	).WithStoreSchemeID("STORE-SCHEME")
	if info.SchemeRegistrationID != "SKU-SCHEME-9" {
		t.Fatalf("scheme = %q", info.SchemeRegistrationID)
	}
}

func TestEprPackaging_HasData_Empty(t *testing.T) {
	var empty legal.EprPackaging
	if empty.HasData() {
		t.Fatal("empty packaging should not have data")
	}
}
