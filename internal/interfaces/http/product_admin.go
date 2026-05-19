package http

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ProductAdminHandler serves product write endpoints.
type ProductAdminHandler struct {
	repo    catalog.ProductRepository
	bus     *event.Bus
	auditor *admin.Auditor
	log     logger.Logger
}

type productCategoryAssignmentReader interface {
	ListCategoryIDsByProduct(ctx context.Context, productID string) ([]string, error)
}

// NewProductAdminHandler creates a ProductAdminHandler.
func NewProductAdminHandler(repo catalog.ProductRepository, bus *event.Bus) *ProductAdminHandler {
	log := logger.New("info")
	return NewProductAdminHandlerWithAuditor(repo, bus, admin.NewAuditor(log), log)
}

// NewProductAdminHandlerWithAuditor creates a ProductAdminHandler with a custom auditor.
func NewProductAdminHandlerWithAuditor(repo catalog.ProductRepository, bus *event.Bus, auditor *admin.Auditor, log logger.Logger) *ProductAdminHandler {
	if log == nil {
		log = logger.New("warn")
	}
	return &ProductAdminHandler{repo: repo, bus: bus, auditor: auditor, log: log}
}

func (h *ProductAdminHandler) getAdminID(r *http.Request) string {
	ac, err := admin.FromContext(r.Context())
	if err != nil || ac == nil || ac.AdminID == "" {
		id := auth.IdentityFrom(r.Context())
		if id.Role == identity.RoleAdmin {
			h.log.Warn("admin.context.missing", map[string]interface{}{
				"component":   "product_admin.getAdminID",
				"path":        r.URL.Path,
				"admin_id":    "system",
				"identity_id": id.UserID,
				"reason":      "admin context missing or empty",
			})
		}
		return "system"
	}
	return ac.AdminID
}

func productAdminScopeDetailsFromRequest(r *http.Request) map[string]interface{} {
	return fullAdminScopeDetailsFromRequest(r)
}

// List handles GET /api/v1/admin/products.
func (h *ProductAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := parsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		adminID := h.getAdminID(r)

		products, err := h.repo.List(r.Context(), offset, limit)
		if err != nil {
			details := productAdminScopeDetailsFromRequest(r)
			details["offset"] = offset
			details["limit"] = limit
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "products",
				Details:      details,
				Result:       "error",
				Error:        err.Error(),
			})
			JSONError(w, err)
			return
		}
		if products == nil {
			products = []catalog.Product{}
		}

		details := productAdminScopeDetailsFromRequest(r)
		details["offset"] = offset
		details["limit"] = limit
		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditProductRead,
			ResourceType: "products",
			Result:       "success",
			Details:      details,
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"products": products,
		})
	}
}

// Get handles GET /api/v1/admin/products/{id}.
func (h *ProductAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := r.PathValue("id")
		adminID := h.getAdminID(r)
		if pid == "" {
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				Result:       "error",
				Error:        "product id is required",
				Details:      details,
			})
			JSONError(w, apperror.Validation("product id is required"))
			return
		}

		product, err := h.repo.FindByID(r.Context(), pid)
		if err != nil {
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			JSONError(w, err)
			return
		}
		if product == nil {
			details := productAdminScopeDetailsFromRequest(r)
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductRead,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        "product not found",
				Details:      details,
			})
			JSONError(w, apperror.NotFound("product not found"))
			return
		}

		categoryIDs := []string{}
		if assignments, ok := h.repo.(productCategoryAssignmentReader); ok {
			categoryIDs, err = assignments.ListCategoryIDsByProduct(r.Context(), pid)
			if err != nil {
				details := productAdminScopeDetailsFromRequest(r)
				h.auditor.LogAction(r.Context(), admin.AuditEntry{
					AdminID:      adminID,
					Action:       admin.AuditProductRead,
					ResourceType: "product",
					ResourceID:   pid,
					Result:       "error",
					Error:        err.Error(),
					Details:      details,
				})
				JSONError(w, err)
				return
			}
			if categoryIDs == nil {
				categoryIDs = []string{}
			}
		}

		details := productAdminScopeDetailsFromRequest(r)
		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditProductRead,
			ResourceType: "product",
			ResourceID:   pid,
			Result:       "success",
			Details:      details,
		})

		JSON(w, http.StatusOK, map[string]interface{}{
			"product":      product,
			"category_ids": categoryIDs,
		})
	}
}

// createProductRequest is the JSON body for creating a product.
type createProductRequest struct {
	Name        string                 `json:"name"`
	Slug        string                 `json:"slug"`
	Description string                 `json:"description"`
	Attributes  map[string]interface{} `json:"attributes"`
}

