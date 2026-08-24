package admin

import (
	"net/http"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// CategoryProductAssignmentAdminHandler manages product-category assignment writes.
type CategoryProductAssignmentAdminHandler struct {
	categories  catalog.CategoryRepository
	products    catalog.ProductRepository
	assignments catalog.ProductCategoryAssignmentRepository
	auditor     *admin.Auditor
}

// NewCategoryProductAssignmentAdminHandler creates a CategoryProductAssignmentAdminHandler with a default auditor.
func NewCategoryProductAssignmentAdminHandler(
	categories catalog.CategoryRepository,
	products catalog.ProductRepository,
	assignments catalog.ProductCategoryAssignmentRepository,
) *CategoryProductAssignmentAdminHandler {
	return NewCategoryProductAssignmentAdminHandlerWithAuditor(categories, products, assignments, admin.NewAuditor(logger.New("info")))
}

// NewCategoryProductAssignmentAdminHandlerWithAuditor creates a CategoryProductAssignmentAdminHandler with a custom auditor.
func NewCategoryProductAssignmentAdminHandlerWithAuditor(
	categories catalog.CategoryRepository,
	products catalog.ProductRepository,
	assignments catalog.ProductCategoryAssignmentRepository,
	auditor *admin.Auditor,
) *CategoryProductAssignmentAdminHandler {
	if categories == nil {
		panic("CategoryProductAssignmentAdminHandler: categories repository must not be nil")
	}
	if products == nil {
		panic("CategoryProductAssignmentAdminHandler: products repository must not be nil")
	}
	if assignments == nil {
		panic("CategoryProductAssignmentAdminHandler: assignments repository must not be nil")
	}
	if auditor == nil {
		panic("CategoryProductAssignmentAdminHandler: auditor must not be nil")
	}
	return &CategoryProductAssignmentAdminHandler{
		categories:  categories,
		products:    products,
		assignments: assignments,
		auditor:     auditor,
	}
}

func (h *CategoryProductAssignmentAdminHandler) audit(r *http.Request, action admin.AuditAction, categoryID, productID string, err error) {
	details := mergeAuditDetails(map[string]interface{}{"product_id": productID}, fullAdminScopeDetailsFromRequest(r))
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
		ResourceID:   categoryID,
		Details:      details,
		Result:       result,
		Error:        errMsg,
	})
}

func (h *CategoryProductAssignmentAdminHandler) validateAssignmentTargets(r *http.Request) (string, string, error) {
	categoryID := r.PathValue("id")
	if categoryID == "" {
		return "", "", apperror.Validation("category id is required")
	}
	productID := r.PathValue("productId")
	if productID == "" {
		return "", "", apperror.Validation("product id is required")
	}
	category, err := h.categories.FindByID(r.Context(), categoryID)
	if err != nil {
		return "", "", err
	}
	if category == nil {
		return "", "", apperror.NotFound("category not found")
	}
	product, err := h.products.FindByID(r.Context(), productID)
	if err != nil {
		return "", "", err
	}
	if product == nil {
		return "", "", apperror.NotFound("product not found")
	}
	return categoryID, productID, nil
}

// Assign handles POST /api/v1/admin/categories/{id}/products/{productId}.
func (h *CategoryProductAssignmentAdminHandler) Assign() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		categoryID, productID, err := h.validateAssignmentTargets(r)
		if err != nil {
			h.audit(r, admin.AuditCategoryProductAssign, r.PathValue("id"), r.PathValue("productId"), err)
			httpshared.JSONError(w, err)
			return
		}
		if err := h.assignments.AssignCategory(r.Context(), productID, categoryID); err != nil {
			h.audit(r, admin.AuditCategoryProductAssign, categoryID, productID, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditCategoryProductAssign, categoryID, productID, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"assigned": true})
	}
}

// Unassign handles DELETE /api/v1/admin/categories/{id}/products/{productId}.
func (h *CategoryProductAssignmentAdminHandler) Unassign() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		categoryID, productID, err := h.validateAssignmentTargets(r)
		if err != nil {
			h.audit(r, admin.AuditCategoryProductUnassign, r.PathValue("id"), r.PathValue("productId"), err)
			httpshared.JSONError(w, err)
			return
		}
		if err := h.assignments.RemoveCategory(r.Context(), productID, categoryID); err != nil {
			h.audit(r, admin.AuditCategoryProductUnassign, categoryID, productID, err)
			httpshared.JSONError(w, err)
			return
		}
		h.audit(r, admin.AuditCategoryProductUnassign, categoryID, productID, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{"assigned": false})
	}
}
