package cms

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
	domaincms "github.com/akarso/shopanda/internal/domain/cms"
)

// ResolvedContentBlock is a block ready for API or storefront rendering.
type ResolvedContentBlock struct {
	ID    string
	Type  string
	Title string
	Data  map[string]interface{}
}

// BlockResolver resolves content blocks for rendering.
type BlockResolver struct {
	products        catalog.ProductRepository
	visibilityFacts catalog.VisibilityFactsSource
	visibilityOpts  catalog.VisibilityOptions
}

// NewBlockResolver creates a BlockResolver.
func NewBlockResolver(products catalog.ProductRepository) *BlockResolver {
	return &BlockResolver{products: products}
}

// WithVisibilityFacts filters product carousels by VisibleInCatalog (PR-1057).
func (r *BlockResolver) WithVisibilityFacts(facts catalog.VisibilityFactsSource) *BlockResolver {
	r.visibilityFacts = facts
	return r
}

// ResolveBlocks hydrates block configs for rendering.
func (r *BlockResolver) ResolveBlocks(ctx context.Context, blocks []*domaincms.ContentBlock) ([]ResolvedContentBlock, error) {
	if len(blocks) == 0 {
		return nil, nil
	}
	productCache, err := r.loadProducts(ctx, blocks)
	if err != nil {
		return nil, err
	}
	out := make([]ResolvedContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if block == nil || !block.IsActive() {
			continue
		}
		resolved, err := r.resolveBlock(block, productCache)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved)
	}
	return out, nil
}

func (r *BlockResolver) resolveBlock(block *domaincms.ContentBlock, products map[string]*catalog.Product) (ResolvedContentBlock, error) {
	config := block.Config()
	data := map[string]interface{}{}
	switch block.BlockType() {
	case domaincms.BlockTypeHero:
		data["headline"] = config["headline"]
		data["subheadline"] = config["subheadline"]
		data["cta_label"] = config["cta_label"]
		data["cta_url"] = config["cta_url"]
		data["image_url"] = config["image_url"]
	case domaincms.BlockTypeRichText:
		data["body"] = config["body"]
	case domaincms.BlockTypeProductCarousel:
		data["title"] = config["title"]
		// Keep configured IDs for FPC product tags even when visibility
		// filters omit them from the rendered carousel (PR-1057).
		data["product_ids"] = stringSliceFromAny(config["product_ids"])
		data["products"] = r.carouselProducts(config["product_ids"], products)
	default:
		return ResolvedContentBlock{}, fmt.Errorf("block resolver: unsupported type %q", block.BlockType())
	}
	return ResolvedContentBlock{
		ID:    block.ID(),
		Type:  string(block.BlockType()),
		Title: block.Title(),
		Data:  data,
	}, nil
}

func (r *BlockResolver) carouselProducts(raw interface{}, products map[string]*catalog.Product) []map[string]interface{} {
	ids := stringSliceFromAny(raw)
	out := make([]map[string]interface{}, 0, len(ids))
	for _, productID := range ids {
		product := products[productID]
		if product == nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"id":          product.ID,
			"name":        product.Name,
			"slug":        product.Slug,
			"description": product.Description,
			"image_url":   imageURLFromProduct(product),
		})
	}
	return out
}

func (r *BlockResolver) loadProducts(ctx context.Context, blocks []*domaincms.ContentBlock) (map[string]*catalog.Product, error) {
	cache := map[string]*catalog.Product{}
	if r.products == nil {
		return cache, nil
	}
	ids := make(map[string]struct{})
	for _, block := range blocks {
		if block == nil || block.BlockType() != domaincms.BlockTypeProductCarousel {
			continue
		}
		for _, productID := range stringSliceFromAny(block.Config()["product_ids"]) {
			ids[productID] = struct{}{}
		}
	}
	idList := make([]string, 0, len(ids))
	for productID := range ids {
		idList = append(idList, productID)
	}
	if r.visibilityFacts != nil {
		allowed, err := catalog.FilterIDsWithFacts(ctx, idList, catalog.VisibilitySurfaceCatalog, r.visibilityFacts, r.visibilityOpts)
		if err != nil {
			return nil, fmt.Errorf("block resolver: visibility: %w", err)
		}
		idList = allowed
	}
	for _, productID := range idList {
		product, err := r.products.FindByID(ctx, productID)
		if err != nil {
			return nil, fmt.Errorf("block resolver: product %q: %w", productID, err)
		}
		if product != nil {
			cache[productID] = product
		}
	}
	return cache, nil
}

func stringSliceFromAny(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := fmt.Sprint(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func imageURLFromProduct(product *catalog.Product) string {
	if product == nil || product.Attributes == nil {
		return ""
	}
	if value, ok := product.Attributes["image_url"]; ok {
		if s, ok := value.(string); ok {
			return s
		}
	}
	if value, ok := product.Attributes["image"]; ok {
		if s, ok := value.(string); ok {
			return s
		}
	}
	return ""
}
