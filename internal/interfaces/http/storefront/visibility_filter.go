package storefront

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/search"
)

// filterSearchHitsByVisibility drops products not allowed on surface.
// Used as a fail-closed safety net when enforce is on (facts non-nil): index
// filters keep Total/Facets honest on a current schema, but facts!=nil alone
// is not proof the engine honored new keys (PR-1079 review). Nil facts is a
// no-op (enforce off).
func filterSearchHitsByVisibility(
	r *http.Request,
	result search.SearchResult,
	surface catalog.VisibilitySurface,
	facts catalog.VisibilityFactsSource,
	opts catalog.VisibilityOptions,
) (search.SearchResult, error) {
	if facts == nil || len(result.Products) == 0 {
		return result, nil
	}
	ids := make([]string, len(result.Products))
	for i := range result.Products {
		ids[i] = result.Products[i].ID
	}
	allowed, err := catalog.FilterIDsWithFacts(r.Context(), ids, surface, facts, opts)
	if err != nil {
		return search.SearchResult{}, err
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, id := range allowed {
		allow[id] = struct{}{}
	}
	filtered := make([]search.Product, 0, len(allowed))
	for i := range result.Products {
		if _, ok := allow[result.Products[i].ID]; ok {
			filtered = append(filtered, result.Products[i])
		}
	}
	result.Products = filtered
	// Total / Facets stay index-level; if this safety net removes hits the
	// engine ignored a filter key — prefer fixing index/schema (DEPLOYMENT).
	return result, nil
}

// filterSuggestionsByVisibility drops product suggestions not VisibleInSearch.
// Fail-closed safety net when facts are non-nil (enforce on).
func filterSuggestionsByVisibility(
	r *http.Request,
	suggestions []search.Suggestion,
	facts catalog.VisibilityFactsSource,
	opts catalog.VisibilityOptions,
) ([]search.Suggestion, error) {
	if facts == nil || len(suggestions) == 0 {
		return suggestions, nil
	}
	ids := make([]string, 0, len(suggestions))
	for _, s := range suggestions {
		if s.Type == "product" && s.ProductID != "" {
			ids = append(ids, s.ProductID)
		}
	}
	allowed, err := catalog.FilterIDsWithFacts(r.Context(), ids, catalog.VisibilitySurfaceSearch, facts, opts)
	if err != nil {
		return nil, err
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, id := range allowed {
		allow[id] = struct{}{}
	}
	out := make([]search.Suggestion, 0, len(suggestions))
	for _, s := range suggestions {
		if s.Type != "product" {
			out = append(out, s)
			continue
		}
		if s.ProductID == "" {
			continue
		}
		if _, ok := allow[s.ProductID]; ok {
			out = append(out, s)
		}
	}
	return out, nil
}
