package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

// CategoryAdminHandler serves category write endpoints.
type CategoryAdminHandler struct {
	categories catalog.CategoryRepository
	bus        *event.Bus
}

// NewCategoryAdminHandler creates a CategoryAdminHandler.
func NewCategoryAdminHandler(categories catalog.CategoryRepository, bus *event.Bus) *CategoryAdminHandler {
	if categories == nil {
		panic("CategoryAdminHandler: categories repository must not be nil")
	}
	if bus == nil {
		panic("CategoryAdminHandler: event bus must not be nil")
	}
	return &CategoryAdminHandler{categories: categories, bus: bus}
}

type createCategoryRequest struct {
	ParentID *string                `json:"parent_id"`
	Name     string                 `json:"name"`
	Slug     string                 `json:"slug"`
	Position *int                   `json:"position"`
	Meta     map[string]interface{} `json:"meta"`
}

type updateCategoryRequest struct {
	ParentID *string                `json:"parent_id"`
	Name     *string                `json:"name"`
	Slug     *string                `json:"slug"`
	Position *int                   `json:"position"`
	Meta     map[string]interface{} `json:"meta"`
}

func normalizeCategoryParentID(parentID *string) *string {
	if parentID == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*parentID)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (h *CategoryAdminHandler) validateParent(r *http.Request, categoryID string, parentID *string) error {
	if parentID == nil {
		return nil
	}
	if categoryID != "" && *parentID == categoryID {
		return apperror.Validation("category parent must not reference itself")
	}
	currentID := *parentID
	visited := map[string]struct{}{}
	for currentID != "" {
		if _, seen := visited[currentID]; seen {
			break
		}
		visited[currentID] = struct{}{}
		if categoryID != "" && currentID == categoryID {
			return apperror.Validation("category parent would create a cycle")
		}
		parent, err := h.categories.FindByID(r.Context(), currentID)
		if err != nil {
			return err
		}
		if parent == nil {
			return apperror.Validation("parent category not found")
		}
		if parent.ParentID == nil {
			break
		}
		currentID = *parent.ParentID
	}
	return nil
}

// Create handles POST /api/v1/admin/categories.
func (h *CategoryAdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createCategoryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		parentID := normalizeCategoryParentID(req.ParentID)
		if err := h.validateParent(r, "", parentID); err != nil {
			JSONError(w, err)
			return
		}

		category, err := catalog.NewCategory(id.New(), req.Name, req.Slug)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}
		category.ParentID = parentID
		if req.Position != nil {
			category.Position = *req.Position
		}
		if req.Meta != nil {
			category.Meta = req.Meta
		}

		if err := h.categories.Create(r.Context(), &category); err != nil {
			JSONError(w, err)
			return
		}

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventCategoryCreated, "category.admin", catalog.CategoryCreatedData{
			CategoryID: category.ID,
			Name:       category.Name,
			Slug:       category.Slug,
		}))

		JSON(w, http.StatusCreated, map[string]interface{}{
			"category": category,
		})
	}
}

// Update handles PUT /api/v1/admin/categories/{id}.
func (h *CategoryAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		categoryID := r.PathValue("id")
		if categoryID == "" {
			JSONError(w, apperror.Validation("category id is required"))
			return
		}

		var req updateCategoryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		category, err := h.categories.FindByID(r.Context(), categoryID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if category == nil {
			JSONError(w, apperror.NotFound("category not found"))
			return
		}

		oldSlug := category.Slug
		if req.ParentID != nil {
			parentID := normalizeCategoryParentID(req.ParentID)
			if err := h.validateParent(r, categoryID, parentID); err != nil {
				JSONError(w, err)
				return
			}
			category.ParentID = parentID
		}
		if req.Name != nil {
			trimmed := strings.TrimSpace(*req.Name)
			if trimmed == "" {
				JSONError(w, apperror.Validation("category name must not be empty"))
				return
			}
			category.Name = trimmed
		}
		if req.Slug != nil {
			trimmed := strings.TrimSpace(*req.Slug)
			if trimmed == "" {
				JSONError(w, apperror.Validation("category slug must not be empty"))
				return
			}
			category.Slug = trimmed
		}
		if req.Position != nil {
			category.Position = *req.Position
		}
		if req.Meta != nil {
			category.Meta = req.Meta
		}

		if err := h.categories.Update(r.Context(), category); err != nil {
			JSONError(w, err)
			return
		}

		var publishedOldSlug string
		if oldSlug != category.Slug {
			publishedOldSlug = oldSlug
		}

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventCategoryUpdated, "category.admin", catalog.CategoryUpdatedData{
			CategoryID: category.ID,
			Name:       category.Name,
			Slug:       category.Slug,
			OldSlug:    publishedOldSlug,
		}))

		JSON(w, http.StatusOK, map[string]interface{}{
			"category": category,
		})
	}
}

// Delete handles DELETE /api/v1/admin/categories/{id}.
func (h *CategoryAdminHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		categoryID := r.PathValue("id")
		if categoryID == "" {
			JSONError(w, apperror.Validation("category id is required"))
			return
		}

		category, err := h.categories.FindByID(r.Context(), categoryID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if category == nil {
			JSONError(w, apperror.NotFound("category not found"))
			return
		}

		if err := h.categories.Delete(r.Context(), categoryID); err != nil {
			JSONError(w, err)
			return
		}

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventCategoryDeleted, "category.admin", catalog.CategoryDeletedData{
			CategoryID: category.ID,
			Slug:       category.Slug,
		}))

		JSON(w, http.StatusOK, map[string]interface{}{
			"deleted": true,
		})
	}
}
