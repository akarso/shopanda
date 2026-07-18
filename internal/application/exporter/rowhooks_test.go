package exporter_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	exportctx "github.com/akarso/shopanda/internal/application/exportctx"
	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

func TestProductExport_RowHookRemapsColumn(t *testing.T) {
	prodRepo := &mockProductRepo{products: []catalog.Product{{ID: "p1", Name: "Widget", Slug: "widget", Description: "Desc"}}}
	varRepo := &mockVariantRepo{variants: map[string][]catalog.Variant{
		"p1": {{ID: "v1", ProductID: "p1", SKU: "SKU-001", Name: "Default"}},
	}}

	reg := exportctx.NewRegistry(nil)
	if err := reg.Register(exportctx.EntityProduct, 100, "test/remap", func(ctx *exportctx.RowContext) error {
		if ctx.Row["matnr"] == "" && ctx.Row["sku"] != "" {
			ctx.Row["matnr"] = ctx.Row["sku"]
		}
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	exp := exporter.NewProductExporter(prodRepo, varRepo).WithRowHooks(reg)
	var buf strings.Builder
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Products != 1 || result.Variants != 1 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(buf.String(), "matnr") || !strings.Contains(buf.String(), "SKU-001") {
		t.Fatalf("csv = %q", buf.String())
	}
}

func TestProductExport_RowHookErrorSkipsRow(t *testing.T) {
	prodRepo := &mockProductRepo{products: []catalog.Product{{ID: "p1", Name: "Widget", Slug: "widget"}}}
	varRepo := &mockVariantRepo{variants: map[string][]catalog.Variant{
		"p1": {{ID: "v1", ProductID: "p1", SKU: "SKU-001", Name: "Default"}},
	}}

	reg := exportctx.NewRegistry(nil)
	_ = reg.Register(exportctx.EntityProduct, 100, "test/fail", func(ctx *exportctx.RowContext) error {
		return errors.New("blocked")
	})

	exp := exporter.NewProductExporter(prodRepo, varRepo).WithRowHooks(reg)
	var buf strings.Builder
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Skipped != 1 || len(result.Errors) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Variants != 0 {
		t.Fatalf("expected no rows exported")
	}
}
