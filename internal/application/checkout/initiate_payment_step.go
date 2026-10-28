package checkout

import (
	"context"
	"fmt"
	"time"

	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	orderapp "github.com/akarso/shopanda/internal/application/order"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// InitiatePaymentStep creates a payment record and initiates it via the selected provider.
type InitiatePaymentStep struct {
	providers   *payment.ProviderRegistry
	payments    payment.PaymentRepository
	assignments *assignmentapp.Flow
	bus         *event.Bus
	log         logger.Logger
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

// WithEventBus publishes order.paid on sync payment success and on the
// $0 / payment_skipped path (PR-1075). Pending Stripe does not publish —
// the payment.completed webhook does after capture. Nil skips publish.
func (s *InitiatePaymentStep) WithEventBus(bus *event.Bus) *InitiatePaymentStep {
	if s == nil {
		return nil
	}
	s.bus = bus
	return s
}

// WithLogger records order.paid publish failures after money has already
// settled (so the step can return nil without leaving grants unissued forever
// when a later retry republishes). Nil skips logging.
func (s *InitiatePaymentStep) WithLogger(log logger.Logger) *InitiatePaymentStep {
	if s == nil {
		return nil
	}
	s.log = log
	return s
}

func (s *InitiatePaymentStep) Name() string { return "initiate_payment" }

func (s *InitiatePaymentStep) Execute(ctx context.Context, cctx *Context) error {
	if cctx == nil {
		return fmt.Errorf("initiate_payment: checkout context must not be nil")
	}
	if cctx.Order == nil {
		return fmt.Errorf("initiate_payment: order not created yet")
	}

	if v, ok := cctx.GetMeta("payment_initiated"); ok {
		if b, isBool := v.(bool); isBool && b {
			// Already-paid / $0 heal: republish so a prior publish failure
			// (or crashed subscriber) can still issue grants. Pending Stripe
			// leaves payment pending and must not publish here.
			if skipped, ok := cctx.GetMeta("payment_skipped"); ok {
				if sb, isBool := skipped.(bool); isBool && sb {
					return s.publishOrderPaid(ctx, cctx)
				}
			}
			if raw, ok := cctx.GetMeta("payment"); ok {
				if py, isPay := raw.(*payment.Payment); isPay && py != nil && py.Status() == payment.StatusCompleted {
					return s.publishOrderPaid(ctx, cctx)
				}
			}
			return nil
		}
	}

	if v, ok := cctx.GetMeta("payment_skipped"); ok {
		if b, isBool := v.(bool); isBool && b {
			cctx.SetMeta("payment_initiated", true)
			// Re-publish so a crashed first skip can still grant (idempotent).
			return s.publishOrderPaid(ctx, cctx)
		}
	}

	payable, err := cctx.Order.PayableAmount()
	if err != nil {
		return fmt.Errorf("initiate_payment: payable amount: %w", err)
	}
	if payable.Amount() == 0 {
		cctx.SetMeta("payment_initiated", true)
		cctx.SetMeta("payment_skipped", true)
		return s.publishOrderPaid(ctx, cctx)
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

	// Record the provider outcome before UpdateStatus so a persist error
	// after a synchronous approve does not look like "no charge."
	cctx.SetMeta("payment", &py)
	cctx.SetMeta("payment_initiated", true)

	persistCtx, persistCancel := detachedTimeout(ctx, compensateTimeout)
	err = s.payments.UpdateStatus(persistCtx, &py, prevUpdatedAt)
	persistCancel()
	if err != nil {
		return fmt.Errorf("initiate_payment: update status: %w", err)
	}

	if !result.Success {
		if revErr := s.revokeAssignments(ctx, cctx); revErr != nil {
			return fmt.Errorf("initiate_payment: payment declined (revoke: %v)", revErr)
		}
		return fmt.Errorf("initiate_payment: payment declined")
	}

	return s.publishOrderPaid(ctx, cctx)
}

func (s *InitiatePaymentStep) publishOrderPaid(ctx context.Context, cctx *Context) error {
	if s == nil || s.bus == nil || cctx == nil || cctx.Order == nil {
		return nil
	}
	old := string(cctx.Order.Status())
	if err := orderapp.PublishPaid(ctx, s.bus, "checkout.initiate_payment", cctx.Order.ID, old, string(order.OrderStatusPaid)); err != nil {
		// Money / $0 path already succeeded — returning the error would make
		// step retry a no-op (payment_initiated set) leaving grants unissued
		// without a heal. Log and succeed; already-paid branches republish.
		if s.log != nil {
			s.log.Error("initiate_payment.publish_order_paid_failed", err, map[string]interface{}{
				"order_id": cctx.Order.ID,
			})
		}
		return nil
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
