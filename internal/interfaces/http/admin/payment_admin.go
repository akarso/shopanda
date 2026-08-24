package admin

import (
	"net/http"
	"strings"
	"time"

	httpshared "github.com/akarso/shopanda/internal/interfaces/http/shared"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// PaymentAdminHandler serves read-only admin payment ledger endpoints.
type PaymentAdminHandler struct {
	payments payment.PaymentRepository
	auditor  *admin.Auditor
}

// NewPaymentAdminHandler creates a PaymentAdminHandler.
func NewPaymentAdminHandler(payments payment.PaymentRepository, auditor *admin.Auditor) *PaymentAdminHandler {
	if payments == nil {
		panic("http: payment repository must not be nil")
	}
	if auditor == nil {
		panic("http: auditor must not be nil")
	}
	return &PaymentAdminHandler{payments: payments, auditor: auditor}
}

type paymentResp struct {
	ID          string `json:"id"`
	OrderID     string `json:"order_id"`
	Method      string `json:"method"`
	Status      string `json:"status"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	ProviderRef string `json:"provider_ref,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func toPaymentResponse(p *payment.Payment) paymentResp {
	return paymentResp{
		ID:          p.ID,
		OrderID:     p.OrderID,
		Method:      string(p.Method),
		Status:      string(p.Status()),
		Amount:      p.Amount.Amount(),
		Currency:    p.Amount.Currency(),
		ProviderRef: p.ProviderRef,
		CreatedAt:   p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toPaymentResponses(list []payment.Payment) []paymentResp {
	out := make([]paymentResp, 0, len(list))
	for i := range list {
		out = append(out, toPaymentResponse(&list[i]))
	}
	return out
}

// List handles GET /api/v1/admin/payments.
func (h *PaymentAdminHandler) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, limit, err := httpshared.ParsePagination(r)
		if err != nil {
			httpshared.JSONError(w, err)
			return
		}

		status := payment.PaymentStatus(strings.TrimSpace(r.URL.Query().Get("status")))
		if status != "" && !status.IsValid() {
			httpshared.JSONError(w, apperror.Validation("invalid payment status"))
			return
		}

		adminID := adminIDFromRequest(r)
		list, err := h.payments.List(r.Context(), payment.ListFilter{
			Status: status,
			Offset: offset,
			Limit:  limit,
		})
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditPaymentList,
				ResourceType: "payments",
				Result:       "error",
				Error:        err.Error(),
			})
			httpshared.JSONError(w, err)
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditPaymentList,
			ResourceType: "payments",
			Result:       "success",
			Details: map[string]interface{}{
				"offset": offset,
				"limit":  limit,
				"status": string(status),
				"count":  len(list),
			},
		})

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"payments": toPaymentResponses(list),
			"offset":   offset,
			"limit":    limit,
		})
	}
}

// Get handles GET /api/v1/admin/payments/{paymentId}.
func (h *PaymentAdminHandler) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		paymentID := r.PathValue("paymentId")
		if paymentID == "" {
			httpshared.JSONError(w, apperror.Validation("payment id is required"))
			return
		}

		adminID := adminIDFromRequest(r)
		p, err := h.payments.FindByID(r.Context(), paymentID)
		if err != nil {
			h.auditor.LogAction(r.Context(), admin.AuditEntry{
				AdminID:      adminID,
				Action:       admin.AuditPaymentRead,
				ResourceType: "payment",
				ResourceID:   paymentID,
				Result:       "error",
				Error:        err.Error(),
			})
			httpshared.JSONError(w, err)
			return
		}
		if p == nil {
			httpshared.JSONError(w, apperror.NotFound("payment not found"))
			return
		}

		h.auditor.LogAction(r.Context(), admin.AuditEntry{
			AdminID:      adminID,
			Action:       admin.AuditPaymentRead,
			ResourceType: "payment",
			ResourceID:   paymentID,
			Result:       "success",
		})

		httpshared.JSON(w, http.StatusOK, map[string]interface{}{
			"payment": toPaymentResponse(p),
		})
	}
}
