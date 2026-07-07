package graphql

import (
	"database/sql"
	"fmt"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/infrastructure/postgres"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// Plugin registers the optional read-only GraphQL catalog API.
type Plugin struct {
	NewResolver func(db *sql.DB, extensionRegistry *extensionapp.Registry) (*Resolver, error)
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
	resolver, err := newResolver(app.Bootstrap.DB, app.ExtensionRegistry())
	if err != nil {
		return fmt.Errorf("graphql plugin: resolver: %w", err)
	}

	schema, err := NewSchema(resolver)
	if err != nil {
		return fmt.Errorf("graphql plugin: schema: %w", err)
	}

	handler := NewHandler(schema, app.Logger)
	if err := app.RegisterPublicRoute("POST /api/v1/graphql", handler); err != nil {
		return fmt.Errorf("graphql plugin: register route: %w", err)
	}

	if app.Logger != nil {
		app.Logger.Info("graphql plugin: read-only catalog API registered at POST /api/v1/graphql", nil)
	}
	return nil
}

func newResolverFromDB(db *sql.DB, extensionRegistry *extensionapp.Registry) (*Resolver, error) {
	productRepo, err := postgres.NewProductRepo(db)
	if err != nil {
		return nil, err
	}
	categoryRepo, err := postgres.NewCategoryRepo(db)
	if err != nil {
		return nil, err
	}
	extFieldRepo, err := postgres.NewExtensionFieldRepo(db)
	if err != nil {
		return nil, err
	}
	extValueRepo, err := postgres.NewExtensionValueRepo(db)
	if err != nil {
		return nil, err
	}
	if extensionRegistry == nil {
		extensionRegistry = extensionapp.NewRegistry()
	}
	fields := extensionapp.NewFieldService(extensionRegistry, extFieldRepo)
	values := extensionapp.NewValueService(extensionRegistry, extValueRepo)

	resolver, err := NewResolver(productRepo, categoryRepo)
	if err != nil {
		return nil, err
	}
	return resolver.WithExtensions(fields, values), nil
}
