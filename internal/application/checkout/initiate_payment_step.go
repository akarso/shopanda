package checkout

import (
	"context"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// InitiatePaymentStep creates a payment record and initiates it via the selected provider.
type InitiatePaymentStep struct {
	providers *payment.ProviderRegistry
	payments  payment.PaymentRepository
}

// NewInitiatePaymentStep creates an InitiatePaymentStep.
func NewInitiatePaymentStep(
	providers *payment.ProviderRegistry,
	payments payment.PaymentRepository,
) *InitiatePaymentStep {
	if providers == nil || providers.Len() == 0 {
		panic("checkout: payment providers must not be empty")
	}
	if payments == nil {
		panic("checkout: payment repository must not be nil")
	}
	return &InitiatePaymentStep{providers: providers, payments: payments}
}

func (s *InitiatePaymentStep) Name() string { return "initiate_payment" }

func (s *InitiatePaymentStep) Execute(cctx *Context) error {
	if cctx == nil {
		return fmt.Errorf("initiate_payment: checkout context must not be nil")
	}
	if v, ok := cctx.GetMeta("payment_initiated"); ok {
		if b, isBool := v.(bool); isBool && b {
			return nil // idempotent
		}
	}

	if cctx.Order == nil {
		return fmt.Errorf("initiate_payment: order not created yet")
	}

	provider, err := s.providers.Resolve(cctx.Input.PaymentMethod)
	if err != nil {
		return apperror.Validation("selected payment method is unavailable")
	}

	py, err := payment.NewPayment(id.New(), cctx.Order.ID, provider.Method(), cctx.Order.TotalAmount)
	if err != nil {
		return fmt.Errorf("initiate_payment: create payment: %w", err)
	}

	if err := s.payments.Create(context.Background(), &py); err != nil {
		return fmt.Errorf("initiate_payment: save payment: %w", err)
	}

	prevUpdatedAt := py.UpdatedAt

	result, err := provider.Initiate(context.Background(), &py)
	if err != nil {
		return fmt.Errorf("initiate_payment: provider error: %w", err)
	}

	if result.Pending {
		// Async provider (e.g. Stripe): payment stays pending until webhook.
		if result.ProviderRef == "" {
			return fmt.Errorf("initiate_payment: pending result has no provider reference")
		}
		py.ProviderRef = result.ProviderRef
		py.UpdatedAt = time.Now().UTC()
		if err := s.payments.UpdateStatus(context.Background(), &py, prevUpdatedAt); err != nil {
			return fmt.Errorf("initiate_payment: update status: %w", err)
		}
		cctx.SetMeta("payment", &py)
		cctx.SetMeta("payment_initiated", true)
		if result.ClientSecret != "" {
			cctx.SetMeta("client_secret", result.ClientSecret)
		}
		return nil
	}

	if result.Success {
		if err := py.Complete(result.ProviderRef); err != nil {
			return fmt.Errorf("initiate_payment: complete: %w", err)
		}
	} else {
		if err := py.Fail(); err != nil {
			return fmt.Errorf("initiate_payment: fail: %w", err)
		}
	}

	if err := s.payments.UpdateStatus(context.Background(), &py, prevUpdatedAt); err != nil {
		return fmt.Errorf("initiate_payment: update status: %w", err)
	}

	cctx.SetMeta("payment", &py)
	cctx.SetMeta("payment_initiated", true)

	if !result.Success {
		return fmt.Errorf("initiate_payment: payment declined")
	}

	return nil
}
