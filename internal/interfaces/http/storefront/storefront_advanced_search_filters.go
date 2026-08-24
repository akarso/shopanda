package storefront

import (
	"context"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/search"
)

// WithAdvancedSearchAttributes enables advanced search attribute filters on /search.
func (h *StorefrontHandler) WithAdvancedSearchAttributes(lister AdvancedSearchAttributeLister) *StorefrontHandler {
	h.advancedSearchAttrs = lister
	return h
}

func (h *StorefrontHandler) advancedSearchAttributes(ctx context.Context) ([]catalog.Attribute, error) {
	if h.advancedSearchAttrs == nil {
		return nil, nil
	}
	return h.advancedSearchAttrs.ListAdvancedSearchAttributes(ctx)
}

func storefrontAdvancedSearchOnlyFilterGroups(r *http.Request, params storefrontListingParams, facets map[string][]search.FacetValue, layeredNav, advancedSearch []catalog.Attribute) []StorefrontFilterGroup {
	layeredSet := storefrontAllowedAttributeSet(layeredNav)
	advancedOnly := make([]catalog.Attribute, 0, len(advancedSearch))
	for _, attr := range advancedSearch {
		if _, inLayered := layeredSet[attr.Code]; inLayered {
			continue
		}
		advancedOnly = append(advancedOnly, attr)
	}
	return storefrontAttributeFilterGroups(r, params, facets, advancedOnly)
}
