package catalog_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestNewBundleComponent_DefaultsRequired(t *testing.T) {
	c, err := catalog.NewBundleComponent(" bundle-1 ", " comp-1 ", 2)
	if err != nil {
		t.Fatalf("NewBundleComponent: %v", err)
	}
	if c.BundleProductID != "bundle-1" || c.ComponentProductID != "comp-1" {
		t.Errorf("ids not trimmed: %+v", c)
	}
	if !c.Required {
		t.Error("Required = false, want true")
	}
}

func TestBundleComponent_Validate(t *testing.T) {
	valid := &catalog.BundleComponent{
		BundleProductID:    "bundle-1",
		ComponentProductID: "comp-1",
		Quantity:           2,
		Required:           true,
	}

	tests := []struct {
		name    string
		c       *catalog.BundleComponent
		wantErr string
	}{
		{"valid", valid, ""},
		{"nil", nil, "must not be nil"},
		{"empty bundle id", &catalog.BundleComponent{ComponentProductID: "c", Quantity: 1}, "bundle product id"},
		{"empty component id", &catalog.BundleComponent{BundleProductID: "b", Quantity: 1}, "component product id"},
		{"quantity zero", &catalog.BundleComponent{BundleProductID: "b", ComponentProductID: "c", Quantity: 0}, "quantity"},
		{"quantity too large", &catalog.BundleComponent{BundleProductID: "b", ComponentProductID: "c", Quantity: 10000}, "9999"},
		{"self reference", &catalog.BundleComponent{BundleProductID: "same", ComponentProductID: "same", Quantity: 1}, "itself"},
		{"whitespace bundle id", &catalog.BundleComponent{BundleProductID: "  ", ComponentProductID: "c", Quantity: 1}, "bundle product id"},
		{"padded ids are not trimmed", &catalog.BundleComponent{BundleProductID: "  b  ", ComponentProductID: "  c  ", Quantity: 1}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var beforeBundle, beforeComponent string
			if tc.c != nil {
				beforeBundle, beforeComponent = tc.c.BundleProductID, tc.c.ComponentProductID
			}
			err := tc.c.Validate()
			if tc.c != nil && (tc.c.BundleProductID != beforeBundle || tc.c.ComponentProductID != beforeComponent) {
				t.Fatalf("Validate mutated ids: %+v", tc.c)
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestBundleComponent_ValidateComposition(t *testing.T) {
	c := &catalog.BundleComponent{
		BundleProductID:    "bundle-1",
		ComponentProductID: "comp-1",
		Quantity:           1,
	}

	for _, leaf := range []catalog.Type{catalog.TypeSimple, catalog.TypeVirtual, catalog.TypeDownloadable} {
		if err := c.ValidateComposition(catalog.TypeBundle, leaf); err != nil {
			t.Fatalf("ValidateComposition(bundle, %s) = %v", leaf, err)
		}
	}

	if err := c.ValidateComposition(catalog.TypeSimple, catalog.TypeSimple); err == nil {
		t.Fatal("expected error when parent is not bundle")
	}

	for _, bad := range []catalog.Type{catalog.TypeBundle, catalog.TypeGrouped, catalog.TypeConfigurable} {
		if err := c.ValidateComposition(catalog.TypeBundle, bad); err == nil {
			t.Fatalf("expected error when component is %s", bad)
		}
	}
}

func TestType_IsBundleComponentLeafType(t *testing.T) {
	if !catalog.TypeSimple.IsBundleComponentLeafType() {
		t.Error("simple should be leaf")
	}
	if catalog.TypeBundle.IsBundleComponentLeafType() {
		t.Error("bundle should not be leaf")
	}
}
