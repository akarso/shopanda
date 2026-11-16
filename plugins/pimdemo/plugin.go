package pimdemo

import (
	"fmt"
	"time"

	sdkgraphql "github.com/akarso/shopanda/pkg/integrationsdk/graphql"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

// Plugin demonstrates outbound PIM GraphQL enrichment on the PDP composition pipeline.
type Plugin struct{}

// New returns the PIM enrichment reference plugin.
func New() pluginsdk.Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "pimdemo/reference" }

func (p *Plugin) Init(h pluginsdk.Host) error {
	if h == nil {
		return fmt.Errorf("pimdemo plugin: host not configured")
	}
	if !h.ConfigBool("plugins.pimdemo.enabled") {
		return fmt.Errorf("pimdemo plugin: disabled (plugins.pimdemo.enabled=false)")
	}
	endpoint := h.ConfigValue("plugins.pimdemo.pim_graphql_endpoint")
	if endpoint == "" {
		return fmt.Errorf("pimdemo plugin: pim_graphql_endpoint is required")
	}
	apiKey := h.ConfigValue("plugins.pimdemo.pim_api_key")
	cacheTTLStr := h.ConfigValue("plugins.pimdemo.cache_ttl")

	headers := map[string]string{}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	client, err := sdkgraphql.New(sdkgraphql.Config{
		Endpoint: endpoint,
		Timeout:  2 * time.Second,
		Headers:  headers,
		Logger:   h.Logger(),
	})
	if err != nil {
		return fmt.Errorf("pimdemo plugin: graphql client: %w", err)
	}

	cacheTTL := 5 * time.Minute
	if cacheTTLStr != "" {
		parsed, err := time.ParseDuration(cacheTTLStr)
		if err != nil {
			return fmt.Errorf("pimdemo plugin: invalid cache_ttl %q: %w", cacheTTLStr, err)
		}
		if parsed > 0 {
			cacheTTL = parsed
		}
	}

	step := NewEnrichmentStep(newEnrichmentFetcher(client), newTTLCache(cacheTTL), h.Logger())
	h.RegisterCompositionStep("pdp", step)
	if log := h.Logger(); log != nil {
		log.Info("pimdemo plugin: PDP enrichment step registered", map[string]interface{}{
			"step":      StepName,
			"cache_ttl": cacheTTL.String(),
		})
	}
	return nil
}
