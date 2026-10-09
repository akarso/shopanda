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
