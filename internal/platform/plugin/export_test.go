package plugin_test

import (
	"context"
	"io"
	"testing"

	exportctx "github.com/akarso/shopanda/internal/application/exportctx"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

func TestApp_Export_RegisterRowHook(t *testing.T) {
	reg := exportctx.NewRegistry(nil)
	app := &plugin.App{Logger: logger.NewWithWriter(io.Discard, "error")}
	app.SetExportRegistry(reg)

	if err := app.Export("acme.demo").RegisterRowHook(extapi.ExportEntityProduct, 100, func(ctx *extapi.ExportRowContext) error {
		ctx.Row["erp_sku"] = ctx.Row["sku"]
		return nil
	}); err != nil {
		t.Fatalf("RegisterRowHook: %v", err)
	}

	rowCtx, err := exportctx.NewRowContext(exportctx.EntityProduct, 2, map[string]string{"sku": "SKU-1"})
	if err != nil {
		t.Fatalf("NewRowContext: %v", err)
	}
	if err := reg.Invoke(context.Background(), rowCtx); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if rowCtx.Row["erp_sku"] != "SKU-1" {
		t.Fatalf("row = %v", rowCtx.Row)
	}
}
