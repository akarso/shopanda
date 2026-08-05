package http

import (
	"context"
	"net/url"
	"sort"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/search"
)

// AdvancedSearchAttributeLister supplies attributes flagged for advanced search filters.
type AdvancedSearchAttributeLister interface {
	ListAdvancedSearchAttributes(ctx context.Context) ([]catalog.Attribute, error)
}

func searchApplyAttributeFilters(query *search.SearchQuery, q url.Values, allowed []catalog.Attribute) {
	allowedSet := storefrontAllowedAttributeSet(allowed)
	for code, value := range parseStorefrontAttributeFilters(q) {
		if _, ok := allowedSet[code]; !ok {
			continue
		}
		query.Filters[storefrontAttributeQueryPrefix+code] = value
	}
	if len(allowed) > 0 {
		query.FacetAttributes = storefrontFacetAttributeCodes(allowed)
	}
}

func storefrontAllowedAttributeSet(attrs []catalog.Attribute) map[string]struct{} {
	out := make(map[string]struct{}, len(attrs))
	for _, attr := range attrs {
		if storefrontAttributeCodeValid(attr.Code) && !search.ReservedFacetKey(attr.Code) {
			out[attr.Code] = struct{}{}
		}
	}
	return out
}

func storefrontFacetAttributeCodes(attrs []catalog.Attribute) []string {
	codes := make([]string, 0, len(attrs))
	for _, attr := range attrs {
		if search.ReservedFacetKey(attr.Code) {
			continue
		}
		codes = append(codes, attr.Code)
	}
	return codes
}

func storefrontMergeAttributes(lists ...[]catalog.Attribute) []catalog.Attribute {
	byCode := make(map[string]catalog.Attribute)
	for _, list := range lists {
		for _, attr := range list {
			byCode[attr.Code] = attr
		}
	}
	if len(byCode) == 0 {
		return nil
	}
	codes := make([]string, 0, len(byCode))
	for code := range byCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	out := make([]catalog.Attribute, len(codes))
	for i, code := range codes {
		out[i] = byCode[code]
	}
	return out
}
