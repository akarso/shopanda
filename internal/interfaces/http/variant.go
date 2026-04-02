package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

// VariantHandler serves variant CRUD endpoints nested under a product.
type VariantHandler struct {
	products catalog.ProductRepository
	variants catalog.VariantRepository
	bus      *event.Bus
}

// NewVariantHandler creates a VariantHandler.
func NewVariantHandler(products catalog.ProductRepository, variants catalog.VariantRepository, bus *event.Bus) *VariantHandler {
	return &VariantHandler{products: products, variants: variants, bus: bus}
}

// requireProduct verifies the parent product exists and returns it.
func (h *VariantHandler) requireProduct(w http.ResponseWriter, r *http.Request) *catalog.Product {
	pid := r.PathValue("id")
	if pid == "" {
		JSONError(w, apperror.Validation("product id is required"))
		return nil
	}
	p, err := h.products.FindByID(r.Context(), pid)
	if err != nil {
		JSONError(w, err)
		return nil
	}
	if p == nil {
		JSONError(w, apperror.NotFound("product not found"))
		return nil
	}
	return p
}

const (
	variantDefaultLimit = 50
	variantMaxLimit     = 100
)

// List handles GET /api/v1/products/{id}/variants.
func (h *VariantHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := h.requireProduct(w, r)
		if p == nil {
			return
		}

		offset, limit, err := parseVariantPagination(r)
		if err != nil {
			JSONError(w, err)
			return
		}

		variants, err := h.variants.ListByProductID(r.Context(), p.ID, offset, limit)
		if err != nil {
			JSONError(w, err)
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"variants": variants,
			"offset":   offset,
			"limit":    limit,
		})
	}
}

// parseVariantPagination extracts offset and limit query parameters for variant listing.
func parseVariantPagination(r *http.Request) (int, int, error) {
	offset := 0
	limit := variantDefaultLimit

	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0, 0, apperror.Validation("offset must be a non-negative integer")
		}
		offset = n
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return 0, 0, apperror.Validation("limit must be a positive integer")
		}
		if n > variantMaxLimit {
			n = variantMaxLimit
		}
		limit = n
	}

	return offset, limit, nil
}

// Get handles GET /api/v1/products/{id}/variants/{variantId}.
func (h *VariantHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := h.requireProduct(w, r)
		if p == nil {
			return
		}

		vid := r.PathValue("variantId")
		if vid == "" {
			JSONError(w, apperror.Validation("variant id is required"))
			return
		}

		v, err := h.variants.FindByID(r.Context(), vid)
		if err != nil {
			JSONError(w, err)
			return
		}
		if v == nil || v.ProductID != p.ID {
			JSONError(w, apperror.NotFound("variant not found"))
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"variant": v,
		})
	}
}

// createVariantRequest is the JSON body for creating a variant.
type createVariantRequest struct {
	SKU        string                 `json:"sku"`
	Name       string                 `json:"name"`
	Attributes map[string]interface{} `json:"attributes"`
}

// Create handles POST /api/v1/admin/products/{id}/variants.
func (h *VariantHandler) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := h.requireProduct(w, r)
		if p == nil {
			return
		}

		var req createVariantRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		v, err := catalog.NewVariant(id.New(), p.ID, req.SKU)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}
		v.Name = req.Name
		if req.Attributes != nil {
			v.Attributes = req.Attributes
		}

		if err := h.variants.Create(r.Context(), &v); err != nil {
			JSONError(w, err)
			return
		}

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventVariantCreated, "variant.handler", catalog.VariantCreatedData{
			VariantID: v.ID,
			ProductID: v.ProductID,
			SKU:       v.SKU,
		}))

		JSON(w, http.StatusCreated, map[string]interface{}{
			"variant": v,
		})
	}
}

// updateVariantRequest is the JSON body for updating a variant.
type updateVariantRequest struct {
	SKU        *string                `json:"sku"`
	Name       *string                `json:"name"`
	Attributes map[string]interface{} `json:"attributes"`
}

// Update handles PUT /api/v1/admin/products/{id}/variants/{variantId}.
func (h *VariantHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := h.requireProduct(w, r)
		if p == nil {
			return
		}

		vid := r.PathValue("variantId")
		if vid == "" {
			JSONError(w, apperror.Validation("variant id is required"))
			return
		}

		var req updateVariantRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		v, err := h.variants.FindByID(r.Context(), vid)
		if err != nil {
			JSONError(w, err)
			return
		}
		if v == nil || v.ProductID != p.ID {
			JSONError(w, apperror.NotFound("variant not found"))
			return
		}

		if req.SKU != nil {
			if *req.SKU == "" {
				JSONError(w, apperror.Validation("sku must not be empty"))
				return
			}
			v.SKU = *req.SKU
		}
		if req.Name != nil {
			v.Name = *req.Name
		}
		if req.Attributes != nil {
			v.Attributes = req.Attributes
		}

		if err := h.variants.Update(r.Context(), v); err != nil {
			JSONError(w, err)
			return
		}

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventVariantUpdated, "variant.handler", catalog.VariantUpdatedData{
			VariantID: v.ID,
			ProductID: v.ProductID,
			SKU:       v.SKU,
		}))

		JSON(w, http.StatusOK, map[string]interface{}{
			"variant": v,
		})
	}
}