// updateProductRequest is the JSON body for updating a product.
type updateProductRequest struct {
	Name        *string                `json:"name"`
	Slug        *string                `json:"slug"`
	Description *string                `json:"description"`
	Status      *string                `json:"status"`
	Attributes  map[string]interface{} `json:"attributes"`
}

// Create handles POST /api/v1/admin/products.
func (h *ProductAdminHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminID := h.getAdminID(r)
		details := productAdminScopeDetailsFromRequest(r)

		var req createProductRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        "invalid request body",
				Details:      details,
			})
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		product, err := catalog.NewProduct(id.New(), req.Name, req.Slug)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			JSONError(w, apperror.Validation(err.Error()))
			return
		}
		product.Description = req.Description
		if req.Attributes != nil {
			product.Attributes = req.Attributes
		}

		if err := h.repo.Create(r.Context(), &product); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductCreate,
				ResourceType: "product",
				ResourceID:   product.ID,
				Result:       "error",
				Error:        err.Error(),
				Details:      details,
			})
			JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditProductCreate,
			ResourceType: "product",
			ResourceID:   product.ID,
			Result:       "success",
			Details:      details,
		})

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventProductCreated, "product.admin", catalog.ProductCreatedData{
			ProductID: product.ID,
			Name:      product.Name,
			Slug:      product.Slug,
			Status:    product.Status,
		}))

		JSON(w, http.StatusCreated, map[string]interface{}{
			"product": product,
		})
	}
}

// Update handles PUT /api/v1/admin/products/{id}.
func (h *ProductAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := r.PathValue("id")
		adminID := h.getAdminID(r)
		if pid == "" {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				Result:       "error",
				Error:        "product id is required",
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			JSONError(w, apperror.Validation("product id is required"))
			return
		}

		var req updateProductRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        "invalid request body",
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		product, err := h.repo.FindByID(r.Context(), pid)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			JSONError(w, err)
			return
		}
		if product == nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        "product not found",
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			JSONError(w, apperror.NotFound("product not found"))
			return
		}

		if req.Name != nil {
			if *req.Name == "" {
				h.auditor.LogAction(r.Context(), admin.AuditEntry{
					AdminID:      adminID,
					Action:       admin.AuditProductUpdate,
					ResourceType: "product",
					ResourceID:   pid,
					Result:       "error",
					Error:        "name must not be empty",
					Details:      productAdminScopeDetailsFromRequest(r),
				})
				JSONError(w, apperror.Validation("name must not be empty"))
				return
			}
			product.Name = *req.Name
		}
		if req.Slug != nil {
			if *req.Slug == "" {
				h.auditor.LogAction(r.Context(), admin.AuditEntry{
					AdminID:      adminID,
					Action:       admin.AuditProductUpdate,
					ResourceType: "product",
					ResourceID:   pid,
					Result:       "error",
					Error:        "slug must not be empty",
					Details:      productAdminScopeDetailsFromRequest(r),
				})
				JSONError(w, apperror.Validation("slug must not be empty"))
				return
			}
			product.Slug = *req.Slug
		}
		if req.Description != nil {
			product.Description = *req.Description
		}
		if req.Status != nil {
			s := catalog.Status(*req.Status)
			if !s.IsValid() {
				h.auditor.LogAction(r.Context(), admin.AuditEntry{
					AdminID:      adminID,
					Action:       admin.AuditProductUpdate,
					ResourceType: "product",
					ResourceID:   pid,
					Result:       "error",
					Error:        "invalid status",
					Details:      productAdminScopeDetailsFromRequest(r),
				})
				JSONError(w, apperror.Validation("invalid status"))
				return
			}
			product.Status = s
		}
		if req.Attributes != nil {
			product.Attributes = req.Attributes
		}

		if err := h.repo.Update(r.Context(), product); err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditProductUpdate,
				ResourceType: "product",
				ResourceID:   pid,
				Result:       "error",
				Error:        err.Error(),
				Details:      productAdminScopeDetailsFromRequest(r),
			})
			JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditProductUpdate,
			ResourceType: "product",
			ResourceID:   pid,
			Result:       "success",
			Details:      productAdminScopeDetailsFromRequest(r),
		})

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventProductUpdated, "product.admin", catalog.ProductUpdatedData{
			ProductID: product.ID,
			Name:      product.Name,
			Slug:      product.Slug,
			Status:    product.Status,
		}))

		JSON(w, http.StatusOK, map[string]interface{}{
			"product": product,
		})
	}
}
