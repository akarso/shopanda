package http

import (
	"encoding/json"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
)

// PaymentWebhookHandler handles incoming payment provider webhooks.
type PaymentWebhookHandler struct {
	repo payment.PaymentRepository
	bus  *event.Bus
}

// NewPaymentWebhookHandler creates a PaymentWebhookHandler.
func NewPaymentWebhookHandler(repo payment.PaymentRepository, bus *event.Bus) *PaymentWebhookHandler {
	if repo == nil {
		panic("http: payment repository must not be nil")
	}
	if bus == nil {
		panic("http: event bus must not be nil")
	}
	return &PaymentWebhookHandler{repo: repo, bus: bus}
}

type webhookRequest struct {
	PaymentID   string `json:"payment_id"`
	ProviderRef string `json:"provider_ref"`
	Success     bool   `json:"success"`
}

// Handle processes POST /api/v1/payments/webhook/{provider}.
func (h *PaymentWebhookHandler) Handle() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := r.PathValue("provider")
		if provider == "" {
			JSONError(w, apperror.Validation("provider is required"))
			return
		}

		method := payment.PaymentMethod(provider)
		if !method.IsValid() {
			JSONError(w, apperror.Validation("unknown payment provider"))
			return
		}

		var req webhookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		if req.PaymentID == "" {
			JSONError(w, apperror.Validation("payment_id is required"))
			return
		}

		p, err := h.repo.FindByID(r.Context(), req.PaymentID)
		if err != nil {
			JSONError(w, err)
			return
		}
		if p == nil {
			JSONError(w, apperror.NotFound("payment not found"))
			return
		}

		if p.Method != method {
			JSONError(w, apperror.Validation("provider mismatch"))
			return
		}

		prevUpdatedAt := p.UpdatedAt
		oldStatus := p.Status()

		if req.Success {
			if err := p.Complete(req.ProviderRef); err != nil {
				JSONError(w, apperror.Validation(err.Error()))
				return
			}
		} else {
			if err := p.Fail(); err != nil {
				JSONError(w, apperror.Validation(err.Error()))
				return
			}
		}

		if err := h.repo.UpdateStatus(r.Context(), p, prevUpdatedAt); err != nil {
			JSONError(w, err)
			return
		}

		evtName := payment.EventPaymentCompleted
		if !req.Success {
			evtName = payment.EventPaymentFailed
		}
		_ = h.bus.Publish(r.Context(), event.New(evtName, "payment.webhook", payment.PaymentStatusChangedData{
			PaymentID:   p.ID,
			OrderID:     p.OrderID,
			OldStatus:   oldStatus,
			NewStatus:   p.Status(),
			ProviderRef: p.ProviderRef,
		}))

		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "accepted",
		})
	}
}
