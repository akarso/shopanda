package pricing

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/b2b/groups"
)

// AdminHandler serves group price admin endpoints.
type AdminHandler struct {
	groups   groups.Repository
	prices   GroupPriceRepository
	variants pluginsdk.CatalogPort
}

// NewAdminHandler creates an AdminHandler.
func NewAdminHandler(
	groupRepo groups.Repository,
	prices GroupPriceRepository,
	variants pluginsdk.CatalogPort,
) *AdminHandler {
	if groupRepo == nil || prices == nil || variants == nil {
		panic("b2b group price admin: repositories must not be nil")
	}
	return &AdminHandler{groups: groupRepo, prices: prices, variants: variants}
}

// VariantRepository returns the variants port wired into this handler.
func (h *AdminHandler) VariantRepository() pluginsdk.CatalogPort {
	return h.variants
}

type updateGroupPriceRequest struct {
	Amount *int64 `json:"amount"`
}

// Get handles GET /api/v1/admin/customer-groups/{groupId}/variants/{variantId}/price.
func (h *AdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID := strings.TrimSpace(r.PathValue("groupId"))
		variantID := strings.TrimSpace(r.PathValue("variantId"))
		currency := pluginsdk.AdminCurrencyScope(r)
		storeID := pluginsdk.AdminStoreScope(r)
		if groupID == "" || variantID == "" {
			pluginsdk.JSONError(w, pluginsdk.ValidationError("group id and variant id are required"))
			return
		}
		if currency == "" {
			pluginsdk.JSONError(w, pluginsdk.ValidationError("currency context is required"))
			return
		}
		if err := h.ensureGroupAndVariant(w, r, groupID, variantID); err != nil {
			return
		}

		price, err := h.prices.FindExactByVariantGroupCurrencyAndStore(r.Context(), variantID, groupID, currency, storeID)
		if err != nil {
			pluginsdk.JSONError(w, pluginsdk.InternalError("get group price failed", err))
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
				pluginsdk.JSONError(w, pluginsdk.InternalError("get group price failed", err))
				return
			}
			globalFallback = groupPricePayload(globalPrice)
		}

		pluginsdk.JSON(w, http.StatusOK, map[string]interface{}{
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
		currency := pluginsdk.AdminCurrencyScope(r)
		storeID := pluginsdk.AdminStoreScope(r)
		if groupID == "" || variantID == "" {
			pluginsdk.JSONError(w, pluginsdk.ValidationError("group id and variant id are required"))
			return
		}
		if currency == "" {
			pluginsdk.JSONError(w, pluginsdk.ValidationError("currency context is required"))
			return
		}

		var req updateGroupPriceRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			pluginsdk.JSONError(w, pluginsdk.ValidationError("invalid request body"))
			return
		}
		if req.Amount == nil {
			pluginsdk.JSONError(w, pluginsdk.ValidationError("amount is required"))
			return
		}

		if err := h.ensureGroupAndVariant(w, r, groupID, variantID); err != nil {
			return
		}

		money, err := extapi.NewMoney(*req.Amount, currency)
		if err != nil {
			pluginsdk.JSONError(w, pluginsdk.ValidationError(err.Error()))
			return
		}
		price, err := NewGroupPrice(uuid.NewString(), groupID, variantID, storeID, money)
		if err != nil {
			pluginsdk.JSONError(w, pluginsdk.ValidationError(err.Error()))
			return
		}
		if err := h.prices.Upsert(r.Context(), &price); err != nil {
			if strings.Contains(err.Error(), "not found") {
				pluginsdk.JSONError(w, pluginsdk.NotFoundError("group or variant not found"))
				return
			}
			pluginsdk.JSONError(w, pluginsdk.InternalError("update group price failed", err))
			return
		}

		pluginsdk.JSON(w, http.StatusOK, map[string]interface{}{
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
		currency := pluginsdk.AdminCurrencyScope(r)
		storeID := pluginsdk.AdminStoreScope(r)
		if groupID == "" || variantID == "" || currency == "" {
			pluginsdk.JSONError(w, pluginsdk.ValidationError("group id, variant id, and currency are required"))
			return
		}
		if err := h.ensureGroupAndVariant(w, r, groupID, variantID); err != nil {
			return
		}

		price, err := h.prices.FindExactByVariantGroupCurrencyAndStore(r.Context(), variantID, groupID, currency, storeID)
		if err != nil {
			pluginsdk.JSONError(w, pluginsdk.InternalError("delete group price failed", err))
			return
		}
		if price == nil {
			pluginsdk.JSONError(w, pluginsdk.NotFoundError("group price not found"))
			return
		}
		if err := h.prices.Delete(r.Context(), price.ID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				pluginsdk.JSONError(w, pluginsdk.NotFoundError("group price not found"))
				return
			}
			pluginsdk.JSONError(w, pluginsdk.InternalError("delete group price failed", err))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *AdminHandler) ensureGroupAndVariant(w http.ResponseWriter, r *http.Request, groupID, variantID string) error {
	group, err := h.groups.FindByID(r.Context(), groupID)
	if err != nil {
		pluginsdk.JSONError(w, pluginsdk.InternalError("lookup group failed", err))
		return err
	}
	if group == nil {
		pluginsdk.JSONError(w, pluginsdk.NotFoundError("customer group not found"))
		return pluginsdk.NotFoundError("customer group not found")
	}
	variant, err := h.variants.FindVariant(r.Context(), variantID)
	if err != nil {
		pluginsdk.JSONError(w, pluginsdk.InternalError("lookup variant failed", err))
		return err
	}
	if variant == nil {
		pluginsdk.JSONError(w, pluginsdk.NotFoundError("variant not found"))
		return pluginsdk.NotFoundError("variant not found")
	}
	return nil
}

func groupPricePayload(p *GroupPrice) map[string]interface{} {
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
