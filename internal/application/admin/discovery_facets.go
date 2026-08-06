package admin

import "context"

// AttributeFacetConfigurer updates search-engine filterable fields for discovery attributes.
type AttributeFacetConfigurer interface {
	ConfigureAttributeFacets(context.Context, []string) error
}

// DiscoveryFacetSyncer syncs discovery-flagged attribute codes to the active search engine.
type DiscoveryFacetSyncer struct {
	store  *AttributeStore
	engine AttributeFacetConfigurer
}

// NewDiscoveryFacetSyncer creates a syncer. engine may implement AttributeFacetConfigurer; otherwise Sync is a no-op.
func NewDiscoveryFacetSyncer(store *AttributeStore, engine interface{}) *DiscoveryFacetSyncer {
	var configurer AttributeFacetConfigurer
	if c, ok := engine.(AttributeFacetConfigurer); ok {
		configurer = c
	}
	return &DiscoveryFacetSyncer{store: store, engine: configurer}
}

// Sync reloads Meilisearch filterable attribute fields from current discovery flags.
func (s *DiscoveryFacetSyncer) Sync(ctx context.Context) error {
	if s.engine == nil {
		return nil
	}
	codes, err := s.store.ListDiscoveryAttributeCodes(ctx)
	if err != nil {
		return err
	}
	return s.engine.ConfigureAttributeFacets(ctx, codes)
}
