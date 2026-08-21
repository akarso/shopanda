package admin

import (
	"encoding/json"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ProductPriceAdminHandler serves store-scoped variant price read/write
// endpoints. The store and currency come from the active admin scope context;
// an empty store means the global/default price.
type ProductPriceAdminHandler struct {
	products catalog.ProductRepository
	variants catalog.VariantRepository
	prices   pricing.PriceRepository
	auditor  *admin.Auditor
	log      logger.Logger
}

// NewProductPriceAdminHandler creates a ProductPriceAdminHandler.
func NewProductPriceAdminHandler(products catalog.ProductRepository, variants catalog.VariantRepository, prices pricing.PriceRepository, auditor *admin.Auditor, log logger.Logger) *ProductPriceAdminHandler {
	if products == nil {
		panic("http: product price handler: product repository must not be nil")
	}
	if variants == nil {
		panic("http: product price handler: variant repository must not be nil")
	}
	if prices == nil {
		panic("http: product price handler: price repository must not be nil")
	}
	if auditor == nil {
		panic("http: product price handler: auditor must not be nil")
	}
	if log == nil {
		log = logger.New("warn")
	}
	return &ProductPriceAdminHandler{products: products, variants: variants, prices: prices, auditor: auditor, log: log}
}

type updateProductPriceRequest struct {
	Amount *int64 `json:"amount"`
}

// resolveVariant verifies the parent product and the variant belong together.
// It writes an audit entry and JSON error and returns nil on any failure.
func (h *ProductPriceAdminHandler) resolveVariant(w http.ResponseWriter, r *http.Request, action admin.AuditAction, pid, vid string) *catalog.Variant {
	product, err := h.products.FindByID(r.Context(), pid)
	if err != nil {
		h.audit(r, action, vid, nil, err)
		httpshared.JSONError(w, err)
		return nil
	}
	if product == nil {
		h.audit(r, action, vid, nil, apperror.NotFound("product not found"))
		httpshared.JSONError(w, apperror.NotFound("product not found"))
		return nil
	}
	variant, err := h.variants.FindByID(r.Context(), vid)
	if err != nil {
		h.audit(r, action, vid, nil, err)
		httpshared.JSONError(w, err)
		return nil
	}
	if variant == nil || variant.ProductID != pid {
		h.audit(r, action, vid, nil, apperror.NotFound("variant not found"))
		httpshared.JSONError(w, apperror.NotFound("variant not found"))
		return nil
	}
	return variant
}

// Get handles GET /api/v1/admin/products/{id}/variants/{variantId}/price.
func (h *ProductPriceAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := strings.TrimSpace(r.PathValue("id"))
		vid := strings.TrimSpace(r.PathValue("variantId"))
		currency := ResolveCurrencyScopeID(r)
		storeID := ResolveStoreScopeID(r)
		if pid == "" || vid == "" {
			h.audit(r, admin.AuditPriceRead, vid, nil, apperror.Validation("product id and variant id are required"))
			httpshared.JSONError(w, apperror.Validation("product id and variant id are required"))
			return
		}
		if currency == "" {
			h.audit(r, admin.AuditPriceRead, vid, nil, apperror.Validation("currency context is required"))
			httpshared.JSONError(w, apperror.Validation("currency context is required"))
			return
		}

		variant := h.resolveVariant(w, r, admin.AuditPriceRead, pid, vid)
		if variant == nil {
			return
		}

		price, err := h.prices.FindByVariantCurrencyAndStore(r.Context(), vid, currency, storeID)
		if err != nil {
			h.audit(r, admin.AuditPriceRead, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}

		var globalFallback map[string]interface{}
		priceScope := "unset"
		if price != nil {
			if price.StoreID == "" {
				priceScope = "global"
			} else {
				priceScope = "store"
			}
		} else if storeID != "" {
			globalPrice, err := h.prices.FindByVariantCurrencyAndStore(r.Context(), vid, currency, "")
			if err != nil {
				h.audit(r, admin.AuditPriceRead, vid, nil, err)
				httpshared.JSONError(w, err)
				return
			}
			globalFallback = pricePayload(globalPrice)
		}

		h.audit(r, admin.AuditPriceRead, vid, map[string]interface{}{"found": price != nil}, nil)

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"price":           pricePayload(price),
			"global_fallback": globalFallback,
			"price_scope":     priceScope,
			"currency":        currency,
			"scope":           scopePayloadFromRequest(r),
		})
	}
}

// Update handles PUT /api/v1/admin/products/{id}/variants/{variantId}/price.
func (h *ProductPriceAdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := strings.TrimSpace(r.PathValue("id"))
		vid := strings.TrimSpace(r.PathValue("variantId"))
		currency := ResolveCurrencyScopeID(r)
		storeID := ResolveStoreScopeID(r)
		if pid == "" || vid == "" {
			h.audit(r, admin.AuditPriceUpdate, vid, nil, apperror.Validation("product id and variant id are required"))
			httpshared.JSONError(w, apperror.Validation("product id and variant id are required"))
			return
		}
		if currency == "" {
			h.audit(r, admin.AuditPriceUpdate, vid, nil, apperror.Validation("currency context is required"))
			httpshared.JSONError(w, apperror.Validation("currency context is required"))
			return
		}

		var req updateProductPriceRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			h.audit(r, admin.AuditPriceUpdate, vid, nil, apperror.Validation("invalid request body"))
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if req.Amount == nil {
			h.audit(r, admin.AuditPriceUpdate, vid, nil, apperror.Validation("amount is required"))
			httpshared.JSONError(w, apperror.Validation("amount is required"))
			return
		}

		variant := h.resolveVariant(w, r, admin.AuditPriceUpdate, pid, vid)
		if variant == nil {
			return
		}

		money, err := shared.NewMoney(*req.Amount, currency)
		if err != nil {
			err := apperror.Validation(err.Error())
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		price, err := pricing.NewPrice(id.New(), vid, storeID, money)
		if err != nil {
			err := apperror.Validation(err.Error())
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if err := h.prices.Upsert(r.Context(), &price); err != nil {
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditPriceUpdate, vid, map[string]interface{}{"amount": *req.Amount, "price_store_id": storeID}, nil)

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"price":           pricePayload(&price),
			"global_fallback": nil,
			"price_scope":     priceScopeForStore(storeID),
			"currency":        currency,
			"scope":           scopePayloadFromRequest(r),
		})
	}
}

func pricePayload(p *pricing.Price) map[string]interface{} {
	if p == nil {
		return nil
	}
	return map[string]interface{}{
		"amount":   p.Amount.Amount(),
		"currency": p.Amount.Currency(),
		"store_id": p.StoreID,
	}
}

func priceScopeForStore(storeID string) string {
	if storeID == "" {
		return "global"
	}
	return "store"
}

func (h *ProductPriceAdminHandler) audit(r *http.Request, action admin.AuditAction, resourceID string, details map[string]interface{}, err error) {
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
		ResourceType: "variant_price",
		ResourceID:   resourceID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
}
