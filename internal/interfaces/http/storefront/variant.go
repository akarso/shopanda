package storefront

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

// VariantHandler serves variant CRUD endpoints nested under a product.
type VariantHandler struct {
	products        catalog.ProductRepository
	variants        catalog.VariantRepository
	bus             *event.Bus
	visibilityFacts catalog.VisibilityFactsSource
	visibilityOpts  catalog.VisibilityOptions
	offers          variantOfferLoader
}

// variantOfferLoader attaches a sellable price and availability to variant reads.
type variantOfferLoader interface {
	VariantOffers(ctx context.Context, product *catalog.Product, variants []catalog.Variant, storeID, currency string) (map[string]composition.PublicOffer, error)
}

// NewVariantHandler creates a VariantHandler.
func NewVariantHandler(products catalog.ProductRepository, variants catalog.VariantRepository, bus *event.Bus) *VariantHandler {
	return &VariantHandler{products: products, variants: variants, bus: bus}
}

// WithVisibilityFacts enables four-axis visibility gating on public reads (PR-1057).
// Admin paths bypass via context.
func (h *VariantHandler) WithVisibilityFacts(facts catalog.VisibilityFactsSource) *VariantHandler {
	h.visibilityFacts = facts
	return h
}

// WithOffers attaches per-variant price and availability on public reads.
// Admin paths keep the catalog payload and do not resolve a storefront price.
func (h *VariantHandler) WithOffers(loader variantOfferLoader) *VariantHandler {
	h.offers = loader
	return h
}

// requireProduct verifies the parent product exists and returns it.
func (h *VariantHandler) requireProduct(w http.ResponseWriter, r *http.Request) *catalog.Product {
	pid := r.PathValue("id")
	if pid == "" {
		httpshared.JSONError(w, apperror.Validation("product id is required"))
		return nil
	}
	p, err := findProductByIDOrSlug(r.Context(), h.products, pid)
	if err != nil {
		httpshared.JSONError(w, err)
		return nil
	}
	if p == nil {
		httpshared.JSONError(w, apperror.NotFound("product not found"))
		return nil
	}
	if h.visibilityFacts != nil {
		ok, err := catalog.AllowWithFacts(r.Context(), p, catalog.VisibilitySurfaceIndividually, h.visibilityFacts, h.visibilityOpts)
		if err != nil {
			httpshared.JSONError(w, err)
			return nil
		}
		if !ok {
			httpshared.JSONError(w, apperror.NotFound("product not found"))
			return nil
		}
	}
	return p
}

const (
	variantDefaultLimit = 50
	variantMaxLimit     = 100
)

