package cart_test

import (
	"context"
	"testing"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/hooks"
	"github.com/akarso/shopanda/pkg/extapi"
)

func TestService_Validate_InvokesCartValidateHook(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	var hookRan bool
	if err := reg.Register(hooks.HookCartValidate, 100, "plugin.test", func(ctx *hooks.Context) error {
		hookRan = true
		if _, ok := ctx.Get("cart"); !ok {
			t.Fatal("expected cart in payload")
		}
		hooks.AppendValidationIssue(ctx, extapi.CartValidationIssue{
			Code:    "acme.min_qty",
			Message: "minimum quantity is 2",
			Level:   "warning",
		})
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	svc := newCartServiceWithHooks(reg)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	issues, err := svc.ValidationIssues(ctx, c)
	if err != nil {
		t.Fatalf("ValidationIssues: %v", err)
	}
	if !hookRan {
		t.Fatal("expected cart.validate hook to run")
	}
	if len(issues) != 1 || issues[0].Code != "acme.min_qty" {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestService_AddItem_BlockedByValidateHook(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	if err := reg.Register(hooks.HookCartValidate, 100, "plugin.test", func(ctx *hooks.Context) error {
		hooks.AppendValidationIssue(ctx, extapi.CartValidationIssue{
			Code:      "acme.blocked",
			Message:   "item not allowed",
			VariantID: "var-1",
		})
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	svc := newCartServiceWithHooks(reg)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err == nil {
		t.Fatal("expected add item to fail validation")
	} else if !cartApp.IsValidationFailed(err) {
		t.Fatalf("err = %v, want ValidationFailed", err)
	}

	updated, err := svc.GetCart(ctx, c.ID, "cust-1")
	if err != nil {
		t.Fatalf("GetCart: %v", err)
	}
	if len(updated.Items) != 0 {
		t.Fatalf("items = %d, want 0 when validation blocks", len(updated.Items))
	}
}

func TestService_AddItem_WarningDoesNotBlockMutation(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	if err := reg.Register(hooks.HookCartValidate, 100, "plugin.test", func(ctx *hooks.Context) error {
		hooks.AppendValidationIssue(ctx, extapi.CartValidationIssue{
			Code:    "acme.soft",
			Message: "consider adding another item",
			Level:   "warning",
		})
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	svc := newCartServiceWithHooks(reg)
	ctx := context.Background()
	c, err := svc.CreateCart(ctx, "cust-1", "EUR")
	if err != nil {
		t.Fatalf("CreateCart: %v", err)
	}
	if _, err := svc.AddItem(ctx, c.ID, "cust-1", "var-1", 1, cartApp.AddItemOptions{}); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	updated, err := svc.GetCart(ctx, c.ID, "cust-1")
	if err != nil {
		t.Fatalf("GetCart: %v", err)
	}
	if len(updated.Items) != 1 {
		t.Fatalf("items = %d, want 1 when only warnings", len(updated.Items))
	}
}
