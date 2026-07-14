package importer_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/application/importer"
)

func TestProductImport_RowHookRemapsColumn(t *testing.T) {
	csv := `name,slug,matnr
Widget,widget,SKU-001
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}

	reg := importctx.NewRegistry(nil)
	if err := reg.Register(importctx.EntityProduct, 100, "test/remap", func(ctx *importctx.RowContext) error {
		if ctx.Row["sku"] == "" && ctx.Row["matnr"] != "" {
			ctx.Row["sku"] = ctx.Row["matnr"]
		}
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	imp := importer.NewProductImporter(prodRepo, varRepo, nil).WithRowHooks(reg)
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 1 || result.Variants != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(varRepo.variants) != 1 || varRepo.variants[0].SKU != "SKU-001" {
		t.Fatalf("variants = %+v", varRepo.variants)
	}
}

func TestProductImport_RowHookErrorSkipsRow(t *testing.T) {
	csv := `name,slug,sku
Widget,widget,SKU-001
`
	reg := importctx.NewRegistry(nil)
	_ = reg.Register(importctx.EntityProduct, 100, "test/fail", func(ctx *importctx.RowContext) error {
		return errors.New("blocked")
	})

	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{}, nil).WithRowHooks(reg)
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 || len(result.Errors) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Products != 0 || result.Variants != 0 {
		t.Fatalf("expected no imports, got %+v", result)
	}
}

func TestProductImport_RowHookSkipRow(t *testing.T) {
	csv := `name,slug,sku
Widget,widget,SKU-001
Widget,widget,SKU-002
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}

	reg := importctx.NewRegistry(nil)
	_ = reg.Register(importctx.EntityProduct, 100, "test/skip", func(ctx *importctx.RowContext) error {
		if ctx.Row["sku"] == "SKU-001" {
			ctx.SkipRow()
		}
		return nil
	})

	imp := importer.NewProductImporter(prodRepo, varRepo, nil).WithRowHooks(reg)
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 || result.Products != 1 || result.Variants != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Errors) != 0 || len(result.RowErrors) != 0 {
		t.Fatalf("expected no errors, got errors=%v rowErrors=%v", result.Errors, result.RowErrors)
	}
}

func TestProductImport_RowHookAppendErrorFailsRow(t *testing.T) {
	csv := `name,slug,sku
Widget,widget,SKU-001
`
	reg := importctx.NewRegistry(nil)
	_ = reg.Register(importctx.EntityProduct, 100, "test/validate", func(ctx *importctx.RowContext) error {
		ctx.AppendError("erp.invalid_sku", "SKU not in ERP catalog")
		return nil
	})

	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{}, nil).WithRowHooks(reg)
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 || result.Products != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.RowErrors) != 1 || result.RowErrors[0].Code != "erp.invalid_sku" || result.RowErrors[0].RowIndex != 2 {
		t.Fatalf("rowErrors = %+v", result.RowErrors)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "erp.invalid_sku") {
		t.Fatalf("errors = %v", result.Errors)
	}
}
