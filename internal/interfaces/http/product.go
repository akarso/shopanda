package http

import (
	"net/http"
	"strconv"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// ProductHandler serves product read endpoints.
type ProductHandler struct {
	repo catalog.ProductRepository
	pdp  *composition.Pipeline[composition.ProductContext]
	plp  *composition.Pipeline[composition.ListingContext]
}

// NewProductHandler creates a ProductHandler with the given dependencies.
func NewProductHandler(
	repo catalog.ProductRepository,
	pdp *composition.Pipeline[composition.ProductContext],
	plp *composition.Pipeline[composition.ListingContext],
) *ProductHandler {
	return &ProductHandler{repo: repo, pdp: pdp, plp: plp}
}

// List handles GET /api/v1/products.
func (h *ProductHandler) List() http.HandlerFunc {
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

		ptrs := make([]*catalog.Product, len(products))
		for i := range products {
			ptrs[i] = &products[i]
		}

		ctx := composition.NewListingContext(ptrs)
		ctx.Ctx = r.Context()
		if err := h.plp.Execute(ctx); err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "composition failed", err))
			return
		}

		JSON(w, http.StatusOK, listingResponse(ctx))
	}
}

// Get handles GET /api/v1/products/{id}.
func (h *ProductHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			JSONError(w, apperror.Validation("product id is required"))
			return
		}

		product, err := h.repo.FindByID(r.Context(), id)
		if err != nil {
			JSONError(w, err)
			return
		}
		if product == nil {
			JSONError(w, apperror.NotFound("product not found"))
			return
		}

		ctx := composition.NewProductContext(product)
		ctx.Ctx = r.Context()
		if s := store.FromContext(r.Context()); s != nil {
			ctx.StoreID = s.ID
			if ctx.Currency == "" {
				ctx.Currency = s.Currency
			}
			if ctx.Country == "" {
				ctx.Country = s.Country
			}
		}
		if err := h.pdp.Execute(ctx); err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "composition failed", err))
			return
		}

		JSON(w, http.StatusOK, productResponse(ctx))
	}
}

const (
	defaultLimit = 20
	maxLimit     = 100
)

// parsePagination extracts offset and limit query parameters.
func parsePagination(r *http.Request) (int, int, error) {
	offset := 0
	limit := defaultLimit

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
		if n > maxLimit {
			n = maxLimit
		}
		limit = n
	}

	return offset, limit, nil
}

// productResponse builds the response body for a single product.
func productResponse(ctx *composition.ProductContext) map[string]interface{} {
	return map[string]interface{}{
		"product": ctx.Product,
		"blocks":  ctx.Blocks,
		"meta":    ctx.Meta,
	}
}

// listingResponse builds the response body for a product listing.
func listingResponse(ctx *composition.ListingContext) map[string]interface{} {
	return map[string]interface{}{
		"products": ctx.Products,
		"blocks":   ctx.Blocks,
		"filters":  ctx.Filters,
		"meta":     ctx.Meta,
	}
}
