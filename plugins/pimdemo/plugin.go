package pimdemo

import (
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/platform/plugin"
	sdkgraphql "github.com/akarso/shopanda/pkg/integrationsdk/graphql"
)

// Plugin demonstrates outbound PIM GraphQL enrichment on the PDP composition pipeline.
type Plugin struct{}

// New returns the PIM enrichment reference plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "pimdemo/reference" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("pimdemo plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("pimdemo plugin: config not configured")
	}
	cfg := app.Config.Plugins.PimDemo
	if !cfg.Enabled {
		return fmt.Errorf("pimdemo plugin: disabled (plugins.pimdemo.enabled=false)")
	}
	if cfg.PimGraphQLEndpoint == "" {
		return fmt.Errorf("pimdemo plugin: pim_graphql_endpoint is required")
	}

	headers := map[string]string{}
	if cfg.PimAPIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.PimAPIKey
	}
	client, err := sdkgraphql.New(sdkgraphql.Config{
		Endpoint: cfg.PimGraphQLEndpoint,
		Headers:  headers,
		Logger:   app.Logger,
	})
	if err != nil {
		return fmt.Errorf("pimdemo plugin: graphql client: %w", err)
	}

	cacheTTL := 5 * time.Minute
	if cfg.CacheTTL != "" {
		parsed, err := time.ParseDuration(cfg.CacheTTL)
		if err != nil {
			return fmt.Errorf("pimdemo plugin: invalid cache_ttl %q: %w", cfg.CacheTTL, err)
		}
		if parsed > 0 {
			cacheTTL = parsed
		}
	}

	step := NewEnrichmentStep(newEnrichmentFetcher(client), newTTLCache(cacheTTL), app.Logger)
	app.RegisterCompositionStep("pdp", step)
	if app.Logger != nil {
		app.Logger.Info("pimdemo plugin: PDP enrichment step registered", map[string]interface{}{
			"step":      StepName,
			"cache_ttl": cacheTTL.String(),
		})
	}
	return nil
}
