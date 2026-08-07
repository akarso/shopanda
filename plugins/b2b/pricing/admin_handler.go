package pricing

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/akarso/shopanda/internal/domain/shared"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// AdminHandler serves group price admin endpoints.
type AdminHandler struct {
	groups   customergroup.Repository
	prices   customergroup.GroupPriceRepository
	products catalog.ProductRepository
	variants catalog.VariantRepository
}

// NewAdminHandler creates an AdminHandler.
func NewAdminHandler(
	groups customergroup.Repository,
	prices customergroup.GroupPriceRepository,
	products catalog.ProductRepository,
	variants catalog.VariantRepository,
) *AdminHandler {
	if groups == nil || prices == nil || products == nil || variants == nil {
		panic("b2b group price admin: repositories must not be nil")
	}
	return &AdminHandler{groups: groups, prices: prices, products: products, variants: variants}
}

type updateGroupPriceRequest struct {
	Amount *int64 `json:"amount"`
}

// Get handles GET /api/v1/admin/customer-groups/{groupId}/variants/{variantId}/price.
func (h *AdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := strings.TrimSpace(r.PathValue("groupId"))
		variantID := strings.TrimSpace(r.PathValue("variantId"))
		currency := shophttp.ResolveCurrencyScopeID(r)
		storeID := shophttp.ResolveStoreScopeID(r)
		if groupID == "" || variantID == "" {
			shophttp.JSONError(w, apperror.Validation("group id and variant id are required"))
			return
		}
		if currency == "" {
			shophttp.JSONError(w, apperror.Validation("currency context is required"))
			return
		}
		if err := h.ensureGroupAndVariant(w, r, groupID, variantID); err != nil {
			return
		}

		price, err := h.prices.FindExactByVariantGroupCurrencyAndStore(r.Context(), variantID, groupID, currency, storeID)
		if err != nil {
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "get group price failed", err))
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
			globalPrice, err := h.prices.FindExactByVariantGroupCurrencyAndStore(r.Context(), variantID, groupID, currency, "")
			if err != nil {
				shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "get group price failed", err))
				return
			}
			globalFallback = groupPricePayload(globalPrice)
		}

		shophttp.JSON(w, http.StatusOK, map[string]interface{}{
			"price":           groupPricePayload(price),
			"global_fallback": globalFallback,
			"price_scope":     priceScope,
			"currency":        currency,
			"group_id":        groupID,
			"variant_id":      variantID,
		})
	}
}

// Update handles PUT /api/v1/admin/customer-groups/{groupId}/variants/{variantId}/price.
func (h *AdminHandler) Update() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := strings.TrimSpace(r.PathValue("groupId"))
		variantID := strings.TrimSpace(r.PathValue("variantId"))
		currency := shophttp.ResolveCurrencyScopeID(r)
		storeID := shophttp.ResolveStoreScopeID(r)
		if groupID == "" || variantID == "" {
			shophttp.JSONError(w, apperror.Validation("group id and variant id are required"))
			return
		}
		if currency == "" {
			shophttp.JSONError(w, apperror.Validation("currency context is required"))
			return
		}

		var req updateGroupPriceRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			shophttp.JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if req.Amount == nil {
			shophttp.JSONError(w, apperror.Validation("amount is required"))
			return
		}

		if err := h.ensureGroupAndVariant(w, r, groupID, variantID); err != nil {
			return
		}

		money, err := shared.NewMoney(*req.Amount, currency)
		if err != nil {
			shophttp.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		price, err := customergroup.NewGroupPrice(id.New(), groupID, variantID, storeID, money)
		if err != nil {
			shophttp.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		if err := h.prices.Upsert(r.Context(), &price); err != nil {
			if strings.Contains(err.Error(), "not found") {
				shophttp.JSONError(w, apperror.NotFound("group or variant not found"))
				return
			}
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "update group price failed", err))
			return
		}

		shophttp.JSON(w, http.StatusOK, map[string]interface{}{
			"price":           groupPricePayload(&price),
			"global_fallback": nil,
			"price_scope":     groupPriceScopeForStore(storeID),
			"currency":        currency,
			"group_id":        groupID,
			"variant_id":      variantID,
		})
	}
}

// Delete handles DELETE /api/v1/admin/customer-groups/{groupId}/variants/{variantId}/price.
func (h *AdminHandler) Delete() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := strings.TrimSpace(r.PathValue("groupId"))
		variantID := strings.TrimSpace(r.PathValue("variantId"))
		currency := shophttp.ResolveCurrencyScopeID(r)
		storeID := shophttp.ResolveStoreScopeID(r)
		if groupID == "" || variantID == "" || currency == "" {
			shophttp.JSONError(w, apperror.Validation("group id, variant id, and currency are required"))
			return
		}
		if err := h.ensureGroupAndVariant(w, r, groupID, variantID); err != nil {
			return
		}

		price, err := h.prices.FindExactByVariantGroupCurrencyAndStore(r.Context(), variantID, groupID, currency, storeID)
		if err != nil {
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "delete group price failed", err))
			return
		}
		if price == nil {
			shophttp.JSONError(w, apperror.NotFound("group price not found"))
			return
		}
		if err := h.prices.Delete(r.Context(), price.ID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				shophttp.JSONError(w, apperror.NotFound("group price not found"))
				return
			}
			shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "delete group price failed", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *AdminHandler) ensureGroupAndVariant(w http.ResponseWriter, r *http.Request, groupID, variantID string) error {
	group, err := h.groups.FindByID(r.Context(), groupID)
	if err != nil {
		shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "lookup group failed", err))
		return err
	}
	if group == nil {
		shophttp.JSONError(w, apperror.NotFound("customer group not found"))
		return apperror.NotFound("customer group not found")
	}
	variant, err := h.variants.FindByID(r.Context(), variantID)
	if err != nil {
		shophttp.JSONError(w, apperror.Wrap(apperror.CodeInternal, "lookup variant failed", err))
		return err
	}
	if variant == nil {
		shophttp.JSONError(w, apperror.NotFound("variant not found"))
		return apperror.NotFound("variant not found")
	}
	return nil
}

func groupPricePayload(p *customergroup.GroupPrice) map[string]interface{} {
	if p == nil {
		return nil
	}
	return map[string]interface{}{
		"amount":   p.Amount.Amount(),
		"currency": p.Amount.Currency(),
		"store_id": p.StoreID,
		"group_id": p.GroupID,
	}
}

func groupPriceScopeForStore(storeID string) string {
	if storeID == "" {
		return "global"
	}
	return "store"
}
