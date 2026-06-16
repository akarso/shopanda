package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// CategoryAdminHandler serves category write endpoints.
type CategoryAdminHandler struct {
	categories catalog.CategoryRepository
	bus        *event.Bus
	auditor    *admin.Auditor
}

// NewCategoryAdminHandler creates a CategoryAdminHandler with a default auditor.
func NewCategoryAdminHandler(categories catalog.CategoryRepository, bus *event.Bus) *CategoryAdminHandler {
	return NewCategoryAdminHandlerWithAuditor(categories, bus, admin.NewAuditor(logger.New("info")))
}

// NewCategoryAdminHandlerWithAuditor creates a CategoryAdminHandler with a custom auditor.
func NewCategoryAdminHandlerWithAuditor(categories catalog.CategoryRepository, bus *event.Bus, auditor *admin.Auditor) *CategoryAdminHandler {
	if categories == nil {
		panic("CategoryAdminHandler: categories repository must not be nil")
	}
	if bus == nil {
		panic("CategoryAdminHandler: event bus must not be nil")
	}
	if auditor == nil {
		panic("CategoryAdminHandler: auditor must not be nil")
	}
	return &CategoryAdminHandler{categories: categories, bus: bus, auditor: auditor}
}

func (h *CategoryAdminHandler) audit(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
	merged := mergeAuditDetails(details, fullAdminScopeDetailsFromRequest(r))
	result := "success"
	errMsg := ""
	if err != nil {
		result = "error"
		errMsg = err.Error()
	}
	h.auditor.LogAction(r.Context(), admin.AuditEntry{
		AdminID:      adminIDFromRequest(r),
		Action:       action,
		ResourceType: "category",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
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
			return apperror.Validation(fmt.Sprintf("invalid parent chain: cycle detected at ID %s", currentID))
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
			verr := apperror.Validation("invalid request body")
			h.audit(r, admin.AuditCategoryCreate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		parentID := normalizeCategoryParentID(req.ParentID)
		if err := h.validateParent(r, "", parentID); err != nil {
			h.audit(r, admin.AuditCategoryCreate, "", nil, err)
			JSONError(w, err)
			return
		}

		category, err := catalog.NewCategory(id.New(), req.Name, req.Slug)
		if err != nil {
			verr := apperror.Validation(err.Error())
			h.audit(r, admin.AuditCategoryCreate, "", nil, verr)
			JSONError(w, verr)
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
			h.audit(r, admin.AuditCategoryCreate, category.ID, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditCategoryCreate, category.ID, map[string]interface{}{"slug": category.Slug}, nil)

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
			verr := apperror.Validation("category id is required")
			h.audit(r, admin.AuditCategoryUpdate, "", nil, verr)
			JSONError(w, verr)
			return
		}

		var req updateCategoryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			verr := apperror.Validation("invalid request body")
			h.audit(r, admin.AuditCategoryUpdate, categoryID, nil, verr)
			JSONError(w, verr)
			return
		}

		category, err := h.categories.FindByID(r.Context(), categoryID)
		if err != nil {
			h.audit(r, admin.AuditCategoryUpdate, categoryID, nil, err)
			JSONError(w, err)
			return
		}
		if category == nil {
			nf := apperror.NotFound("category not found")
			h.audit(r, admin.AuditCategoryUpdate, categoryID, nil, nf)
			JSONError(w, nf)
			return
		}

		oldSlug := category.Slug
		if req.ParentID != nil {
			parentID := normalizeCategoryParentID(req.ParentID)
			if err := h.validateParent(r, categoryID, parentID); err != nil {
				h.audit(r, admin.AuditCategoryUpdate, categoryID, nil, err)
				JSONError(w, err)
				return
			}
			category.ParentID = parentID
		}
		if req.Name != nil {
			trimmed := strings.TrimSpace(*req.Name)
			if trimmed == "" {
				verr := apperror.Validation("category name must not be empty")
				h.audit(r, admin.AuditCategoryUpdate, categoryID, nil, verr)
				JSONError(w, verr)
				return
			}
			category.Name = trimmed
		}
		if req.Slug != nil {
			trimmed := strings.TrimSpace(*req.Slug)
			if trimmed == "" {
				verr := apperror.Validation("category slug must not be empty")
				h.audit(r, admin.AuditCategoryUpdate, categoryID, nil, verr)
				JSONError(w, verr)
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
			h.audit(r, admin.AuditCategoryUpdate, categoryID, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditCategoryUpdate, categoryID, map[string]interface{}{"slug": category.Slug}, nil)

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
			verr := apperror.Validation("category id is required")
			h.audit(r, admin.AuditCategoryDelete, "", nil, verr)
			JSONError(w, verr)
			return
		}

		category, err := h.categories.FindByID(r.Context(), categoryID)
		if err != nil {
			h.audit(r, admin.AuditCategoryDelete, categoryID, nil, err)
			JSONError(w, err)
			return
		}
		if category == nil {
			nf := apperror.NotFound("category not found")
			h.audit(r, admin.AuditCategoryDelete, categoryID, nil, nf)
			JSONError(w, nf)
			return
		}

		if err := h.categories.Delete(r.Context(), categoryID); err != nil {
			h.audit(r, admin.AuditCategoryDelete, categoryID, nil, err)
			JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditCategoryDelete, categoryID, map[string]interface{}{"slug": category.Slug}, nil)

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventCategoryDeleted, "category.admin", catalog.CategoryDeletedData{
			CategoryID: category.ID,
			Slug:       category.Slug,
		}))

		JSON(w, http.StatusOK, map[string]interface{}{
			"deleted": true,
		})
	}
}
