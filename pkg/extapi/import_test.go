package extapi_test

import (
	"testing"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/pkg/extapi"
)

func TestStableImportEntitiesMatchInternal(t *testing.T) {
	internal := importctx.EntityCatalog()
	stable := extapi.ImportEntities()
	if len(stable) != len(internal) {
		t.Fatalf("stable len = %d, internal len = %d", len(stable), len(internal))
	}
	for i, name := range internal {
		if stable[i] != name {
			t.Fatalf("entity[%d] = %q, want %q", i, stable[i], name)
		}
	}
}

func TestStableImportRowHookCatalog(t *testing.T) {
	stable := extapi.ImportRowHookCatalog()
	internal := importctx.RowHookCatalog()
	if len(stable) != len(internal) {
		t.Fatalf("stable len = %d, internal len = %d", len(stable), len(internal))
	}
	for i, name := range internal {
		if stable[i] != name {
			t.Fatalf("hook[%d] = %q, want %q", i, stable[i], name)
		}
	}
}

func TestRowHookPoint(t *testing.T) {
	if got := extapi.RowHookPoint(extapi.ImportEntityProduct); got != "import.product.row" {
		t.Fatalf("RowHookPoint = %q", got)
	}
}
