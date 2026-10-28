package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	orderapp "github.com/akarso/shopanda/internal/application/order"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
)

const maxWebhookBodySize = 1 << 20 // 1 MB

// PaymentWebhookHandler handles incoming payment provider webhooks.
type PaymentWebhookHandler struct {
	repo     payment.PaymentRepository
	bus      *event.Bus
	verifier payment.WebhookVerifier
}

// NewPaymentWebhookHandler creates a PaymentWebhookHandler.
func NewPaymentWebhookHandler(repo payment.PaymentRepository, bus *event.Bus, verifier payment.WebhookVerifier) *PaymentWebhookHandler {
	if repo == nil {
		panic("http: payment repository must not be nil")
	}
	if bus == nil {
		panic("http: event bus must not be nil")
	}
	if verifier == nil {
		panic("http: webhook verifier must not be nil")
	}
	return &PaymentWebhookHandler{repo: repo, bus: bus, verifier: verifier}
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

		// Buffer the raw body for signature verification.
		// MaxBytesReader discards the remainder of oversized requests,
		// freeing the connection for keep-alive reuse.
		r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodySize)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				JSONError(w, apperror.Validation("request body too large"))
				return
			}
			JSONError(w, apperror.Validation("failed to read request body"))
			return
		}

		// Verify webhook signature.
		sig := r.Header.Get("X-Webhook-Signature")
		if verifyErr := h.verifier.Verify(provider, sig, raw); verifyErr != nil {
			if errors.Is(verifyErr, payment.ErrSignatureMissing) {
				JSONError(w, apperror.Unauthorized("webhook signature missing"))
				return
			}
			JSONError(w, apperror.Unauthorized("webhook signature invalid"))
			return
		}

		var req webhookRequest
		if err := json.Unmarshal(raw, &req); err != nil {
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

		// Idempotent heal: republish side-effects when money already settled
		// (mirrors payment.failed retry) so grant issuance can catch up.
		if p.Status() != payment.StatusPending {
			if !req.Success && p.Status() == payment.StatusFailed {
				if err := publishPaymentFailed(h.bus, r.Context(), "payment.webhook", p, p.Status()); err != nil {
					JSONError(w, err)
					return
				}
			}
			if req.Success && p.Status() == payment.StatusCompleted {
				_ = publishOrderPaidAfterPayment(h.bus, r.Context(), "payment.webhook", p)
			}
			JSON(w, http.StatusOK, map[string]interface{}{
				"status": "already_processed",
			})
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
		if err := h.bus.Publish(r.Context(), event.New(evtName, "payment.webhook", payment.PaymentStatusChangedData{
			PaymentID:   p.ID,
			OrderID:     p.OrderID,
			OldStatus:   oldStatus,
			NewStatus:   p.Status(),
			ProviderRef: p.ProviderRef,
		})); err != nil && evtName == payment.EventPaymentFailed {
			JSONError(w, err)
			return
		}
		if req.Success {
			// Persist already succeeded — do not HTTP 500 on publish failure.
			// Provider retries still republish order.paid via already_processed.
			_ = publishOrderPaidAfterPayment(h.bus, r.Context(), "payment.webhook", p)
		}

		JSON(w, http.StatusOK, map[string]interface{}{
			"status": "accepted",
		})
	}
}

func publishPaymentFailed(bus *event.Bus, ctx context.Context, source string, p *payment.Payment, oldStatus payment.PaymentStatus) error {
	if bus == nil || p == nil {
		return nil
	}
	return bus.Publish(ctx, event.New(payment.EventPaymentFailed, source, payment.PaymentStatusChangedData{
		PaymentID:   p.ID,
		OrderID:     p.OrderID,
		OldStatus:   oldStatus,
		NewStatus:   p.Status(),
		ProviderRef: p.ProviderRef,
	}))
}

// publishOrderPaidAfterPayment emits order.paid after a completed payment
// persist so DownloadGrant issuance (and notification) share one subscriber
// (PR-1075). Does not change order status — ownership is payment-based.
// OldStatus/NewStatus are order statuses (not payment statuses).
func publishOrderPaidAfterPayment(bus *event.Bus, ctx context.Context, source string, p *payment.Payment) error {
	if bus == nil || p == nil {
		return nil
	}
	return orderapp.PublishPaid(ctx, bus, source, p.OrderID, string(order.OrderStatusPending), string(order.OrderStatusPaid))
}
