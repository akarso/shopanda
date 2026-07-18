package extapi_test

import (
	"testing"

	exportctx "github.com/akarso/shopanda/internal/application/exportctx"
	"github.com/akarso/shopanda/pkg/extapi"
)

func TestStableExportEntitiesMatchInternal(t *testing.T) {
	internal := exportctx.EntityCatalog()
	stable := extapi.ExportEntities()
	if len(stable) != len(internal) {
		t.Fatalf("stable len = %d, internal len = %d", len(stable), len(internal))
	}
	for i, name := range internal {
		if stable[i] != name {
			t.Fatalf("entity[%d] = %q, want %q", i, stable[i], name)
		}
	}
}

func TestStableExportRowHookCatalog(t *testing.T) {
	stable := extapi.ExportRowHookCatalog()
	internal := exportctx.RowHookCatalog()
	if len(stable) != len(internal) {
		t.Fatalf("stable len = %d, internal len = %d", len(stable), len(internal))
	}
	for i, name := range internal {
		if stable[i] != name {
			t.Fatalf("hook[%d] = %q, want %q", i, stable[i], name)
		}
	}
}

func TestExportRowHookPoint(t *testing.T) {
	if got := extapi.ExportRowHookPoint(extapi.ExportEntityProduct); got != "export.product.row" {
		t.Fatalf("ExportRowHookPoint = %q", got)
	}
}
