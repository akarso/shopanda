package http

import (
	"net/http"
	"strconv"

	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// ShippingRatesHandler serves shipping rate quote endpoints.
type ShippingRatesHandler struct {
	providers []shipping.Provider
}

// NewShippingRatesHandler creates a ShippingRatesHandler with the given providers.
func NewShippingRatesHandler(providers ...shipping.Provider) *ShippingRatesHandler {
	if len(providers) == 0 {
		panic("http: at least one shipping provider is required")
	}
	return &ShippingRatesHandler{providers: providers}
}

type rateResponse struct {
	Method      string `json:"method"`
	ProviderRef string `json:"provider_ref"`
	Cost        int64  `json:"cost"`
	Currency    string `json:"currency"`
	Label       string `json:"label"`
}

// List handles GET /api/v1/shipping/rates.
func (h *ShippingRatesHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := r.URL.Query().Get("order_id")
		if orderID == "" {
			JSONError(w, apperror.Validation("order_id is required"))
			return
		}

		currency := r.URL.Query().Get("currency")
		if currency == "" {
			JSONError(w, apperror.Validation("currency is required"))
			return
		}

		itemCountStr := r.URL.Query().Get("item_count")
		if itemCountStr == "" {
			JSONError(w, apperror.Validation("item_count is required"))
			return
		}
		itemCount, err := strconv.Atoi(itemCountStr)
		if err != nil || itemCount < 1 {
			JSONError(w, apperror.Validation("item_count must be a positive integer"))
			return
		}

		rates := make([]rateResponse, 0, len(h.providers))
		for _, p := range h.providers {
			rate, err := p.CalculateRate(r.Context(), orderID, currency, itemCount)
			if err != nil {
				continue
			}
			rates = append(rates, rateResponse{
				Method:      string(p.Method()),
				ProviderRef: rate.ProviderRef,
				Cost:        rate.Cost.Amount(),
				Currency:    rate.Cost.Currency(),
				Label:       rate.Label,
			})
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"rates": rates,
		})
	}
}
