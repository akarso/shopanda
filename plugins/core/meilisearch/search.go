package meilisearch

import (
	"fmt"

	"github.com/akarso/shopanda/internal/infrastructure/meili"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// SearchPlugin registers the Meilisearch search backend.
type SearchPlugin struct{}

func NewSearchPlugin() *SearchPlugin { return &SearchPlugin{} }

func (p *SearchPlugin) Name() string { return "core/meilisearch-search" }

func (p *SearchPlugin) Init(app *plugin.App) error {
	if app.Config == nil {
		return fmt.Errorf("meilisearch search: config not configured")
	}
	if app.Config.Search.Engine != "meilisearch" {
		return fmt.Errorf("meilisearch search: disabled (search.engine=%q)", app.Config.Search.Engine)
	}

	cfg := app.Config.Search.Meilisearch
	if cfg.Host == "" {
		return fmt.Errorf("meilisearch search: empty host")
	}
	if cfg.Index == "" {
		return fmt.Errorf("meilisearch search: empty index")
	}

	se, err := meili.New(meili.Config{
		Host:   cfg.Host,
		APIKey: cfg.APIKey,
		Index:  cfg.Index,
	})
	if err != nil {
		return fmt.Errorf("meilisearch search: init client: %w", err)
	}
	app.RegisterSearchProvider(se)
	return nil
}
