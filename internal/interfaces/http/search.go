package http

import (
	"net/http"
	"strconv"

	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// SearchHandler serves the public search endpoint.
type SearchHandler struct {
	engine search.SearchEngine
}

// NewSearchHandler creates a SearchHandler with the given search engine.
func NewSearchHandler(engine search.SearchEngine) *SearchHandler {
	if engine == nil {
		panic("SearchHandler: search engine must not be nil")
	}
	return &SearchHandler{engine: engine}
}

// Search handles GET /api/v1/search.
func (h *SearchHandler) Search() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		query := search.SearchQuery{
			Text:    q.Get("q"),
			Sort:    q.Get("sort"),
			Filters: map[string]interface{}{},
		}

		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				JSONError(w, apperror.Validation("limit must be a non-negative integer"))
				return
			}
			query.Limit = n
		}

		if v := q.Get("offset"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				JSONError(w, apperror.Validation("offset must be a non-negative integer"))
				return
			}
			query.Offset = n
		}

		if v := q.Get("category"); v != "" {
			query.Filters["category"] = v
		}

		result, err := h.engine.Search(r.Context(), query)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, searchResponse(result))
	}
}

// searchResponse builds the response body for a search result.
func searchResponse(r search.SearchResult) map[string]interface{} {
	products := make([]map[string]interface{}, len(r.Products))
	for i, p := range r.Products {
		products[i] = map[string]interface{}{
			"id":          p.ID,
			"name":        p.Name,
			"slug":        p.Slug,
			"description": p.Description,
			"attributes":  p.Attributes,
		}
	}

	facets := map[string]interface{}{}
	for k, vals := range r.Facets {
		items := make([]map[string]interface{}, len(vals))
		for i, fv := range vals {
			items[i] = map[string]interface{}{
				"value": fv.Value,
				"count": fv.Count,
			}
		}
		facets[k] = items
	}

	return map[string]interface{}{
		"products": products,
		"total":    r.Total,
		"facets":   facets,
	}
}
