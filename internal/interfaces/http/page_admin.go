package http

import (
	"encoding/json"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/cms"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

// PageAdminHandler serves page write endpoints.
type PageAdminHandler struct {
	pages cms.PageRepository
	bus   *event.Bus
}

// NewPageAdminHandler creates a PageAdminHandler.
func NewPageAdminHandler(pages cms.PageRepository, bus *event.Bus) *PageAdminHandler {
	return &PageAdminHandler{pages: pages, bus: bus}
}

type createPageRequest struct {
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type updatePageRequest struct {
	Slug     *string `json:"slug"`
	Title    *string `json:"title"`
	Content  *string `json:"content"`
	IsActive *bool   `json:"is_active"`
}

// adminPageResponse includes all fields for admin views.
type adminPageResponse struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func toAdminPageResponse(p *cms.Page) adminPageResponse {
	return adminPageResponse{
		ID:        p.ID(),
		Slug:      p.Slug(),
		Title:     p.Title(),
		Content:   p.Content(),
		IsActive:  p.IsActive(),
		CreatedAt: p.CreatedAt().Format("2006-01-02T15:04:05Z"),
		UpdatedAt: p.UpdatedAt().Format("2006-01-02T15:04:05Z"),
	}
}

// List handles GET /api/v1/admin/pages.
func (h *PageAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := parsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		pages, err := h.pages.List(r.Context(), offset, limit)
		if err != nil {
			JSONError(w, err)
			return
		}

		result := make([]adminPageResponse, 0, len(pages))
		for _, p := range pages {
			result = append(result, toAdminPageResponse(p))
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"pages": result,
		})
	}
}

// Create handles POST /api/v1/admin/pages.
func (h *PageAdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createPageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		page, err := cms.NewPage(id.New(), req.Slug, req.Title, req.Content)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}

		if err := h.pages.Create(r.Context(), page); err != nil {
			JSONError(w, err)
			return
		}

		_ = h.bus.Publish(r.Context(), event.New(cms.EventPageCreated, "page.admin", cms.PageCreatedData{
			PageID: page.ID(),
			Slug:   page.Slug(),
			Title:  page.Title(),
		}))

		JSON(w, http.StatusCreated, map[string]interface{}{
			"page": toAdminPageResponse(page),
		})
	}
}

// Update handles PUT /api/v1/admin/pages/{id}.
func (h *PageAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := r.PathValue("id")
		if pid == "" {
			JSONError(w, apperror.Validation("page id is required"))
			return
		}

		var req updatePageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		page, err := h.pages.FindByID(r.Context(), pid)
		if err != nil {
			JSONError(w, err)
			return
		}
		if page == nil {
			JSONError(w, apperror.NotFound("page not found"))
			return
		}

		if req.Slug != nil {
			if err := page.SetSlug(*req.Slug); err != nil {
				JSONError(w, apperror.Validation(err.Error()))
				return
			}
		}
		if req.Title != nil {
			if err := page.SetTitle(*req.Title); err != nil {
				JSONError(w, apperror.Validation(err.Error()))
				return
			}
		}
		if req.Content != nil {
			page.SetContent(*req.Content)
		}
		if req.IsActive != nil {
			page.SetActive(*req.IsActive)
		}

		if err := h.pages.Update(r.Context(), page); err != nil {
			JSONError(w, err)
			return
		}

		_ = h.bus.Publish(r.Context(), event.New(cms.EventPageUpdated, "page.admin", cms.PageUpdatedData{
			PageID: page.ID(),
			Slug:   page.Slug(),
			Title:  page.Title(),
		}))

		JSON(w, http.StatusOK, map[string]interface{}{
			"page": toAdminPageResponse(page),
		})
	}
}

// Delete handles DELETE /api/v1/admin/pages/{id}.
func (h *PageAdminHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := r.PathValue("id")
		if pid == "" {
			JSONError(w, apperror.Validation("page id is required"))
			return
		}

		if err := h.pages.Delete(r.Context(), pid); err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"deleted": true,
		})
	}
}
