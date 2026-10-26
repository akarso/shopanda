package storefront

import (
	"net/http"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// wantSnakeFieldNames is the opt-in storefront JSON shape for this release.
// Default remains Go exported names so existing clients do not break.
func wantSnakeFieldNames(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("fields")), "snake") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Shopanda-Field-Names")), "snake")
}

func productResponse(ctx *composition.ProductContext, snake bool) map[string]interface{} {
	if snake {
		return map[string]interface{}{
			"product": productPublicJSON(ctx.Product),
			"blocks":  ctx.Blocks,
			"meta":    ctx.Meta,
		}
	}
	return map[string]interface{}{
		"product": ctx.Product,
		"blocks":  ctx.Blocks,
		"meta":    ctx.Meta,
	}
}

func listingResponse(ctx *composition.ListingContext, snake bool) map[string]interface{} {
	if snake {
		return map[string]interface{}{
			"products": productsPublicJSON(ctx.Products),
			"blocks":   ctx.Blocks,
			"filters":  ctx.Filters,
			"meta":     ctx.Meta,
		}
	}
	return map[string]interface{}{
		"products": ctx.Products,
		"blocks":   ctx.Blocks,
		"filters":  ctx.Filters,
		"meta":     ctx.Meta,
	}
}

func productPublicJSON(p *catalog.Product) map[string]interface{} {
	if p == nil {
		return nil
	}
	return map[string]interface{}{
		"id":          p.ID,
		"name":        p.Name,
		"slug":        p.Slug,
		"description": p.Description,
		"status":      string(p.Status),
		"type":        string(p.Type),
		"attributes":  p.Attributes,
		"created_at":  formatPublicTime(p.CreatedAt),
		"updated_at":  formatPublicTime(p.UpdatedAt),
	}
}

func productsPublicJSON(products []*catalog.Product) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(products))
	for _, p := range products {
		out = append(out, productPublicJSON(p))
	}
	return out
}

func productsValuePublicJSON(products []catalog.Product, snake bool) interface{} {
	if !snake {
		return products
	}
	out := make([]map[string]interface{}, 0, len(products))
	for i := range products {
		out = append(out, productPublicJSON(&products[i]))
	}
	return out
}

func categoryJSON(c *catalog.Category, snake bool) interface{} {
	if !snake {
		return c
	}
	return categoryPublicJSON(c)
}

func categoryPublicJSON(c *catalog.Category) map[string]interface{} {
	if c == nil {
		return nil
	}
	return map[string]interface{}{
		"id":         c.ID,
		"parent_id":  c.ParentID,
		"name":       c.Name,
		"slug":       c.Slug,
		"position":   c.Position,
		"meta":       c.Meta,
		"created_at": formatPublicTime(c.CreatedAt),
		"updated_at": formatPublicTime(c.UpdatedAt),
	}
}

func formatPublicTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
