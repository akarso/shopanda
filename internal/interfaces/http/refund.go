package http

import (
	"encoding/json"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
)

// RefundHandler handles POST /api/v1/admin/orders/{orderId}/refund.
type RefundHandler struct {
	payments payment.PaymentRepository
	refunder payment.Refunder
	bus      *event.Bus
}

// NewRefundHandler creates a RefundHandler.
func NewRefundHandler(payments payment.PaymentRepository, refunder payment.Refunder, bus *event.Bus) *RefundHandler {
	if payments == nil {
		panic("http: payment repository must not be nil")
	}
	if refunder == nil {
		panic("http: refunder must not be nil")
	}
	if bus == nil {
		panic("http: event bus must not be nil")
	}
	return &RefundHandler{payments: payments, refunder: refunder, bus: bus}
}

type refundRequest struct {
	Amount int64  `json:"amount"`
	Reason string `json:"reason"`
}

// Refund handles POST /api/v1/admin/orders/{orderId}/refund.
func (h *RefundHandler) Refund() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orderID := r.PathValue("orderId")
		if orderID == "" {
			JSONError(w, apperror.Validation("order id is required"))
			return
		}

		var req refundRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		if req.Amount <= 0 {
			JSONError(w, apperror.Validation("amount must be positive"))
			return
		}

		p, err := h.payments.FindByOrderID(r.Context(), orderID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if p == nil {
			JSONError(w, apperror.NotFound("payment not found for order"))
			return
		}

		if p.ProviderRef == "" {
			JSONError(w, apperror.Validation("payment has no provider reference"))
			return
		}

		if req.Amount > p.Amount.Amount() {
			JSONError(w, apperror.Validation("refund amount exceeds payment amount"))
			return
		}

		// Call the provider to create the refund.
		result, err := h.refunder.Refund(r.Context(), p.ProviderRef, req.Amount, p.Currency())
		if err != nil {
			JSONError(w, apperror.Internal("refund failed: "+err.Error()))
			return
		}

		// Transition payment status.
		prevUpdatedAt := p.UpdatedAt
		oldStatus := p.Status()

		if err := p.Refund(); err != nil {
			JSONError(w, apperror.Validation(err.Error()))
			return
		}

		if err := h.payments.UpdateStatus(r.Context(), p, prevUpdatedAt); err != nil {
			JSONError(w, err)
			return
		}

		_ = h.bus.Publish(r.Context(), event.New(payment.EventPaymentRefunded, "payment.admin_refund", payment.PaymentStatusChangedData{
			PaymentID:   p.ID,
			OrderID:     p.OrderID,
			OldStatus:   oldStatus,
			NewStatus:   p.Status(),
			ProviderRef: result.ProviderRef,
		}))

		JSON(w, http.StatusOK, map[string]interface{}{
			"refund": map[string]interface{}{
				"payment_id":   p.ID,
				"order_id":     p.OrderID,
				"provider_ref": result.ProviderRef,
				"status":       string(p.Status()),
			},
		})
	}
}
