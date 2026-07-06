package plugin_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/application/hooks"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestApp_Hooks_Register(t *testing.T) {
	reg := hooks.NewRegistry(nil)
	app := &plugin.App{}
	app.SetHookRegistry(reg)

	if err := app.Hooks("acme.demo").Register(hooks.HookCartAddItemAfter, 100, func(ctx *hooks.Context) error {
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

func TestApp_SetHookRegistry_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil hook registry")
		}
	}()
	app := &plugin.App{}
	app.SetHookRegistry(nil)
}
