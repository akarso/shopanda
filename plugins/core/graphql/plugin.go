package graphql

import (
	"database/sql"
	"fmt"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/domain/catalog"
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

	if err := attachVisibility(resolver, app); err != nil {
		return fmt.Errorf("graphql plugin: visibility: %w", err)
	}
	if err := attachComposition(resolver, app.Bootstrap.DB); err != nil {
		return fmt.Errorf("graphql plugin: composition: %w", err)
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

// newVisibilityFactsSource builds the facts repo used when enforce is on.
// Tests may replace it to assert attachVisibility runs without a live DB.
var newVisibilityFactsSource = func(db *sql.DB) (catalog.VisibilityFactsSource, error) {
	return postgres.NewVisibilityFactsRepo(db)
}

// setVisibilityFactsSourceForTest replaces the facts-source factory used by
// attachVisibility. It returns a restore func for test cleanup. Not part of
// the public plugin API (see export_test.go for the test-only re-export).
func setVisibilityFactsSourceForTest(fn func(db *sql.DB) (catalog.VisibilityFactsSource, error)) func() {
	prev := newVisibilityFactsSource
	if fn == nil {
		newVisibilityFactsSource = func(db *sql.DB) (catalog.VisibilityFactsSource, error) {
			return postgres.NewVisibilityFactsRepo(db)
		}
	} else {
		newVisibilityFactsSource = fn
	}
	return func() { newVisibilityFactsSource = prev }
}

func attachVisibility(resolver *Resolver, app *plugin.App) error {
	if resolver == nil || app == nil || app.Config == nil {
		return nil
	}
	// Match REST PR-1057: facts are only wired when enforce is on. Nil facts
	// leave AllowWithFacts / ListVisiblePage ungated.
	if !app.Config.Catalog.Visibility.Enforce {
		return nil
	}
	if app.Bootstrap == nil || app.Bootstrap.DB == nil {
		return fmt.Errorf("database bootstrap not configured")
	}
	factsRepo, err := newVisibilityFactsSource(app.Bootstrap.DB)
	if err != nil {
		return err
	}
	resolver.WithVisibility(factsRepo, catalog.VisibilityOptions{})
	return nil
}

func attachComposition(resolver *Resolver, db *sql.DB) error {
	if resolver == nil || db == nil {
		return nil
	}
	bundleRepo, err := postgres.NewBundleComponentRepo(db)
	if err != nil {
		return err
	}
	groupedRepo, err := postgres.NewGroupedMemberRepo(db)
	if err != nil {
		return err
	}
	// Reuse the resolver's product repo (already constructed in NewResolver)
	// so composition list services do not open a second write-capable stack.
	resolver.WithComposition(
		catalogApp.NewBundleService(resolver.productRepo, bundleRepo, db),
		catalogApp.NewGroupedService(resolver.productRepo, groupedRepo, db),
	)
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
