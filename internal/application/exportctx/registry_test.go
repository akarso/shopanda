package exportctx_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/application/exportctx"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestRowHookName_SupportedEntities(t *testing.T) {
	cases := map[string]string{
		exportctx.EntityProduct:   "export.product.row",
		exportctx.EntityPrice:     "export.price.row",
		exportctx.EntityStock:     "export.stock.row",
		exportctx.EntityCategory:  "export.category.row",
		exportctx.EntityCustomer:  "export.customer.row",
		exportctx.EntityAttribute: "export.attribute.row",
	}
	for entity, want := range cases {
		got, err := exportctx.RowHookName(entity)
		if err != nil {
			t.Fatalf("RowHookName(%q): %v", entity, err)
		}
		if got != want {
			t.Fatalf("RowHookName(%q) = %q, want %q", entity, got, want)
		}
	}
}

func TestRegistry_RegisterAndInvoke_OrderAndMutation(t *testing.T) {
	reg := exportctx.NewRegistry(nil)
	var order []string
	if err := reg.Register(exportctx.EntityProduct, 200, "second", func(ctx *exportctx.RowContext) error {
		order = append(order, "second")
		ctx.Row["erp_sku"] = ctx.Row["sku"]
		return nil
	}); err != nil {
		t.Fatalf("Register second: %v", err)
	}
	if err := reg.Register(exportctx.EntityProduct, 100, "first", func(ctx *exportctx.RowContext) error {
		order = append(order, "first")
		return nil
	}); err != nil {
		t.Fatalf("Register first: %v", err)
	}

	rowCtx, err := exportctx.NewRowContext(exportctx.EntityProduct, 3, map[string]string{"sku": "SKU-1"})
	if err != nil {
		t.Fatalf("NewRowContext: %v", err)
	}
	if err := reg.Invoke(context.Background(), rowCtx); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("order = %v", order)
	}
	if rowCtx.Row["erp_sku"] != "SKU-1" {
		t.Fatalf("row = %v", rowCtx.Row)
	}
}

func TestRegistry_Invoke_HandlerErrorStopsChain(t *testing.T) {
	reg := exportctx.NewRegistry(nil)
	_ = reg.Register(exportctx.EntityProduct, 100, "fail", func(ctx *exportctx.RowContext) error {
		return errors.New("boom")
	})
	rowCtx, _ := exportctx.NewRowContext(exportctx.EntityProduct, 1, map[string]string{})
	if err := reg.Invoke(context.Background(), rowCtx); err == nil {
		t.Fatal("expected handler error")
	}
}

func TestRegistry_HandlerPanicRecovered(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := exportctx.NewRegistry(log)
	_ = reg.Register(exportctx.EntityProduct, 100, "panic", func(ctx *exportctx.RowContext) error {
		panic("boom")
	})
	rowCtx, _ := exportctx.NewRowContext(exportctx.EntityProduct, 1, map[string]string{})
	if err := reg.Invoke(context.Background(), rowCtx); err == nil {
		t.Fatal("expected panic error")
	}
}
