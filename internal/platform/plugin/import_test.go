package plugin_test

import (
	"context"
	"io"
	"testing"

	importctx "github.com/akarso/shopanda/internal/application/importctx"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

type stubImportPlugin struct{}

func (p *stubImportPlugin) Name() string { return "stub/import" }

func (p *stubImportPlugin) Init(app *plugin.App) error {
	return app.Import("stub/import").RegisterRowHook(extapi.ImportEntityProduct, 100, func(ctx *extapi.ImportRowContext) error {
		ctx.Row["slug"] = "stub-slug"
		return nil
	})
}

func TestApp_Import_RegisterRowHook(t *testing.T) {
	reg := importctx.NewRegistry(nil)
	app := &plugin.App{Logger: logger.NewWithWriter(io.Discard, "error")}
	app.SetImportRegistry(reg)

	if err := app.Import("acme.demo").RegisterRowHook(extapi.ImportEntityProduct, 100, func(ctx *extapi.ImportRowContext) error {
		ctx.Row["slug"] = "from-plugin"
		ctx.SetMeta("mapped", true)
		return nil
	}); err != nil {
		t.Fatalf("RegisterRowHook: %v", err)
	}

	rowCtx, err := importctx.NewRowContext(importctx.EntityProduct, 2, map[string]string{"MATNR": "100"})
	if err != nil {
		t.Fatalf("NewRowContext: %v", err)
	}
	if err := reg.Invoke(context.Background(), rowCtx); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if rowCtx.Row["slug"] != "from-plugin" {
		t.Fatalf("row = %v", rowCtx.Row)
	}
	seen, ok := rowCtx.GetMeta("mapped")
	if !ok || seen != true {
		t.Fatalf("meta = %v", rowCtx.Meta)
	}

	catalog := reg.Catalog()
	if len(catalog) != 1 || catalog[0].Handlers[0].Registrant != "acme.demo" {
		t.Fatalf("catalog = %+v", catalog)
	}
}

func TestApp_SetImportRegistry_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil import registry")
		}
	}()
	app := &plugin.App{}
	app.SetImportRegistry(nil)
}

func TestRegistry_StubPluginRegistersImportRowHook(t *testing.T) {
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(&stubImportPlugin{})

	app := &plugin.App{
		Logger: logger.NewWithWriter(io.Discard, "error"),
		Bus:    event.NewBus(logger.NewWithWriter(io.Discard, "error")),
		Config: &config.Config{},
	}
	app.SetImportRegistry(importctx.NewRegistry(app.Logger))
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("summary = %+v", summary)
	}

	rowCtx, err := importctx.NewRowContext(importctx.EntityProduct, 1, map[string]string{})
	if err != nil {
		t.Fatalf("NewRowContext: %v", err)
	}
	if err := app.ImportRegistry().Invoke(context.Background(), rowCtx); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if rowCtx.Row["slug"] != "stub-slug" {
		t.Fatalf("row = %v", rowCtx.Row)
	}
}
