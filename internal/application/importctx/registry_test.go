package importctx_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestRowHookName_SupportedEntities(t *testing.T) {
	cases := map[string]string{
		importctx.EntityProduct:   "import.product.row",
		importctx.EntityPrice:     "import.price.row",
		importctx.EntityStock:     "import.stock.row",
		importctx.EntityCategory:  "import.category.row",
		importctx.EntityCustomer:  "import.customer.row",
		importctx.EntityAttribute: "import.attribute.row",
	}
	for entity, want := range cases {
		got, err := importctx.RowHookName(entity)
		if err != nil {
			t.Fatalf("RowHookName(%q): %v", entity, err)
		}
		if got != want {
			t.Fatalf("RowHookName(%q) = %q, want %q", entity, got, want)
		}
	}
}

func TestRowHookName_UnsupportedEntity(t *testing.T) {
	if _, err := importctx.RowHookName("erp"); err == nil {
		t.Fatal("expected error for unsupported entity")
	}
}

func TestNewRowContext_CopiesRow(t *testing.T) {
	row := map[string]string{"MATNR": "100", "slug": "tee"}
	ctx, err := importctx.NewRowContext(importctx.EntityProduct, 2, row)
	if err != nil {
		t.Fatalf("NewRowContext: %v", err)
	}
	row["MATNR"] = "changed"
	if ctx.Row["MATNR"] != "100" {
		t.Fatalf("row copy not isolated: %v", ctx.Row)
	}
}

func TestRegistry_RegisterAndInvoke_OrderAndMutation(t *testing.T) {
	reg := importctx.NewRegistry(nil)
	var order []string
	if err := reg.Register(importctx.EntityProduct, 200, "second", func(ctx *importctx.RowContext) error {
		order = append(order, "second")
		ctx.Row["slug"] = "mapped-slug"
		return nil
	}); err != nil {
		t.Fatalf("Register second: %v", err)
	}
	if err := reg.Register(importctx.EntityProduct, 100, "first", func(ctx *importctx.RowContext) error {
		order = append(order, "first")
		ctx.SetMeta("seen", true)
		return nil
	}); err != nil {
		t.Fatalf("Register first: %v", err)
	}

	rowCtx, err := importctx.NewRowContext(importctx.EntityProduct, 3, map[string]string{"MATNR": "100"})
	if err != nil {
		t.Fatalf("NewRowContext: %v", err)
	}
	if err := reg.Invoke(context.Background(), rowCtx); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("order = %v", order)
	}
	if rowCtx.Row["slug"] != "mapped-slug" {
		t.Fatalf("row = %v", rowCtx.Row)
	}
	seen, ok := rowCtx.GetMeta("seen")
	if !ok || seen != true {
		t.Fatalf("meta seen = %v, ok=%v", seen, ok)
	}
}

func TestRegistry_Invoke_HandlerErrorStopsChain(t *testing.T) {
	reg := importctx.NewRegistry(nil)
	_ = reg.Register(importctx.EntityProduct, 100, "fail", func(ctx *importctx.RowContext) error {
		return errors.New("boom")
	})
	var ran bool
	_ = reg.Register(importctx.EntityProduct, 200, "skip", func(ctx *importctx.RowContext) error {
		ran = true
		return nil
	})

	rowCtx, _ := importctx.NewRowContext(importctx.EntityProduct, 1, map[string]string{})
	if err := reg.Invoke(context.Background(), rowCtx); err == nil {
		t.Fatal("expected handler error")
	}
	if ran {
		t.Fatal("second handler should not run")
	}
}

func TestRegistry_Catalog(t *testing.T) {
	reg := importctx.NewRegistry(nil)
	_ = reg.Register(importctx.EntityProduct, 100, "acme/demo", func(ctx *importctx.RowContext) error { return nil })
	_ = reg.Register(importctx.EntityPrice, 50, "acme/demo", func(ctx *importctx.RowContext) error { return nil })

	catalog := reg.Catalog()
	if len(catalog) != 2 {
		t.Fatalf("catalog len = %d", len(catalog))
	}
	if catalog[0].Entity != importctx.EntityPrice || catalog[0].Hook != "import.price.row" {
		t.Fatalf("catalog[0] = %+v", catalog[0])
	}
}

func TestRegistry_HandlerPanicRecovered(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := importctx.NewRegistry(log)
	_ = reg.Register(importctx.EntityProduct, 100, "panic", func(ctx *importctx.RowContext) error {
		panic("boom")
	})
	rowCtx, _ := importctx.NewRowContext(importctx.EntityProduct, 1, map[string]string{})
	if err := reg.Invoke(context.Background(), rowCtx); err == nil {
		t.Fatal("expected panic error")
	}
}

func TestRowHookCatalog_MatchesEntities(t *testing.T) {
	got := importctx.RowHookCatalog()
	want := []string{
		"import.product.row",
		"import.price.row",
		"import.stock.row",
		"import.category.row",
		"import.customer.row",
		"import.attribute.row",
	}
	if len(got) != len(want) {
		t.Fatalf("catalog len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hook[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
