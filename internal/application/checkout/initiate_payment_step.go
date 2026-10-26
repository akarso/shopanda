package checkout

import (
	"context"
	"fmt"
	"time"

	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

// InitiatePaymentStep creates a payment record and initiates it via the selected provider.
type InitiatePaymentStep struct {
	providers   *payment.ProviderRegistry
	payments    payment.PaymentRepository
	assignments *assignmentapp.Flow
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

// WithAssignmentFlow revokes current rows for this order when payment
// does not succeed (decline or provider error after Create).
func (s *InitiatePaymentStep) WithAssignmentFlow(flow *assignmentapp.Flow) *InitiatePaymentStep {
	if s == nil {
		return nil
	}
	s.assignments = flow
	return s
}

func (s *InitiatePaymentStep) Name() string { return "initiate_payment" }

func (s *InitiatePaymentStep) Execute(ctx context.Context, cctx *Context) error {
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

	if v, ok := cctx.GetMeta("payment_skipped"); ok {
		if b, isBool := v.(bool); isBool && b {
			cctx.SetMeta("payment_initiated", true)
			return nil
		}
	}

	payable, err := cctx.Order.PayableAmount()
	if err != nil {
		return fmt.Errorf("initiate_payment: payable amount: %w", err)
	}
	if payable.Amount() == 0 {
		cctx.SetMeta("payment_initiated", true)
		cctx.SetMeta("payment_skipped", true)
		return nil
	}

	provider, err := s.providers.Resolve(cctx.Input.PaymentMethod)
	if err != nil {
		return apperror.Validation("selected payment method is unavailable")
	}

	py, err := payment.NewPayment(id.New(), cctx.Order.ID, provider.Method(), payable)
	if err != nil {
		return fmt.Errorf("initiate_payment: create payment: %w", err)
	}

	if err := s.payments.Create(ctx, &py); err != nil {
		return fmt.Errorf("initiate_payment: save payment: %w", err)
	}

	prevUpdatedAt := py.UpdatedAt

	result, err := provider.Initiate(ctx, &py)
	if err != nil {
		if revErr := s.revokeAssignments(ctx, cctx); revErr != nil {
			return fmt.Errorf("initiate_payment: provider error: %w (revoke: %v)", err, revErr)
		}
		return fmt.Errorf("initiate_payment: provider error: %w", err)
	}

	if result.Pending {
		// Async provider (e.g. Stripe): payment stays pending until webhook.
		if result.ProviderRef == "" {
			return fmt.Errorf("initiate_payment: pending result has no provider reference")
		}
		py.ProviderRef = result.ProviderRef
		py.UpdatedAt = time.Now().UTC()
		// Provider already accepted the charge. Record that before
		// UpdateStatus so a persist error does not look like "no charge"
		// to checkout compensation.
		cctx.SetMeta("payment", &py)
		cctx.SetMeta("payment_initiated", true)
		if result.ClientSecret != "" {
			cctx.SetMeta("client_secret", result.ClientSecret)
		}
		persistCtx, persistCancel := detachedTimeout(ctx, compensateTimeout)
		err := s.payments.UpdateStatus(persistCtx, &py, prevUpdatedAt)
		persistCancel()
		if err != nil {
			return fmt.Errorf("initiate_payment: update status: %w", err)
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

	persistCtx, persistCancel := detachedTimeout(ctx, compensateTimeout)
	err = s.payments.UpdateStatus(persistCtx, &py, prevUpdatedAt)
	persistCancel()
	if err != nil {
		return fmt.Errorf("initiate_payment: update status: %w", err)
	}

	cctx.SetMeta("payment", &py)
	cctx.SetMeta("payment_initiated", true)

	if !result.Success {
		if revErr := s.revokeAssignments(ctx, cctx); revErr != nil {
			return fmt.Errorf("initiate_payment: payment declined (revoke: %v)", revErr)
		}
		return fmt.Errorf("initiate_payment: payment declined")
	}

	return nil
}

func (s *InitiatePaymentStep) revokeAssignments(ctx context.Context, cctx *Context) error {
	if s == nil || s.assignments == nil || cctx == nil || cctx.Order == nil {
		return nil
	}
	persistCtx, persistCancel := detachedTimeout(ctx, compensateTimeout)
	err := s.assignments.RevokeForOrder(persistCtx, cctx.Order.ID)
	persistCancel()
	return err
}
