package admin

import (
	"context"
	"sync"
)

// AttributeFacetConfigurer updates search-engine filterable fields for discovery attributes.
type AttributeFacetConfigurer interface {
	ConfigureAttributeFacets(context.Context, []string) error
}

// DiscoveryFacetSyncer syncs discovery-flagged attribute codes to the active search engine.
type DiscoveryFacetSyncer struct {
	store  *AttributeStore
	engine AttributeFacetConfigurer
	mu     sync.Mutex
}

// NewDiscoveryFacetSyncer creates a syncer. Pass nil engine when the search backend has no facet configurer.
func NewDiscoveryFacetSyncer(store *AttributeStore, engine AttributeFacetConfigurer) *DiscoveryFacetSyncer {
	return &DiscoveryFacetSyncer{store: store, engine: engine}
}

// Sync reloads Meilisearch filterable attribute fields from current discovery flags.
func (s *DiscoveryFacetSyncer) Sync(ctx context.Context) error {
	if s.engine == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	codes, err := s.store.ListDiscoveryAttributeCodes(ctx)
	if err != nil {
		return err
	}
	return s.engine.ConfigureAttributeFacets(ctx, codes)
}
