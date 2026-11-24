package plugin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/application/hooks"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

func TestApp_Hooks_Register(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	app := &plugin.App{}
	app.SetHookRegistry(reg)

	if err := app.Hooks("acme.demo").Register(extapi.HookCartAddItemAfter, 100, func(ctx *extapi.HookContext) error {
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	catalog := app.HookRegistry().Catalog()
	if len(catalog) != 1 || len(catalog[0].Handlers) != 1 {
		t.Fatalf("catalog = %+v", catalog)
	}
	if catalog[0].Handlers[0].Registrant != "acme.demo" {
		t.Fatalf("registrant = %q", catalog[0].Handlers[0].Registrant)
	}
}

func TestApp_Hooks_PayloadSyncValidationIssues(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	app := &plugin.App{}
	app.SetHookRegistry(reg)

	if err := app.Hooks("acme.demo").Register(extapi.HookCartValidate, 100, func(ctx *extapi.HookContext) error {
		ctx.AppendValidationError(extapi.CartValidationIssue{
			Code:    "acme.rule",
			Message: "blocked",
		})
		return nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	hookCtx := hooks.NewContext(hooks.HookCartValidate)
	issues := []extapi.CartValidationIssue{}
	hookCtx.Set("validation_errors", &issues)
	if err := reg.Invoke(nil, hookCtx); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	got := hooks.ValidationIssuesFromContext(hookCtx)
	if len(got) != 1 || got[0].Code != "acme.rule" {
		t.Fatalf("issues = %+v", got)
	}
}

func TestApp_Hooks_OrderLineDisplay_SealsOrderIDAndDropsFailedAppends(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	app := &plugin.App{}
	app.SetHookRegistry(reg)

	if err := app.Hooks("attacker").Register(extapi.HookOrderLineDisplay, 50, func(ctx *extapi.HookContext) error {
		ctx.Set("order_id", "other-order")
		ctx.AppendOrderLineDisplayField(extapi.OrderLineDisplayField{
			Code: "plugin.partial", Kind: extapi.DisplayKindText, Value: "drop-me",
		})
		return errors.New("fail after append")
	}); err != nil {
		t.Fatalf("Register attacker: %v", err)
	}
	var seen string
	if err := app.Hooks("metaldogs").Register(extapi.HookOrderLineDisplay, 100, func(ctx *extapi.HookContext) error {
		seen = ctx.OrderLineDisplayOrderID()
		ctx.AppendOrderLineDisplayField(extapi.OrderLineDisplayField{
			Code: "plugin.ok", Kind: extapi.DisplayKindText, Value: "ok",
		})
		return nil
	}); err != nil {
		t.Fatalf("Register metaldogs: %v", err)
	}

	hookCtx := hooks.NewContext(hooks.HookOrderLineDisplay)
	hookCtx.Set("order_id", "ord-real")
	hookCtx.Set("variant_id", "var-1")
	fields := []extapi.OrderLineDisplayField{}
	hookCtx.Set("fields", &fields)
	var errs int
	reg.InvokeContinueOnError(context.Background(), hookCtx, func(string, error) { errs++ })
	if errs != 1 {
		t.Fatalf("errs = %d", errs)
	}
	if seen != "ord-real" {
		t.Fatalf("second plugin saw order_id=%q", seen)
	}
	got := hooks.OrderLineDisplayFieldsFromContext(hookCtx)
	if len(got) != 1 || got[0].Code != "plugin.ok" {
		t.Fatalf("fields = %+v, want only plugin.ok", got)
	}
}

func TestApp_Hooks_OrderLineDisplay_DeepCopiesExtensionsPerHandler(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	app := &plugin.App{}
	app.SetHookRegistry(reg)

	if err := app.Hooks("mutator").Register(extapi.HookOrderLineDisplay, 50, func(ctx *extapi.HookContext) error {
		ext := ctx.OrderLineDisplayExtensions()
		ext["design_id"] = "tampered"
		return nil
	}); err != nil {
		t.Fatalf("Register mutator: %v", err)
	}
	var seen interface{}
	if err := app.Hooks("reader").Register(extapi.HookOrderLineDisplay, 100, func(ctx *extapi.HookContext) error {
		seen = ctx.OrderLineDisplayExtensions()["design_id"]
		return nil
	}); err != nil {
		t.Fatalf("Register reader: %v", err)
	}

	hookCtx := hooks.NewContext(hooks.HookOrderLineDisplay)
	hookCtx.Set("extensions", map[string]interface{}{"design_id": "d1"})
	fields := []extapi.OrderLineDisplayField{}
	hookCtx.Set("fields", &fields)
	reg.InvokeContinueOnError(context.Background(), hookCtx, nil)
	if seen != "d1" {
		t.Fatalf("reader saw design_id=%v after successful mutator", seen)
	}

	// Failed mutator must also leave the next handler on the original value.
	reg2 := hooks.NewRegistry(nil)
	app2 := &plugin.App{}
	app2.SetHookRegistry(reg2)
	if err := app2.Hooks("mutator").Register(extapi.HookOrderLineDisplay, 50, func(ctx *extapi.HookContext) error {
		ctx.OrderLineDisplayExtensions()["design_id"] = "tampered"
		return errors.New("fail")
	}); err != nil {
		t.Fatalf("Register mutator fail: %v", err)
	}
	var seenAfterFail interface{}
	if err := app2.Hooks("reader").Register(extapi.HookOrderLineDisplay, 100, func(ctx *extapi.HookContext) error {
		seenAfterFail = ctx.OrderLineDisplayExtensions()["design_id"]
		return nil
	}); err != nil {
		t.Fatalf("Register reader fail path: %v", err)
	}
	hookCtx2 := hooks.NewContext(hooks.HookOrderLineDisplay)
	hookCtx2.Set("extensions", map[string]interface{}{"design_id": "d1"})
	fields2 := []extapi.OrderLineDisplayField{}
	hookCtx2.Set("fields", &fields2)
	reg2.InvokeContinueOnError(context.Background(), hookCtx2, func(string, error) {})
	if seenAfterFail != "d1" {
		t.Fatalf("reader saw design_id=%v after failed mutator", seenAfterFail)
	}
}

func TestApp_SetHookRegistry_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil hook registry")
		}
	}()
	app := &plugin.App{}
	app.SetHookRegistry(nil)
}
