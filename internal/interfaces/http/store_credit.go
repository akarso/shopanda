package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	storecreditApp "github.com/akarso/shopanda/internal/application/storecredit"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/storecredit"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth"
)

// StoreCreditAdminHandler serves admin store credit endpoints.
type StoreCreditAdminHandler struct {
	svc *storecreditApp.Service
}

// NewStoreCreditAdminHandler creates a StoreCreditAdminHandler.
func NewStoreCreditAdminHandler(svc *storecreditApp.Service) *StoreCreditAdminHandler {
	if svc == nil {
		panic("http: store credit admin service must not be nil")
	}
	return &StoreCreditAdminHandler{svc: svc}
}

type issueStoreCreditRequest struct {
	Amount int64  `json:"amount"`
	Note   string `json:"note"`
}

// Get handles GET /api/v1/admin/customers/{customerId}/store-credit.
func (h *StoreCreditAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := strings.TrimSpace(r.PathValue("customerId"))
		currency := ResolveCurrencyScopeID(r)
		if customerID == "" {
			JSONError(w, apperror.Validation("customer id is required"))
			return
		}
		if currency == "" {
			JSONError(w, apperror.Validation("currency context is required"))
			return
		}

		balance, err := h.svc.GetBalance(r.Context(), customerID, currency)
		if err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "get store credit failed", err))
			return
		}

		offset, limit, err := ParsePagination(r)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}
		entries, err := h.svc.ListLedger(r.Context(), customerID, currency, offset, limit)
		if err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "list store credit ledger failed", err))
			return
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"balance": map[string]interface{}{
				"amount":   balance.Amount(),
				"currency": balance.Currency(),
			},
			"ledger": toStoreCreditLedgerResponses(entries),
		})
	}
}

// Issue handles POST /api/v1/admin/customers/{customerId}/store-credit/issue.
func (h *StoreCreditAdminHandler) Issue() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := strings.TrimSpace(r.PathValue("customerId"))
		currency := ResolveCurrencyScopeID(r)
		if customerID == "" {
			JSONError(w, apperror.Validation("customer id is required"))
			return
		}
		if currency == "" {
			JSONError(w, apperror.Validation("currency context is required"))
			return
		}

		var req issueStoreCreditRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if req.Amount <= 0 {
			JSONError(w, apperror.Validation("amount must be positive"))
			return
		}

		amount, err := shared.NewMoney(req.Amount, currency)
		if err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}
		if err := h.svc.Issue(r.Context(), customerID, amount, req.Note); err != nil {
			if apperror.Is(err, apperror.CodeNotFound) {
				JSONError(w, err)
				return
			}
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "issue store credit failed", err))
			return
		}

		balance, err := h.svc.GetBalance(r.Context(), customerID, currency)
		if err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "get store credit failed", err))
			return
		}
		JSON(w, http.StatusCreated, map[string]interface{}{
			"balance": map[string]interface{}{
				"amount":   balance.Amount(),
				"currency": balance.Currency(),
			},
		})
	}
}

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
			JSONError(w, apperror.Unauthorized("authentication required"))
			return
		}
		currency := strings.TrimSpace(r.URL.Query().Get("currency"))
		if currency == "" {
			JSONError(w, apperror.Validation("currency query parameter is required"))
			return
		}

		balance, err := h.svc.GetBalance(r.Context(), customerID, currency)
		if err != nil {
			JSONError(w, apperror.Wrap(apperror.CodeInternal, "get store credit failed", err))
			return
		}
		JSON(w, http.StatusOK, map[string]interface{}{
			"balance": map[string]interface{}{
				"amount":   balance.Amount(),
				"currency": balance.Currency(),
			},
		})
	}
}

func toStoreCreditLedgerResponses(entries []storecredit.Entry) []map[string]interface{} {
	out := make([]map[string]interface{}, len(entries))
	for i, e := range entries {
		out[i] = map[string]interface{}{
			"id":         e.ID,
			"kind":       string(e.Kind),
			"amount":     e.Amount.Amount(),
			"currency":   e.Amount.Currency(),
			"order_id":   e.OrderID,
			"note":       e.Note,
			"created_at": e.CreatedAt.UTC().Format(time.RFC3339),
		}
	}
	return out
}
