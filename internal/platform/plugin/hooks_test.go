package plugin_test

import (
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

func TestApp_SetHookRegistry_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil hook registry")
		}
	}()
	app := &plugin.App{}
	app.SetHookRegistry(nil)
}
