package graphql

import (
	"context"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/catalog"
)

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// Resolver loads catalog data for GraphQL queries.
type Resolver struct {
	productRepo  catalog.ProductRepository
	categoryRepo catalog.CategoryRepository
}

// NewResolver creates a Resolver.
func NewResolver(products catalog.ProductRepository, categories catalog.CategoryRepository) (*Resolver, error) {
	if products == nil {
		return nil, fmt.Errorf("graphql resolver: products repository must not be nil")
	}
	if categories == nil {
		return nil, fmt.Errorf("graphql resolver: categories repository must not be nil")
	}
	return &Resolver{productRepo: products, categoryRepo: categories}, nil
}

func (r *Resolver) productByID(ctx context.Context, id string) (*catalog.Product, error) {
	if id == "" {
		return nil, fmt.Errorf("product id is required")
	}
	return r.productRepo.FindByID(ctx, id)
}

func (r *Resolver) productBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	if slug == "" {
		return nil, fmt.Errorf("product slug is required")
	}
	return r.productRepo.FindBySlug(ctx, slug)
}

// normalizeLimit applies the shared pagination bounds: non-positive limits fall
// back to defaultListLimit and limits above maxListLimit are capped.
func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

func (r *Resolver) products(ctx context.Context, offset, limit int) ([]catalog.Product, error) {
	if offset < 0 {
		return nil, fmt.Errorf("offset must be >= 0")
	}
	return r.productRepo.List(ctx, offset, normalizeLimit(limit))
}

func (r *Resolver) categoryByID(ctx context.Context, id string) (*catalog.Category, error) {
	if id == "" {
		return nil, fmt.Errorf("category id is required")
	}
	return r.categoryRepo.FindByID(ctx, id)
}

func (r *Resolver) categories(ctx context.Context) ([]catalog.Category, error) {
	return r.categoryRepo.FindAll(ctx)
}

func (r *Resolver) categoryProducts(ctx context.Context, categoryID string, offset, limit int) ([]catalog.Product, error) {
	if categoryID == "" {
		return nil, fmt.Errorf("category id is required")
	}
	if offset < 0 {
		return nil, fmt.Errorf("offset must be >= 0")
	}
	return r.productRepo.FindByCategoryID(ctx, categoryID, offset, normalizeLimit(limit))
}

func intArg(args map[string]interface{}, name string, fallback int) (int, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return fallback, nil
	}
	switch v := raw.(type) {
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", name)
	}
}

func stringArg(args map[string]interface{}, name string) (string, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return "", fmt.Errorf("%s is required", name)
	}
	s, ok := raw.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("%s must be a non-empty string", name)
	}
	return s, nil
}