// List handles GET /api/v1/products/{id}/variants and
// GET /api/v1/admin/products/{id}/variants (admin path includes non-active products via read scope).
func (h *VariantHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := h.requireProduct(w, r)
		if p == nil {
			return
		}

		offset, limit, err := parseVariantPagination(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		variants, err := h.variants.ListByProductID(r.Context(), p.ID, offset, limit)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		body := map[string]interface{}{
			"variants": variants,
			"offset":   offset,
			"limit":    limit,
		}
		if h.attachPublicOffers(r) {
			offers, err := h.variantOffers(r, p, variants)
			if err != nil {
				httpshared.JSONError(w, err)
				return
			}
			body["offers"] = offers
		}
		httpshared.JSON(w, http.StatusOK, body)
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

// Get handles GET /api/v1/products/{id}/variants/{variantId} and
// GET /api/v1/admin/products/{id}/variants/{variantId}.
func (h *VariantHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := h.requireProduct(w, r)
		if p == nil {
			return
		}

		vid := r.PathValue("variantId")
		if vid == "" {
			httpshared.JSONError(w, apperror.Validation("variant id is required"))
			return
		}

		v, err := h.variants.FindByID(r.Context(), vid)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if v == nil || v.ProductID != p.ID {
			httpshared.JSONError(w, apperror.NotFound("variant not found"))
			return
		}

		body := map[string]interface{}{
			"variant": v,
		}
		if h.attachPublicOffers(r) {
			offers, err := h.variantOffers(r, p, []catalog.Variant{*v})
			if err != nil {
				httpshared.JSONError(w, err)
				return
			}
			if offer, ok := offers[v.ID]; ok {
				body["offer"] = offer
			}
		}
		httpshared.JSON(w, http.StatusOK, body)
	}
}

// attachPublicOffers reports whether this request should include sellable
// snapshots. Operator scope stays catalog-only.
func (h *VariantHandler) attachPublicOffers(r *http.Request) bool {
	return h != nil && h.offers != nil && publicCatalogOffer(r)
}

func (h *VariantHandler) variantOffers(r *http.Request, product *catalog.Product, variants []catalog.Variant) (map[string]composition.PublicOffer, error) {
	storeID, currency := requestStoreScope(r.Context())
	offers, err := h.offers.VariantOffers(r.Context(), product, variants, storeID, currency)
	if err != nil {
		return nil, err
	}
	if offers == nil {
		offers = map[string]composition.PublicOffer{}
	}
	return offers, nil
}

// createVariantRequest is the JSON body for creating a variant.
type createVariantRequest struct {
	SKU        string                 `json:"sku"`
	Name       string                 `json:"name"`
	Weight     *float64               `json:"weight"`
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
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		v, err := catalog.NewVariant(id.New(), p.ID, req.SKU)
		if err != nil {
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		v.Name = req.Name
		if req.Weight != nil {
			if math.IsNaN(*req.Weight) || math.IsInf(*req.Weight, 0) || *req.Weight < 0 {
				httpshared.JSONError(w, apperror.Validation("weight must be a finite non-negative number"))
				return
			}
			v.Weight = *req.Weight
		}
		if req.Attributes != nil {
			v.Attributes = req.Attributes
		}

		if err := h.variants.Create(r.Context(), &v); err != nil {
			httpshared.JSONError(w, err)
			return
		}

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventVariantCreated, "variant.handler", catalog.VariantCreatedData{
			VariantID: v.ID,
			ProductID: v.ProductID,
			SKU:       v.SKU,
		}))

		httpshared.JSON(w, http.StatusCreated, map[string]interface{}{
			"variant": v,
		})
	}
}

// updateVariantRequest is the JSON body for updating a variant.
type updateVariantRequest struct {
	SKU        *string                `json:"sku"`
	Name       *string                `json:"name"`
	Weight     *float64               `json:"weight"`
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
			httpshared.JSONError(w, apperror.Validation("variant id is required"))
			return
		}

		var req updateVariantRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		v, err := h.variants.FindByID(r.Context(), vid)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}
		if v == nil || v.ProductID != p.ID {
			httpshared.JSONError(w, apperror.NotFound("variant not found"))
			return
		}

		if req.SKU != nil {
			if *req.SKU == "" {
				httpshared.JSONError(w, apperror.Validation("sku must not be empty"))
				return
			}
			v.SKU = *req.SKU
		}
		if req.Name != nil {
			v.Name = *req.Name
		}
		if req.Weight != nil {
			if math.IsNaN(*req.Weight) || math.IsInf(*req.Weight, 0) || *req.Weight < 0 {
				httpshared.JSONError(w, apperror.Validation("weight must be a finite non-negative number"))
				return
			}
			v.Weight = *req.Weight
		}
		if req.Attributes != nil {
			v.Attributes = req.Attributes
		}

		if err := h.variants.Update(r.Context(), v); err != nil {
			httpshared.JSONError(w, err)
			return
		}

		_ = h.bus.Publish(r.Context(), event.New(catalog.EventVariantUpdated, "variant.handler", catalog.VariantUpdatedData{
			VariantID: v.ID,
			ProductID: v.ProductID,
			SKU:       v.SKU,
		}))

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"variant": v,
		})
	}
}
