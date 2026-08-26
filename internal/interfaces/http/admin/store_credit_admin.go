package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	storecreditApp "github.com/akarso/shopanda/internal/application/storecredit"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/storecredit"
	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// StoreCreditAdminHandler serves admin store credit endpoints.
type StoreCreditAdminHandler struct {
	svc     *storecreditApp.Service
	auditor *adminapp.Auditor
}

// NewStoreCreditAdminHandler creates a StoreCreditAdminHandler.
func NewStoreCreditAdminHandler(svc *storecreditApp.Service, auditor *adminapp.Auditor) *StoreCreditAdminHandler {
	if svc == nil {
		panic("http: store credit admin service must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &StoreCreditAdminHandler{svc: svc, auditor: auditor}
}

func (h *StoreCreditAdminHandler) audit(r *http.Request, action adminapp.AuditAction, customerID string, details map[string]interface{}, err error) {
	merged := mergeAuditDetails(details, fullAdminScopeDetailsFromRequest(r))
	result := "success"
	errMsg := ""
	if err != nil {
		result = "error"
		errMsg = err.Error()
	}
	h.auditor.LogAction(r.Context(), adminapp.AuditEntry{
		AdminID:      adminIDFromRequest(r),
		Action:       action,
		ResourceType: "store_credit",
		ResourceID:   customerID,
		Details:      merged,
		Result:       result,
		Error:        errMsg,
	})
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
			httpshared.JSONError(w, apperror.Validation("customer id is required"))
			return
		}
		if currency == "" {
			httpshared.JSONError(w, apperror.Validation("currency context is required"))
			return
		}

		balance, err := h.svc.GetBalance(r.Context(), customerID, currency)
		if err != nil {
			h.audit(r, adminapp.AuditStoreCreditRead, customerID, nil, err)
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "get store credit failed", err))
			return
		}

		offset, limit, err := httpshared.ParsePagination(r)
		if err != nil {
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		entries, err := h.svc.ListLedger(r.Context(), customerID, currency, offset, limit)
		if err != nil {
			h.audit(r, adminapp.AuditStoreCreditRead, customerID, nil, err)
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "list store credit ledger failed", err))
			return
		}

		h.audit(r, adminapp.AuditStoreCreditRead, customerID, map[string]interface{}{
			"currency": currency,
			"count":    len(entries),
		}, nil)
		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
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
			httpshared.JSONError(w, apperror.Validation("customer id is required"))
			return
		}
		if currency == "" {
			httpshared.JSONError(w, apperror.Validation("currency context is required"))
			return
		}

		var req issueStoreCreditRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			httpshared.JSONError(w, apperror.Validation("invalid request body"))
			return
		}
		if req.Amount <= 0 {
			httpshared.JSONError(w, apperror.Validation("amount must be positive"))
			return
		}

		amount, err := shared.NewMoney(req.Amount, currency)
		if err != nil {
			httpshared.JSONError(w, apperror.Validation(err.Error()))
			return
		}
		// Idempotency-Key follows the same header convention used for
		// outbound provider calls (internal/infrastructure/stripepay) and
		// inbound plugin requests (internal/platform/plugin/integration.go):
		// optional, but when a client retries a request with the same key
		// (e.g. after a timeout with an ambiguous response) the service
		// treats it as a no-op instead of issuing credit twice.
		idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if len(idempotencyKey) > 255 {
			// Matches store_credit_ledger.idempotency_key's VARCHAR(255)
			// bound (migration 064) — reject here with a clear 422 rather
			// than let an oversized header reach the DB and surface as a
			// generic constraint-violation 500.
			httpshared.JSONError(w, apperror.Validation("Idempotency-Key must not exceed 255 characters"))
			return
		}
		if err := h.svc.Issue(r.Context(), customerID, amount, req.Note, idempotencyKey); err != nil {
			// JSONError unwraps to the underlying *apperror.Error (via
			// errors.As) and maps its code to the right status — e.g.
			// apperror.Validation from domain-level ledger checks becomes
			// 422, not a blanket 500. Only errors with no apperror code at
			// all fall through to Internal, which is the correct default
			// for those.
			h.audit(r, adminapp.AuditStoreCreditIssue, customerID, map[string]interface{}{
				"currency": currency,
				"amount":   req.Amount,
			}, err)
			httpshared.JSONError(w, err)
			return
		}

		balance, err := h.svc.GetBalance(r.Context(), customerID, currency)
		if err != nil {
			httpshared.JSONError(w, apperror.Wrap(apperror.CodeInternal, "get store credit failed", err))
			return
		}
		h.audit(r, adminapp.AuditStoreCreditIssue, customerID, map[string]interface{}{
			"currency": currency,
			"amount":   req.Amount,
		}, nil)
		httpshared.JSON(w, http.StatusCreated, map[string]interface{}{
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
