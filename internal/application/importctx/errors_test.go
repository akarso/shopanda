package importctx_test

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/application/importctx"
)

func TestRowContext_AppendErrorAndSkip(t *testing.T) {
	ctx, err := importctx.NewRowContext(importctx.EntityProduct, 4, map[string]string{"sku": "1"})
	if err != nil {
		t.Fatalf("NewRowContext: %v", err)
	}
	ctx.AppendError("erp.invalid", "bad material")
	ctx.SkipRow()
	if !ctx.Skip {
		t.Fatal("expected skip")
	}
	if len(ctx.Errors) != 1 || ctx.Errors[0].Code != "erp.invalid" || ctx.Errors[0].RowIndex != 4 {
		t.Fatalf("errors = %+v", ctx.Errors)
	}
}

func TestRegistry_Invoke_AppendErrorFailsAfterChain(t *testing.T) {
	reg := importctx.NewRegistry(nil)
	var ranSecond bool
	_ = reg.Register(importctx.EntityProduct, 100, "first", func(ctx *importctx.RowContext) error {
		ctx.AppendError("erp.code", "validation failed")
		return nil
	})
	_ = reg.Register(importctx.EntityProduct, 200, "second", func(ctx *importctx.RowContext) error {
		ranSecond = true
		return nil
	})

	rowCtx, _ := importctx.NewRowContext(importctx.EntityProduct, 2, map[string]string{})
	if err := reg.Invoke(context.Background(), rowCtx); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !ranSecond {
		t.Fatal("second handler should run when first only appends errors")
	}
	if rowCtx.Skip || len(rowCtx.Errors) != 1 {
		t.Fatalf("ctx = skip=%v errors=%+v", rowCtx.Skip, rowCtx.Errors)
	}
}

func TestRegistry_Invoke_SkipAfterChain(t *testing.T) {
	reg := importctx.NewRegistry(nil)
	_ = reg.Register(importctx.EntityStock, 100, "skipper", func(ctx *importctx.RowContext) error {
		ctx.SkipRow()
		return nil
	})

	rowCtx, _ := importctx.NewRowContext(importctx.EntityStock, 5, map[string]string{"sku": "A"})
	if err := reg.Invoke(context.Background(), rowCtx); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !rowCtx.Skip {
		t.Fatal("expected skip")
	}
}
