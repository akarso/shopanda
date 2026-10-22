package catalog_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestProduct_Validate(t *testing.T) {
	t.Parallel()

	p, err := catalog.NewProduct("p1", "Widget", "widget")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate() default simple: %v", err)
	}

	for _, typ := range catalog.AllTypes() {
		p.Type = typ
		if err := p.Validate(); err != nil {
			t.Fatalf("Validate() type %q: %v", typ, err)
		}
	}

	p.Type = catalog.Type("kit")
	err = p.Validate()
	if err == nil {
		t.Fatal("Validate() unknown type: expected error")
	}
	if !strings.Contains(err.Error(), `"kit"`) {
		t.Errorf("Validate() error = %q, want rejected value quoted", err)
	}

	p.Type = ""
	err = p.Validate()
	if err == nil {
		t.Fatal("Validate() empty type: expected error")
	}

	p.Type = catalog.TypeSimple
	p.VisibilityModes.Catalog = catalog.VisibilityMode("forced")
	err = p.Validate()
	if err == nil {
		t.Fatal("Validate() invalid visibility mode: expected error")
	}
	if !strings.Contains(err.Error(), `"forced"`) {
		t.Errorf("Validate() error = %q, want rejected mode quoted", err)
	}

	var nilProduct *catalog.Product
	if err := nilProduct.Validate(); err == nil {
		t.Fatal("Validate() nil: expected error")
	}
}

func TestAllTypesMatchMigrationCheck(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	migrationPath := filepath.Join(filepath.Dir(thisFile), "../../../migrations/083_add_products_type.sql")
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	re := regexp.MustCompile(`(?s)CHECK\s*\(\s*type\s+IN\s*\((.*?)\)\s*\)`)
	m := re.FindSubmatch(content)
	if m == nil {
		t.Fatal("products_type_check IN (...) not found in 083 migration")
	}
	quoted := regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(string(m[1]), -1)
	if len(quoted) == 0 {
		t.Fatal("no quoted type values in CHECK")
	}

	fromSQL := make(map[string]struct{}, len(quoted))
	for _, q := range quoted {
		fromSQL[q[1]] = struct{}{}
	}
	fromDomain := make(map[string]struct{}, len(catalog.AllTypes()))
	for _, typ := range catalog.AllTypes() {
		fromDomain[string(typ)] = struct{}{}
	}

	if len(fromSQL) != len(fromDomain) {
		t.Fatalf("CHECK has %d values, AllTypes has %d", len(fromSQL), len(fromDomain))
	}
	for v := range fromDomain {
		if _, ok := fromSQL[v]; !ok {
			t.Errorf("AllTypes value %q missing from migration CHECK", v)
		}
	}
	for v := range fromSQL {
		if _, ok := fromDomain[v]; !ok {
			t.Errorf("migration CHECK value %q missing from AllTypes", v)
		}
	}
}

func TestPricingMode_ValidateAndApply(t *testing.T) {
	t.Parallel()

	p, err := catalog.NewProduct("p1", "Widget", "widget")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	if p.PricingMode != catalog.PricingModeFixed {
		t.Fatalf("default mode = %q, want fixed", p.PricingMode)
	}

	p.Type = catalog.TypeBundle
	if err := p.ApplyPricingMode("dynamic", true); err != nil {
		t.Fatalf("bundle dynamic: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("bundle dynamic validate: %v", err)
	}

	p.Type = catalog.TypeSimple
	if err := p.ApplyPricingMode("dynamic", true); err == nil || !strings.Contains(err.Error(), "bundle") {
		t.Fatalf("dynamic on simple = %v, want bundle-only rejection", err)
	}
	if p.PricingMode != catalog.PricingModeDynamic {
		t.Fatalf("rejected apply changed mode to %q", p.PricingMode)
	}

	p.Type = catalog.TypeBundle
	p.PricingMode = catalog.PricingModeDynamic
	p.Type = catalog.TypeGrouped
	if err := p.ApplyPricingMode("", false); err != nil {
		t.Fatalf("omit mode on grouped: %v", err)
	}
	if p.PricingMode != catalog.PricingModeFixed {
		t.Fatalf("grouped mode = %q, want fixed", p.PricingMode)
	}

	p.PricingMode = catalog.PricingModeDynamic
	p.Type = catalog.TypeSimple
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "bundle") {
		t.Fatalf("dynamic simple validate = %v, want bundle-only error", err)
	}

	if err := p.ApplyPricingMode("nope", true); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("invalid mode = %v", err)
	}
}

