package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/domain/store"
)

const storefrontAttributeQueryPrefix = "attr_"

// LayeredNavAttributeLister supplies attributes flagged for storefront layered navigation.
type LayeredNavAttributeLister interface {
	ListLayeredNavAttributes(ctx context.Context) ([]catalog.Attribute, error)
}

// WithLayeredNavAttributes enables attribute facet chips from catalog attribute metadata.
func (h *StorefrontHandler) WithLayeredNavAttributes(lister LayeredNavAttributeLister) *StorefrontHandler {
	h.layeredNavAttrs = lister
	return h
}

func (h *StorefrontHandler) layeredNavAttributes(ctx context.Context) ([]catalog.Attribute, error) {
	if h.layeredNavAttrs == nil {
		return nil, nil
	}
	return h.layeredNavAttrs.ListLayeredNavAttributes(ctx)
}

func storefrontBuildSearchQuery(params storefrontListingParams, layeredNav []catalog.Attribute) search.SearchQuery {
	query := search.SearchQuery{
		Text:    params.Query,
		Sort:    storefrontSearchSort(params.Sort),
		Limit:   params.PerPage,
		Offset:  (params.Page - 1) * params.PerPage,
		Filters: map[string]interface{}{},
	}
	if params.CategoryID != "" {
		query.Filters["category"] = params.CategoryID
	}
	allowed := storefrontLayeredNavAttrSet(layeredNav)
	for code, value := range params.AttributeFilters {
		if _, ok := allowed[code]; !ok {
			continue
		}
		query.Filters[storefrontAttributeQueryPrefix+code] = value
	}
	if len(layeredNav) > 0 {
		codes := make([]string, 0, len(layeredNav))
		for _, attr := range layeredNav {
			codes = append(codes, attr.Code)
		}
		query.FacetAttributes = codes
	}
	return query
}

func storefrontApplyStoreScope(query *search.SearchQuery, r *http.Request) {
	if s := store.FromContext(r.Context()); s != nil {
		query.StoreID = s.ID
		query.Currency = s.Currency
	}
}

func storefrontLayeredNavAttrSet(attrs []catalog.Attribute) map[string]struct{} {
	out := make(map[string]struct{}, len(attrs))
	for _, attr := range attrs {
		if storefrontAttributeCodeValid(attr.Code) {
			out[attr.Code] = struct{}{}
		}
	}
	return out
}

func storefrontAttributeCodeValid(code string) bool {
	if code == "" || len(code) > 64 {
		return false
	}
	for i, r := range code {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' && i > 0 {
			continue
		}
		if r == '_' && i > 0 {
			continue
		}
		return false
	}
	return true
}

func parseStorefrontAttributeFilters(q map[string][]string) map[string]string {
	out := map[string]string{}
	for key, values := range q {
		if !strings.HasPrefix(key, storefrontAttributeQueryPrefix) || len(values) == 0 {
			continue
		}
		code := strings.TrimPrefix(key, storefrontAttributeQueryPrefix)
		if !storefrontAttributeCodeValid(code) {
			continue
		}
		value := strings.TrimSpace(values[0])
		if value == "" {
			continue
		}
		out[code] = value
	}
	return out
}

func storefrontAttributeFilterGroups(r *http.Request, params storefrontListingParams, facets map[string][]search.FacetValue, layeredNav []catalog.Attribute) []StorefrontFilterGroup {
	if len(layeredNav) == 0 {
		return nil
	}
	groups := make([]StorefrontFilterGroup, 0, len(layeredNav))
	for _, attr := range layeredNav {
		values, ok := facets[attr.Code]
		if !ok || len(values) == 0 {
			continue
		}
		selectedValue := params.AttributeFilters[attr.Code]
		group := StorefrontFilterGroup{
			Name:   attr.Label,
			Values: make([]StorefrontFilterValue, 0, len(values)),
		}
		seen := make(map[string]struct{}, len(values))
		for _, facet := range values {
			if facet.Value == "" {
				continue
			}
			if _, dup := seen[facet.Value]; dup {
				continue
			}
			seen[facet.Value] = struct{}{}
			selected := facet.Value == selectedValue
			group.Values = append(group.Values, StorefrontFilterValue{
				Label:    facet.Value,
				Count:    facet.Count,
				URL:      storefrontAttributeFacetURL(r, params, attr.Code, facet.Value, selected),
				Selected: selected,
			})
		}
		if len(group.Values) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

func storefrontAttributeFacetURL(r *http.Request, params storefrontListingParams, code, value string, selected bool) string {
	overrides := map[string]string{"page": "1"}
	key := storefrontAttributeQueryPrefix + code
	if selected {
		overrides[key] = ""
	} else {
		overrides[key] = value
	}
	return storefrontURL(r, params, overrides)
}
