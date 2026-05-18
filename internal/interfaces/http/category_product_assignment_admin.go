package http

import (
	"net/http"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// CategoryProductAssignmentAdminHandler manages product-category assignment writes.
type CategoryProductAssignmentAdminHandler struct {
	categories  catalog.CategoryRepository
	products    catalog.ProductRepository
	assignments catalog.ProductCategoryAssignmentRepository
}

// NewCategoryProductAssignmentAdminHandler creates a CategoryProductAssignmentAdminHandler.
func NewCategoryProductAssignmentAdminHandler(
	categories catalog.CategoryRepository,
	products catalog.ProductRepository,
	assignments catalog.ProductCategoryAssignmentRepository,
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
	return &CategoryProductAssignmentAdminHandler{
		categories:  categories,
		products:    products,
		assignments: assignments,
	}
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
			JSONError(w, err)
			return
		}
		if err := h.assignments.AssignCategory(r.Context(), productID, categoryID); err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{"assigned": true})
	}
}

// Unassign handles DELETE /api/v1/admin/categories/{id}/products/{productId}.
func (h *CategoryProductAssignmentAdminHandler) Unassign() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		categoryID, productID, err := h.validateAssignmentTargets(r)
		if err != nil {
			JSONError(w, err)
			return
		}
		if err := h.assignments.RemoveCategory(r.Context(), productID, categoryID); err != nil {
			JSONError(w, err)
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{"assigned": false})
	}
}
