package catalog_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/id"
)

func TestNewVariant_OK(t *testing.T) {
	vid := id.New()
	pid := id.New()
	v, err := catalog.NewVariant(vid, pid, "SKU-001")
	if err != nil {
		t.Fatalf("NewVariant: %v", err)
	}
	if v.ID != vid {
		t.Errorf("ID = %q, want %q", v.ID, vid)
	}
	if v.ProductID != pid {
		t.Errorf("ProductID = %q, want %q", v.ProductID, pid)
	}
	if v.SKU != "SKU-001" {
		t.Errorf("SKU = %q, want SKU-001", v.SKU)
	}
	if v.Attributes == nil {
		t.Error("Attributes should be initialised")
	}
	if v.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestNewVariant_Validation(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		productID string
		sku       string
	}{
		{"empty id", "", id.New(), "SKU-001"},
		{"empty product_id", id.New(), "", "SKU-001"},
		{"empty sku", id.New(), id.New(), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := catalog.NewVariant(tc.id, tc.productID, tc.sku)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestVariantEventConstants(t *testing.T) {
	if catalog.EventVariantCreated != "catalog.variant.created" {
		t.Errorf("EventVariantCreated = %q", catalog.EventVariantCreated)
	}
	if catalog.EventVariantUpdated != "catalog.variant.updated" {
		t.Errorf("EventVariantUpdated = %q", catalog.EventVariantUpdated)
	}
}
