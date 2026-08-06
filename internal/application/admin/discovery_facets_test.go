package admin_test

import (
	"context"
	"errors"
	"testing"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

type mockFacetConfigurer struct {
	codes []string
	err   error
}

func (m *mockFacetConfigurer) ConfigureAttributeFacets(_ context.Context, codes []string) error {
	m.codes = append([]string(nil), codes...)
	return m.err
}

func TestDiscoveryFacetSyncer_Sync(t *testing.T) {
	ctx := context.Background()
	store := adminApp.NewAttributeStore(newMockConfigRepo())
	if err := store.CreateAttribute(ctx, catalog.Attribute{Code: "color", Label: "Color", Type: catalog.AttributeTypeText, UseInLayeredNav: true}); err != nil {
		t.Fatalf("CreateAttribute: %v", err)
	}
	if err := store.CreateAttribute(ctx, catalog.Attribute{Code: "brand", Label: "Brand", Type: catalog.AttributeTypeText, UseInAdvancedSearch: true}); err != nil {
		t.Fatalf("CreateAttribute: %v", err)
	}

	configurer := &mockFacetConfigurer{}
	syncer := adminApp.NewDiscoveryFacetSyncer(store, configurer)
	if err := syncer.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(configurer.codes) != 2 || configurer.codes[0] != "brand" || configurer.codes[1] != "color" {
		t.Fatalf("codes = %v, want [brand color]", configurer.codes)
	}
}

func TestDiscoveryFacetSyncer_NoOpWithoutConfigurer(t *testing.T) {
	store := adminApp.NewAttributeStore(newMockConfigRepo())
	syncer := adminApp.NewDiscoveryFacetSyncer(store, nil)
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func TestDiscoveryFacetSyncer_PropagatesError(t *testing.T) {
	store := adminApp.NewAttributeStore(newMockConfigRepo())
	syncer := adminApp.NewDiscoveryFacetSyncer(store, &mockFacetConfigurer{err: errors.New("meili down")})
	err := syncer.Sync(context.Background())
	if err == nil || err.Error() != "meili down" {
		t.Fatalf("Sync = %v, want meili down", err)
	}
}
