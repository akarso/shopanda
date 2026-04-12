package http

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// PageHandler serves public page read endpoints.
type PageHandler struct {
	pages cms.PageRepository
}

// NewPageHandler creates a PageHandler.
func NewPageHandler(pages cms.PageRepository) *PageHandler {
	if pages == nil {
		panic("PageHandler: pages repository must not be nil")
	}
	return &PageHandler{pages: pages}
}

// pageResponse is the JSON shape for a public page.
type pageResponse struct {
	ID      string `json:"id"`
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Get handles GET /api/v1/pages/{slug}.
func (h *PageHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		if slug == "" {
			JSONError(w, apperror.Validation("page slug is required"))
			return
		}

		p, err := h.pages.FindBySlug(r.Context(), slug)
		if err != nil {
			JSONError(w, err)
			return
		}
		if p == nil {
			JSONError(w, apperror.NotFound("page not found"))
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"page": pageResponse{
				ID:      p.ID(),
				Slug:    p.Slug(),
				Title:   p.Title(),
				Content: p.Content(),
			},
		})
	}
}
