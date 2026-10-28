package pricing_test

import (
	"testing"

	appPricing "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/tax"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func TestEnsureTaxClasses_FillsFromProductType(t *testing.T) {
	meta := map[string]interface{}{}
	err := appPricing.EnsureTaxClasses(meta, []appPricing.LineTaxClass{
		{VariantID: "v-simple", ProductType: catalog.TypeSimple},
		{VariantID: "v-virtual", ProductType: catalog.TypeVirtual},
		{VariantID: "v-downloadable", ProductType: catalog.TypeDownloadable},
		{VariantID: "v-bundle", ProductType: catalog.TypeBundle},
	})
	if err != nil {
		t.Fatalf("EnsureTaxClasses: %v", err)
	}
	classes := meta["tax_classes"].(map[string]string)
	want := map[string]string{
		"v-simple":       tax.TaxClassStandard,
		"v-virtual":      tax.TaxClassStandard,
		"v-downloadable": tax.TaxClassDigital,
		"v-bundle":       tax.TaxClassStandard,
	}
	for vid, class := range want {
		if got := classes[vid]; got != class {
			t.Errorf("tax_classes[%q] = %q, want %q", vid, got, class)
		}
	}
}

func TestEnsureTaxClasses_PreservesExistingKeys(t *testing.T) {
	meta := map[string]interface{}{
		"tax_classes": map[string]string{"v1": tax.TaxClassReduced},
	}
	err := appPricing.EnsureTaxClasses(meta, []appPricing.LineTaxClass{
		{VariantID: "v1", ProductType: catalog.TypeSimple},
		{VariantID: "v2", ProductType: catalog.TypeDownloadable},
	})
	if err != nil {
		t.Fatalf("EnsureTaxClasses: %v", err)
	}
	classes := meta["tax_classes"].(map[string]string)
	if classes["v1"] != tax.TaxClassReduced {
		t.Errorf("v1 = %q, want reduced (preserved)", classes["v1"])
	}
	if classes["v2"] != tax.TaxClassDigital {
		t.Errorf("v2 = %q, want digital (filled)", classes["v2"])
	}
}

func TestEnsureTaxClasses_CoercesInterfaceMap(t *testing.T) {
	meta := map[string]interface{}{
		"tax_classes": map[string]interface{}{"v1": tax.TaxClassReduced},
	}
	err := appPricing.EnsureTaxClasses(meta, []appPricing.LineTaxClass{
		{VariantID: "v1", ProductType: catalog.TypeSimple},
		{VariantID: "v2", ProductType: catalog.TypeSimple},
	})
	if err != nil {
		t.Fatalf("EnsureTaxClasses: %v", err)
	}
	classes := meta["tax_classes"].(map[string]string)
	if classes["v1"] != tax.TaxClassReduced {
		t.Errorf("v1 = %q, want reduced", classes["v1"])
	}
	if classes["v2"] != tax.TaxClassStandard {
		t.Errorf("v2 = %q, want standard", classes["v2"])
	}
}

func TestEnsureTaxClasses_RejectsWrongTypedMap(t *testing.T) {
	meta := map[string]interface{}{
		"tax_classes": "not-a-map",
	}
	err := appPricing.EnsureTaxClasses(meta, nil)
	if err == nil {
		t.Fatal("expected error for wrong-typed tax_classes")
	}
	if !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("want validation error, got %v", err)
	}
}

func TestEnsureTaxClasses_RejectsNonStringInterfaceValue(t *testing.T) {
	meta := map[string]interface{}{
		"tax_classes": map[string]interface{}{"v1": 19},
	}
	err := appPricing.EnsureTaxClasses(meta, nil)
	if err == nil {
		t.Fatal("expected error for non-string tax_classes value")
	}
}

func TestEnsureTaxClasses_RejectsEmptyOverride(t *testing.T) {
	meta := map[string]interface{}{
		"tax_classes": map[string]string{"v1": ""},
	}
	err := appPricing.EnsureTaxClasses(meta, []appPricing.LineTaxClass{
		{VariantID: "v1", ProductType: catalog.TypeSimple},
	})
	if err == nil {
		t.Fatal("expected error for empty class override")
	}
}

func TestEnsureTaxClasses_RejectsUnknownClass(t *testing.T) {
	meta := map[string]interface{}{
		"tax_classes": map[string]string{"v1": "luxury"},
	}
	err := appPricing.EnsureTaxClasses(meta, []appPricing.LineTaxClass{
		{VariantID: "v1", ProductType: catalog.TypeSimple},
	})
	if err == nil {
		t.Fatal("expected error for unknown class")
	}
}

func TestTaxClassesFromMeta(t *testing.T) {
	t.Run("nil meta", func(t *testing.T) {
		if got := appPricing.TaxClassesFromMeta(nil); got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
	t.Run("absent key", func(t *testing.T) {
		if got := appPricing.TaxClassesFromMeta(map[string]interface{}{}); got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
	t.Run("wrong type", func(t *testing.T) {
		if got := appPricing.TaxClassesFromMeta(map[string]interface{}{"tax_classes": "x"}); got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
	t.Run("string map", func(t *testing.T) {
		meta := map[string]interface{}{
			"tax_classes": map[string]string{"v1": tax.TaxClassDigital},
		}
		got := appPricing.TaxClassesFromMeta(meta)
		if got == nil || got["v1"] != tax.TaxClassDigital {
			t.Fatalf("got %#v", got)
		}
	})
	t.Run("interface map coerced", func(t *testing.T) {
		meta := map[string]interface{}{
			"tax_classes": map[string]interface{}{"v1": tax.TaxClassReduced},
		}
		got := appPricing.TaxClassesFromMeta(meta)
		if got == nil || got["v1"] != tax.TaxClassReduced {
			t.Fatalf("got %#v", got)
		}
	})
}

func TestCatalogTaxClassCode_BridgesTaxDomainConstants(t *testing.T) {
	// Application-layer bridge: catalog must not import tax; constants must match.
	if got := catalog.TaxClassCode(catalog.TypeDownloadable); got != tax.TaxClassDigital {
		t.Errorf("TaxClassCode(downloadable) = %q, want %q", got, tax.TaxClassDigital)
	}
	if got := catalog.TaxClassCode(catalog.TypeVirtual); got != tax.TaxClassStandard {
		t.Errorf("TaxClassCode(virtual) = %q, want %q", got, tax.TaxClassStandard)
	}
	if got := catalog.TaxClassCode(catalog.TypeBundle); got != tax.TaxClassStandard {
		t.Errorf("TaxClassCode(bundle) = %q, want %q (downloadable-only kit still parent standard)", got, tax.TaxClassStandard)
	}
}
