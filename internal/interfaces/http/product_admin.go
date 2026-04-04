package http

import (
	"encoding/json"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

// ProductAdminHandler serves product write endpoints.
type ProductAdminHandler struct {
	repo catalog.ProductRepository
	bus  *event.Bus
}

// NewProductAdminHandler creates a ProductAdminHandler.
func NewProductAdminHandler(repo catalog.ProductRepository, bus *event.Bus) *ProductAdminHandler {
	return &ProductAdminHandler{repo: repo, bus: bus}
}

// List handles GET /api/v1/admin/products.
func (h *ProductAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := parsePagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		products, err := h.repo.List(r.Context(), offset, limit)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"products": products,
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
		var req createProductRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		product, err := catalog.NewProduct(id.New(), req.Name, req.Slug)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}
		product.Description = req.Description
		if req.Attributes != nil {
			product.Attributes = req.Attributes
		}

		if err := h.repo.Create(r.Context(), &product); err != nil {
			JSONError(w, err)
			return
		}

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
		if pid == "" {
			JSONError(w, apperror.Validation("product id is required"))
			return
		}

		var req updateProductRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		product, err := h.repo.FindByID(r.Context(), pid)
		if err != nil {
			JSONError(w, err)
			return
		}
		if product == nil {
			JSONError(w, apperror.NotFound("product not found"))
			return
		}

		if req.Name != nil {
			if *req.Name == "" {
				JSONError(w, apperror.Validation("name must not be empty"))
				return
			}
			product.Name = *req.Name
		}
		if req.Slug != nil {
			if *req.Slug == "" {
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
				JSONError(w, apperror.Validation("invalid status"))
				return
			}
			product.Status = s
		}
		if req.Attributes != nil {
			product.Attributes = req.Attributes
		}

		if err := h.repo.Update(r.Context(), product); err != nil {
			JSONError(w, err)
			return
		}

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