func TestStockMode_ValidateAndApply(t *testing.T) {
	t.Parallel()

	p, err := catalog.NewProduct("p1", "Widget", "widget")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}
	if p.StockMode != catalog.StockModeFixed {
		t.Fatalf("default mode = %q, want fixed", p.StockMode)
	}

	p.Type = catalog.TypeBundle
	if err := p.ApplyStockMode("dynamic", true); err != nil {
		t.Fatalf("bundle dynamic: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("bundle dynamic validate: %v", err)
	}

	p.Type = catalog.TypeSimple
	if err := p.ApplyStockMode("dynamic", true); err == nil || !strings.Contains(err.Error(), "bundle") {
		t.Fatalf("dynamic on simple = %v, want bundle-only rejection", err)
	}
	if p.StockMode != catalog.StockModeDynamic {
		t.Fatalf("rejected apply changed mode to %q", p.StockMode)
	}

	p.Type = catalog.TypeBundle
	p.StockMode = catalog.StockModeDynamic
	p.Type = catalog.TypeGrouped
	if err := p.ApplyStockMode("", false); err != nil {
		t.Fatalf("omit mode on grouped: %v", err)
	}
	if p.StockMode != catalog.StockModeFixed {
		t.Fatalf("grouped mode = %q, want fixed", p.StockMode)
	}

	p.StockMode = catalog.StockModeDynamic
	p.Type = catalog.TypeSimple
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "bundle") {
		t.Fatalf("dynamic simple validate = %v, want bundle-only error", err)
	}

	if err := p.ApplyStockMode("nope", true); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Fatalf("invalid mode = %v", err)
	}
}

func TestAllPricingModesMatchMigrationCheck(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	migrationPath := filepath.Join(filepath.Dir(thisFile), "../../../migrations/090_add_products_pricing_mode.sql")
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	re := regexp.MustCompile(`(?s)CHECK\s*\(\s*pricing_mode\s+IN\s*\((.*?)\)\s*\)`)
	m := re.FindSubmatch(content)
	if m == nil {
		t.Fatal("products_pricing_mode_check IN (...) not found")
	}
	quoted := regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(string(m[1]), -1)
	fromSQL := make(map[string]struct{}, len(quoted))
	for _, q := range quoted {
		fromSQL[q[1]] = struct{}{}
	}
	fromDomain := make(map[string]struct{}, len(catalog.AllPricingModes()))
	for _, mode := range catalog.AllPricingModes() {
		fromDomain[string(mode)] = struct{}{}
	}
	if len(fromSQL) != len(fromDomain) {
		t.Fatalf("CHECK has %d values, AllPricingModes has %d", len(fromSQL), len(fromDomain))
	}
	for v := range fromDomain {
		if _, ok := fromSQL[v]; !ok {
			t.Errorf("AllPricingModes value %q missing from migration CHECK", v)
		}
	}
}

func TestAllStockModesMatchMigrationCheck(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	migrationPath := filepath.Join(filepath.Dir(thisFile), "../../../migrations/092_add_products_stock_mode.sql")
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	re := regexp.MustCompile(`(?s)CHECK\s*\(\s*stock_mode\s+IN\s*\((.*?)\)\s*\)`)
	m := re.FindSubmatch(content)
	if m == nil {
		t.Fatal("products_stock_mode_check IN (...) not found")
	}
	quoted := regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(string(m[1]), -1)
	fromSQL := make(map[string]struct{}, len(quoted))
	for _, q := range quoted {
		fromSQL[q[1]] = struct{}{}
	}
	fromDomain := make(map[string]struct{}, len(catalog.AllStockModes()))
	for _, mode := range catalog.AllStockModes() {
		fromDomain[string(mode)] = struct{}{}
	}
	if len(fromSQL) != len(fromDomain) {
		t.Fatalf("CHECK has %d values, AllStockModes has %d", len(fromSQL), len(fromDomain))
	}
	for v := range fromDomain {
		if _, ok := fromSQL[v]; !ok {
			t.Errorf("AllStockModes value %q missing from migration CHECK", v)
		}
	}
}
