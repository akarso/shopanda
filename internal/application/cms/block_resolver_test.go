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
func (blockResolverProductRepo) List(context.Context, int, int) ([]catalog.Product, error) {
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
