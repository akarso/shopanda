package admin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ProductPriceAdminHandler serves store-scoped variant price read/write
// endpoints. The store and currency come from the active admin scope context;
// an empty store means the global/default price.
type ProductPriceAdminHandler struct {
	products  catalog.ProductRepository
	variants  catalog.VariantRepository
	prices    pricing.PriceRepository
	txStarter txStarter
	auditor   *admin.Auditor
	log       logger.Logger
	bus       *event.Bus // optional; nil (the default) skips publishing — see SetBus.
}

// txPriceRepo binds a price upsert to the same transaction as the product row lock.
type txPriceRepo interface {
	pricing.PriceRepository
	WithTx(tx *sql.Tx) pricing.PriceRepository
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

// WithTxStarter wires the transaction beginner used to lock the product row
// for the duration of a price write. *sql.DB satisfies this. A price PUT with
// a nil starter fails closed so a concurrent mode switch cannot race the upsert.
func (h *ProductPriceAdminHandler) WithTxStarter(starter txStarter) *ProductPriceAdminHandler {
	h.txStarter = starter
	return h
}

// SetBus enables publishing pricing.EventPriceUpserted on every successful
// Update (PR-1036: the search index's on-save subscriber listens for it —
// previously only the bulk price importer published this event, so an
// interactive admin price edit never reached the search index at all).
// Left unset (nil), Update works exactly as before.
func (h *ProductPriceAdminHandler) SetBus(bus *event.Bus) {
	h.bus = bus
}

type updateProductPriceRequest struct {
	Amount *int64 `json:"amount"`
}

// resolveVariant verifies the parent product and the variant belong together.
// It writes an audit entry and JSON error and returns nil, nil on any failure.
func (h *ProductPriceAdminHandler) resolveVariant(w http.ResponseWriter, r *http.Request, action admin.AuditAction, pid, vid string) (*catalog.Variant, *catalog.Product) {
	product, err := h.products.FindByID(r.Context(), pid)
	if err != nil {
		h.audit(r, action, vid, nil, err)
		httpshared.JSONError(w, err)
		return nil, nil
	}
	if product == nil {
		h.audit(r, action, vid, nil, apperror.NotFound("product not found"))
		httpshared.JSONError(w, apperror.NotFound("product not found"))
		return nil, nil
	}
	variant, err := h.variants.FindByID(r.Context(), vid)
	if err != nil {
		h.audit(r, action, vid, nil, err)
		httpshared.JSONError(w, err)
		return nil, nil
	}
	if variant == nil || variant.ProductID != pid {
		h.audit(r, action, vid, nil, apperror.NotFound("variant not found"))
		httpshared.JSONError(w, apperror.NotFound("variant not found"))
		return nil, nil
	}
	return variant, product
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

		variant, _ := h.resolveVariant(w, r, admin.AuditPriceRead, pid, vid)
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
// The product row is locked (SELECT type, pricing_mode FOR UPDATE) and the
// price upsert commits on that same transaction, so a concurrent switch to
// grouped or dynamic cannot land an own price row.
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

		if h.txStarter == nil {
			err := apperror.Internal("product price admin: transaction starter is not configured")
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		tx, err := h.txStarter.BeginTx(r.Context(), nil)
		if err != nil {
			err = fmt.Errorf("product price admin: begin tx: %w", err)
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		defer tx.Rollback()

		txProducts, ok := h.products.(txProductRepo)
		if !ok {
			err := fmt.Errorf("product price admin: product repository does not support transactions")
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		locker, ok := txProducts.WithTx(tx).(lockingProductRepo)
		if !ok {
			err := fmt.Errorf("product price admin: product repository does not support row locks")
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		productType, pricingMode, err := locker.LockTypeAndPricingMode(r.Context(), pid)
		if err != nil {
			if apperror.Is(err, apperror.CodeNotFound) {
				h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
				httpshared.JSONError(w, err)
				return
			}
			err = fmt.Errorf("product price admin: lock product: %w", err)
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}

		variant, err := h.variants.FindByID(r.Context(), vid)
		if err != nil {
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if variant == nil || variant.ProductID != pid {
			err := apperror.NotFound("variant not found")
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if productType == catalog.TypeGrouped {
			err := apperror.Validation(catalog.GroupedPriceRejectedMessage)
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if productType == catalog.TypeBundle && pricingMode == catalog.PricingModeDynamic {
			err := apperror.Validation(catalog.DynamicBundlePriceRejectedMessage)
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
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
		txPrices, ok := h.prices.(txPriceRepo)
		if !ok {
			err := fmt.Errorf("product price admin: price repository does not support transactions")
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if err := txPrices.WithTx(tx).Upsert(r.Context(), &price); err != nil {
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			err = fmt.Errorf("product price admin: commit: %w", err)
			h.audit(r, admin.AuditPriceUpdate, vid, nil, err)
			httpshared.JSONError(w, err)
			return
		}

		h.audit(r, admin.AuditPriceUpdate, vid, map[string]interface{}{"amount": *req.Amount, "price_store_id": storeID}, nil)

		if h.bus != nil {
			_ = h.bus.Publish(r.Context(), event.New(pricing.EventPriceUpserted, "price.admin", pricing.PriceUpsertedData{
				PriceID:   price.ID,
				VariantID: price.VariantID,
				ProductID: pid,
				StoreID:   price.StoreID,
				Currency:  price.Amount.Currency(),
				Amount:    price.Amount.Amount(),
			}))
		}

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
