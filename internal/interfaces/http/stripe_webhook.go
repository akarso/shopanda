package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/infrastructure/stripepay"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
)

// StripeWebhookHandler handles Stripe-specific webhook events at
// POST /api/v1/payments/webhook/stripe. It verifies the Stripe-Signature
// header and routes payment_intent events to update payment status.
type StripeWebhookHandler struct {
	repo          payment.PaymentRepository
	bus           *event.Bus
	webhookSecret string
}

// NewStripeWebhookHandler creates a StripeWebhookHandler.
func NewStripeWebhookHandler(
	repo payment.PaymentRepository,
	bus *event.Bus,
	webhookSecret string,
) *StripeWebhookHandler {
	if repo == nil {
		panic("http: payment repository must not be nil")
	}
	if bus == nil {
		panic("http: event bus must not be nil")
	}
	if webhookSecret == "" {
		panic("http: stripe webhook secret must not be empty")
	}
	return &StripeWebhookHandler{
		repo:          repo,
		bus:           bus,
		webhookSecret: webhookSecret,
	}
}

// stripeEvent is the subset of a Stripe event envelope we parse.
type stripeEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object struct {
			ID       string            `json:"id"`
			Metadata map[string]string `json:"metadata"`
		} `json:"object"`
	} `json:"data"`
}

// Handle returns an http.HandlerFunc for POST /api/v1/payments/webhook/stripe.
func (h *StripeWebhookHandler) Handle() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Buffer raw body for signature verification.
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

		// Verify Stripe-Signature header.
		sig := r.Header.Get("Stripe-Signature")
		if verifyErr := stripepay.VerifySignature(h.webhookSecret, sig, raw); verifyErr != nil {
			if errors.Is(verifyErr, payment.ErrSignatureMissing) {
				JSONError(w, apperror.Unauthorized("webhook signature missing"))
				return
			}
			JSONError(w, apperror.Unauthorized("webhook signature invalid"))
			return
		}

		// Parse Stripe event envelope.
		var evt stripeEvent
		if err := json.Unmarshal(raw, &evt); err != nil {
			JSONError(w, apperror.Validation("invalid request body"))
			return
		}

		if evt.ID == "" {
			JSONError(w, apperror.Validation("event id is required"))
			return
		}

		// Route by event type.
		switch evt.Type {
		case "payment_intent.succeeded", "payment_intent.payment_failed":
			h.handlePaymentIntent(w, r, evt)
		default:
			// Acknowledge unhandled event types so Stripe doesn't retry.
			JSON(w, http.StatusOK, map[string]interface{}{
				"status": "ignored",
			})
		}
	}
}

func (h *StripeWebhookHandler) handlePaymentIntent(w http.ResponseWriter, r *http.Request, evt stripeEvent) {
	paymentID := evt.Data.Object.Metadata["payment_id"]
	if paymentID == "" {
		JSONError(w, apperror.Validation("missing payment_id in event metadata"))
		return
	}

	p, err := h.repo.FindByID(r.Context(), paymentID)
	if err != nil {
		JSONError(w, err)
		return
	}
	if p == nil {
		JSONError(w, apperror.NotFound("payment not found"))
		return
	}

	if p.Method != payment.MethodStripe {
		JSONError(w, apperror.Validation("provider mismatch"))
		return
	}

	prevUpdatedAt := p.UpdatedAt
	oldStatus := p.Status()

	success := evt.Type == "payment_intent.succeeded"

	if success {
		if completeErr := p.Complete(evt.Data.Object.ID); completeErr != nil {
			// Payment already completed/failed — idempotent: return 200.
			JSON(w, http.StatusOK, map[string]interface{}{
				"status": "already_processed",
			})
			return
		}
	} else {
		if failErr := p.Fail(); failErr != nil {
			// Payment already completed/failed — idempotent: return 200.
			JSON(w, http.StatusOK, map[string]interface{}{
				"status": "already_processed",
			})
			return
		}
	}

	if err := h.repo.UpdateStatus(r.Context(), p, prevUpdatedAt); err != nil {
		JSONError(w, err)
		return
	}

	evtName := payment.EventPaymentCompleted
	if !success {
		evtName = payment.EventPaymentFailed
	}
	_ = h.bus.Publish(r.Context(), event.New(evtName, "payment.stripe_webhook", payment.PaymentStatusChangedData{
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
