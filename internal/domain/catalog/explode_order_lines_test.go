package catalog_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestApplyExplodeOrderLines(t *testing.T) {
	t.Parallel()

	p, err := catalog.NewProduct("p1", "Kit", "kit")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	if p.ExplodeOrderLines {
		t.Fatal("default explode flag must be false")
	}
	p.Type = catalog.TypeBundle
	p.PricingMode = catalog.PricingModeDynamic
	p.StockMode = catalog.StockModeDynamic
	if err := p.ApplyExplodeOrderLines(true, true); err != nil {
		t.Fatalf("Apply true on dynamic bundle: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate dynamic explode: %v", err)
	}
	if !p.ExplodeOrderLines {
		t.Fatal("flag was not stored")
	}

	p.PricingMode = catalog.PricingModeFixed
	err = p.ApplyExplodeOrderLines(true, true)
	if err == nil || !strings.Contains(err.Error(), catalog.ExplodeOrderLinesRequiresDynamicMessage) {
		t.Fatalf("fixed price: %v", err)
	}
	p.PricingMode = catalog.PricingModeDynamic
	p.StockMode = catalog.StockModeFixed
	err = p.ApplyExplodeOrderLines(true, true)
	if err == nil || !strings.Contains(err.Error(), catalog.ExplodeOrderLinesRequiresDynamicMessage) {
		t.Fatalf("fixed stock: %v", err)
	}

	p.StockMode = catalog.StockModeFixed
	p.PricingMode = catalog.PricingModeDynamic
	p.Type = catalog.TypeBundle
	p.ExplodeOrderLines = true
	if err := p.ApplyExplodeOrderLines(false, false); err != nil {
		t.Fatalf("omit explode after stock mode change: %v", err)
	}
	if p.ExplodeOrderLines {
		t.Fatal("omitted explode must be false when stock mode is no longer dynamic")
	}
	p.StockMode = catalog.StockModeDynamic
	p.ExplodeOrderLines = true
	if err := p.ApplyExplodeOrderLines(false, false); err != nil {
		t.Fatalf("omit explode on still-dynamic bundle: %v", err)
	}
	if !p.ExplodeOrderLines {
		t.Fatal("omitted explode must stay true while both modes are dynamic")
	}

	p.ExplodeOrderLines = true
	p.Type = catalog.TypeSimple
	if err := p.ApplyPricingMode("", false); err != nil {
		t.Fatalf("omit pricing on type change: %v", err)
	}
	if err := p.ApplyStockMode("", false); err != nil {
		t.Fatalf("omit stock on type change: %v", err)
	}
	if err := p.ApplyExplodeOrderLines(false, false); err != nil {
		t.Fatalf("omit on type change: %v", err)
	}
	if p.ExplodeOrderLines {
		t.Fatal("type change off bundle must coerce the flag to false")
	}
	if p.PricingMode != catalog.PricingModeFixed || p.StockMode != catalog.StockModeFixed {
		t.Fatalf("modes = %s/%s, want fixed/fixed", p.PricingMode, p.StockMode)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate after coerce: %v", err)
	}

	p.Type = catalog.TypeSimple
	p.ExplodeOrderLines = true
	if err := p.Validate(); err == nil {
		t.Fatal("Validate must reject explode on a non-bundle")
	}
}
