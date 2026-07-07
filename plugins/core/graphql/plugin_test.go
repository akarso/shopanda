package graphql_test

import (
	"context"
	"database/sql"
	"io"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	cgraphql "github.com/akarso/shopanda/plugins/core/graphql"
)

func testApp(cfg *config.Config) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
		Bootstrap: &plugin.Bootstrap{
			DB: &sql.DB{},
		},
	}
}

func TestPlugin_Name(t *testing.T) {
	if got := cgraphql.NewPlugin().Name(); got != "core/graphql-api" {
		t.Fatalf("Name() = %q, want core/graphql-api", got)
	}
}

func TestPlugin_Init_Disabled(t *testing.T) {
	cfg := &config.Config{}
	if err := cgraphql.NewPlugin().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when plugins.graphql.enabled=false")
	}
}

func TestPlugin_Init_MissingDB(t *testing.T) {
	cfg := &config.Config{Plugins: config.PluginsConfig{GraphQL: config.GraphQLPluginConfig{Enabled: true}}}
	app := testApp(cfg)
	app.Bootstrap = nil
	if err := cgraphql.NewPlugin().Init(app); err == nil {
		t.Fatal("Init() expected error when database bootstrap is missing")
	}
}

func TestPlugin_Init_RegistersPublicRoute(t *testing.T) {
	cfg := &config.Config{Plugins: config.PluginsConfig{GraphQL: config.GraphQLPluginConfig{Enabled: true}}}
	app := testApp(cfg)

	product, err := catalog.NewProduct("prod-1", "Widget", "widget")
	if err != nil {
		t.Fatalf("NewProduct: %v", err)
	}

	plugin := cgraphql.NewPlugin()
	plugin.NewResolver = func(_ *sql.DB, _ *extensionapp.Registry) (*cgraphql.Resolver, error) {
		return cgraphql.NewResolver(
			&stubProductRepo{
				listFn: func(_ context.Context, _, _ int) ([]catalog.Product, error) {
					return []catalog.Product{product}, nil
				},
			},
			&stubCategoryRepo{},
		)
	}
	if err := plugin.Init(app); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	routes := app.PublicRoutes()
	if len(routes) != 1 {
		t.Fatalf("PublicRoutes() len = %d, want 1", len(routes))
	}
	if routes[0].Pattern != "POST /api/v1/graphql" {
		t.Fatalf("Pattern = %q", routes[0].Pattern)
	}
}
