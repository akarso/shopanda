package composition

import (
	"fmt"
	"unicode/utf8"
)

// ProductMetaStep populates meta title and description from the product.
type ProductMetaStep struct{}

func (ProductMetaStep) Name() string { return "seo_meta" }

func (ProductMetaStep) Apply(ctx *ProductContext) error {
	if ctx.Product == nil {
		return nil
	}
	ctx.Meta["title"] = ctx.Product.Name
	desc := ctx.Product.Description
	if utf8.RuneCountInString(desc) > 160 {
		runes := []rune(desc)
		desc = string(runes[:160])
	}
	ctx.Meta["description"] = desc
	return nil
}

// ListingMetaStep populates meta title and description for listing pages.
type ListingMetaStep struct{}

func (ListingMetaStep) Name() string { return "seo_meta" }

func (ListingMetaStep) Apply(ctx *ListingContext) error {
	ctx.Meta["title"] = "Products"
	ctx.Meta["description"] = fmt.Sprintf("Browse %d products", len(ctx.Products))
	return nil
}
