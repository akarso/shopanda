package plugin_test

import (
	"errors"
	"io"
	"testing"

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
