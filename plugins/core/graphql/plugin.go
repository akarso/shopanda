package graphql

import (
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// Plugin registers the optional read-only GraphQL catalog API.
type Plugin struct {
	NewResolver func(db *sql.DB) (*Resolver, error)
}

// NewPlugin creates a GraphQL API plugin.
func NewPlugin() *Plugin {
	return &Plugin{NewResolver: newResolverFromDB}
}

func (p *Plugin) Name() string { return "core/graphql-api" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("graphql plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("graphql plugin: config not configured")
	}
	if !app.Config.Plugins.GraphQL.Enabled {
		return fmt.Errorf("graphql plugin: disabled (plugins.graphql.enabled=false)")
	}
	if app.Bootstrap == nil || app.Bootstrap.DB == nil {
		return fmt.Errorf("graphql plugin: database bootstrap not configured")
	}

	newResolver := p.NewResolver
	if newResolver == nil {
		newResolver = newResolverFromDB
	}
	resolver, err := newResolver(app.Bootstrap.DB)
	if err != nil {
		return fmt.Errorf("graphql plugin: resolver: %w", err)
	}

	schema, err := NewSchema(resolver)
	if err != nil {
		return fmt.Errorf("graphql plugin: schema: %w", err)
	}

	handler := NewHandler(schema)
	if err := app.RegisterPublicRoute("POST /api/v1/graphql", handler); err != nil {
		return fmt.Errorf("graphql plugin: register route: %w", err)
	}

	if app.Logger != nil {
		app.Logger.Info("graphql plugin: read-only catalog API registered at POST /api/v1/graphql", nil)
	}
	return nil
}

func newResolverFromDB(db *sql.DB) (*Resolver, error) {
	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		return nil, err
	}
	categoryRepo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		return nil, err
	}
	return NewResolver(productRepo, categoryRepo)
}
