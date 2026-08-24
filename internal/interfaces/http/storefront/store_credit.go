package storefront

import (
	"net/http"
	"strings"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	storecreditApp "github.com/akarso/shopanda/internal/application/storecredit"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// StoreCreditAccountHandler serves customer store credit endpoints.
type StoreCreditAccountHandler struct {
	svc *storecreditApp.Service
}

// NewStoreCreditAccountHandler creates a StoreCreditAccountHandler.
func NewStoreCreditAccountHandler(svc *storecreditApp.Service) *StoreCreditAccountHandler {
	if svc == nil {
		panic("http: store credit account service must not be nil")
	}
	return &StoreCreditAccountHandler{svc: svc}
}

// GetBalance handles GET /api/v1/account/store-credit.
func (h *StoreCreditAccountHandler) GetBalance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := auth.IdentityFrom(r.Context()).UserID
		if customerID == "" {
			httpshared.JSONError(w, apperror.Unauthorized("authentication required"))
			return
		}
		currency := strings.TrimSpace(r.URL.Query().Get("currency"))
		if currency == "" {
			httpshared.JSONError(w, apperror.Validation("currency query parameter is required"))
			return
		}

		balance, err := h.svc.GetBalance(r.Context(), customerID, currency)
		if err != nil {
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "get store credit failed", err))
			return
		}
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"balance": map[string]interface{}{
				"amount":   balance.Amount(),
				"currency": balance.Currency(),
			},
		})
	}
}
