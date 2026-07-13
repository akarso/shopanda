package plugin_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type stubSearchProvider struct {
	name string
}

func (s *stubSearchProvider) Name() string { return s.name }

type stubSearchPlugin struct {
	name     string
	provider any
	initErr  error
}

func (p *stubSearchPlugin) Name() string {
	if p.name != "" {
		return p.name
	}
	return "stub/search"
}

func (p *stubSearchPlugin) Init(app *plugin.App) error {
	if p.initErr != nil {
		return p.initErr
	}
	app.RegisterSearchProvider(p.provider)
	return nil
}

func TestApp_RegisterSearchProvider(t *testing.T) {
	app := &plugin.App{}
	provider := &stubSearchProvider{name: "stub"}
	app.RegisterSearchProvider(provider)

	got, ok := app.SearchProvider()
	if !ok {
		t.Fatal("SearchProvider() ok = false, want true")
	}
	if got != provider {
		t.Fatalf("SearchProvider() = %v, want %v", got, provider)
	}
}

func TestApp_RegisterSearchProvider_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil search provider")
		}
	}()
	app := &plugin.App{}
	app.RegisterSearchProvider(nil)
}

func TestApp_RegisterSearchProvider_DoubleRegistrationPanics(t *testing.T) {
	app := &plugin.App{}
	app.RegisterSearchProvider(&stubSearchProvider{name: "first"})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for double search provider registration")
		}
	}()
	app.RegisterSearchProvider(&stubSearchProvider{name: "second"})
}

type stubTaxCalculator struct{}

func (stubTaxCalculator) Calculate(_ context.Context, _ *pricing.PricingContext) error {
	return nil
}

func TestApp_RegisterTaxCalculator(t *testing.T) {
	app := &plugin.App{}
	calc := stubTaxCalculator{}
	app.RegisterTaxCalculator(calc)

	got, ok := app.TaxCalculator()
	if !ok {
		t.Fatal("TaxCalculator() ok = false, want true")
	}
	if got != calc {
		t.Fatalf("TaxCalculator() = %v, want %v", got, calc)
	}
}

func TestApp_RegisterTaxCalculator_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil tax calculator")
		}
	}()
	app := &plugin.App{}
	app.RegisterTaxCalculator(nil)
}

func TestApp_RegisterTaxCalculator_DoubleRegistrationPanics(t *testing.T) {
	app := &plugin.App{}
	app.RegisterTaxCalculator(stubTaxCalculator{})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for double tax calculator registration")
		}
	}()
	app.RegisterTaxCalculator(stubTaxCalculator{})
}

func TestRegistry_StubCorePluginRegistersProvider(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	provider := &stubSearchProvider{name: "stub"}
	reg.Register(&stubSearchPlugin{provider: provider})

	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{},
	}
	reg.InitAll(app)

	got, ok := app.SearchProvider()
	if !ok {
		t.Fatal("SearchProvider() ok = false after stub plugin init")
	}
	if got != provider {
		t.Fatalf("SearchProvider() = %v, want %v", got, provider)
	}
}

func TestRegistry_FailedPluginDoesNotPanic(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	reg.Register(&stubSearchPlugin{name: "stub/failing", initErr: errors.New("boom")})
	reg.Register(&stubSearchPlugin{name: "stub/ok", provider: &stubSearchProvider{name: "ok"}})

	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{},
	}
	summary := reg.InitAll(app)

	if summary.Failed != 1 || summary.Initialized != 1 {
		t.Fatalf("summary = %+v, want 1 failed and 1 initialized", summary)
	}

	got, ok := app.SearchProvider()
	if !ok {
		t.Fatal("SearchProvider() ok = false, want provider from second plugin")
	}
	if got.(*stubSearchProvider).name != "ok" {
		t.Fatalf("SearchProvider() name = %q, want ok", got.(*stubSearchProvider).name)
	}
}

func TestCorePostgresSearchEnabled(t *testing.T) {
	cfg := &config.Config{Search: config.SearchConfig{Engine: "meilisearch"}}
	if cfg.CorePostgresSearchEnabled() {
		t.Fatal("CorePostgresSearchEnabled() = true, want false for meilisearch")
	}

	cfg.Search.Engine = "postgres"
	if !cfg.CorePostgresSearchEnabled() {
		t.Fatal("CorePostgresSearchEnabled() = false, want true for postgres")
	}

	disabled := false
	cfg.Plugins.Core.PostgresSearch = &disabled
	if cfg.CorePostgresSearchEnabled() {
		t.Fatal("CorePostgresSearchEnabled() = true, want false when explicitly disabled")
	}
}
