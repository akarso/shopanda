package cms_test

import (
	"context"
	"testing"

	cmsApp "github.com/akarso/shopanda/internal/application/cms"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/cms"
)

type blockResolverProductRepo struct {
	byID map[string]*catalog.Product
}

func (r blockResolverProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return r.byID[id], nil
}
func (blockResolverProductRepo) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (blockResolverProductRepo) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (blockResolverProductRepo) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (blockResolverProductRepo) Create(context.Context, *catalog.Product) error { return nil }
func (blockResolverProductRepo) Update(context.Context, *catalog.Product) error { return nil }
func (blockResolverProductRepo) Delete(context.Context, string) error           { return nil }

func TestBlockResolverResolveBlocks(t *testing.T) {
	product := &catalog.Product{
		ID:          "prod-1",
		Name:        "Headphones",
		Slug:        "headphones",
		Description: "Wireless",
		Attributes:  map[string]interface{}{"image_url": "/img/headphones.jpg"},
	}
	resolver := cmsApp.NewBlockResolver(blockResolverProductRepo{
		byID: map[string]*catalog.Product{"prod-1": product},
	})

	hero, _ := cms.NewContentBlock("hero-1", "Hero", cms.BlockTypeHero, map[string]interface{}{
		"headline": "Welcome",
	})
	carousel, _ := cms.NewContentBlock("carousel-1", "Featured", cms.BlockTypeProductCarousel, map[string]interface{}{
		"title":       "Featured",
		"product_ids": []string{"prod-1", "prod-1"},
	})

	resolved, err := resolver.ResolveBlocks(context.Background(), []*cms.ContentBlock{hero, carousel})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(resolved))
	}
	products, ok := resolved[1].Data["products"].([]map[string]interface{})
	if !ok || len(products) != 2 {
		t.Fatalf("expected two carousel products for duplicate ids, got %+v", resolved[1].Data["products"])
	}
}

type mapFacts map[string]catalog.VisibilitySnapshot

func (m mapFacts) LoadMany(_ context.Context, ids []string) (map[string]catalog.VisibilitySnapshot, error) {
	out := make(map[string]catalog.VisibilitySnapshot, len(ids))
	for _, id := range ids {
		if s, ok := m[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

func TestBlockResolver_FiltersHiddenCarouselProducts(t *testing.T) {
	modes := catalog.DefaultVisibilityAxes()
	hidden := modes
	hidden.Catalog = catalog.VisibilityModeHidden
	visible := &catalog.Product{ID: "prod-v", Name: "Visible", Slug: "v", Status: catalog.StatusActive}
	hiddenP := &catalog.Product{ID: "prod-h", Name: "Hidden", Slug: "h", Status: catalog.StatusActive}
	resolver := cmsApp.NewBlockResolver(blockResolverProductRepo{
		byID: map[string]*catalog.Product{"prod-v": visible, "prod-h": hiddenP},
	}).WithVisibilityFacts(mapFacts{
		"prod-v": {Status: catalog.StatusActive, Modes: modes, Inputs: catalog.VisibilityInputs{QuantityPositive: true, HasPrice: true, HasStoreAssignment: true}},
		"prod-h": {Status: catalog.StatusActive, Modes: hidden, Inputs: catalog.VisibilityInputs{QuantityPositive: true, HasPrice: true, HasStoreAssignment: true}},
	})

	carousel, _ := cms.NewContentBlock("carousel-1", "Featured", cms.BlockTypeProductCarousel, map[string]interface{}{
		"product_ids": []string{"prod-v", "prod-h"},
	})
	resolved, err := resolver.ResolveBlocks(context.Background(), []*cms.ContentBlock{carousel})
	if err != nil {
		t.Fatal(err)
	}
	products, ok := resolved[0].Data["products"].([]map[string]interface{})
	if !ok || len(products) != 1 {
		t.Fatalf("want 1 visible carousel product, got %+v", resolved[0].Data["products"])
	}
	if products[0]["id"] != "prod-v" {
		t.Fatalf("id = %v, want prod-v", products[0]["id"])
	}
}
