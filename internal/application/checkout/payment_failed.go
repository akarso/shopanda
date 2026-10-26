package checkout

import (
	"context"
	"fmt"
	"strings"

	assignmentapp "github.com/akarso/shopanda/internal/application/assignment"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/event"
)

const paymentFailedCreditKeyPrefix = "checkout:payment-failed:"

func paymentFailedCreditKey(orderID string) string {
	return paymentFailedCreditKeyPrefix + orderID
}

func paymentFailedCreditNote(orderID string) string {
	return "checkout: restore store credit for failed payment (" + orderID + ")"
}

// RegisterPaymentFailedCompensation fails a still-pending order, issues
// StoreCreditApplied back once (idempotency key tied to the order id),
// and revokes current assignments when payment.failed is published.
func RegisterPaymentFailedCompensation(
	bus *event.Bus,
	orders unpaidOrderWriter,
	credits storeCreditIssuer,
	assignments *assignmentapp.Flow,
) {
	if bus == nil {
		return
	}
	bus.On(payment.EventPaymentFailed, func(ctx context.Context, evt event.Event) error {
		data, ok := evt.Data.(payment.PaymentStatusChangedData)
		if !ok || strings.TrimSpace(data.OrderID) == "" {
			return nil
		}
		return compensateFailedPayment(ctx, orders, credits, assignments, data.OrderID)
	})
}

func compensateFailedPayment(
	ctx context.Context,
	orders unpaidOrderWriter,
	credits storeCreditIssuer,
	assignments *assignmentapp.Flow,
	orderID string,
) error {
	persistCtx, persistCancel := detachedTimeout(ctx, compensateTimeout)
	defer persistCancel()

	var failErr, creditErr, revokeErr error
	var o *order.Order
	if orders != nil {
		found, err := orders.FindByID(persistCtx, orderID)
		if err != nil {
			failErr = fmt.Errorf("checkout: payment.failed lookup order: %w", err)
		} else {
			o = found
		}
		if failErr == nil && o != nil && o.Status() == order.OrderStatusPending {
			if err := o.Fail(); err != nil {
				failErr = fmt.Errorf("checkout: payment.failed fail order: %w", err)
			} else if err := orders.UpdateStatus(persistCtx, o); err != nil {
				failErr = fmt.Errorf("checkout: payment.failed persist order: %w", err)
			}
		}
		if failErr == nil && o != nil && o.Status() == order.OrderStatusFailed {
			customerID := strings.TrimSpace(o.CustomerID)
			creditErr = issueFailedPaymentCredit(persistCtx, credits, customerID, o.ID, o.StoreCreditApplied)
		}
	}
	if assignments != nil {
		revokeErr = assignments.RevokeForOrder(persistCtx, orderID)
	}
	if failErr != nil && creditErr != nil && revokeErr != nil {
		return fmt.Errorf("%w (store credit: %v; revoke: %v)", failErr, creditErr, revokeErr)
	}
	if failErr != nil && creditErr != nil {
		return fmt.Errorf("%w (store credit: %v)", failErr, creditErr)
	}
	if failErr != nil && revokeErr != nil {
		return fmt.Errorf("%w (revoke: %v)", failErr, revokeErr)
	}
	if creditErr != nil && revokeErr != nil {
		return fmt.Errorf("checkout: payment.failed store credit: %w (revoke: %v)", creditErr, revokeErr)
	}
	if failErr != nil {
		return failErr
	}
	if creditErr != nil {
		return fmt.Errorf("checkout: payment.failed store credit: %w", creditErr)
	}
	return revokeErr
}

func issueFailedPaymentCredit(
	ctx context.Context,
	credits storeCreditIssuer,
	customerID, orderID string,
	applied shared.Money,
) error {
	if credits == nil || applied.Amount() <= 0 {
		return nil
	}
	if strings.TrimSpace(customerID) == "" {
		return fmt.Errorf("checkout: restore store credit: customer id is empty")
	}
	return credits.Restore(ctx, customerID, applied, paymentFailedCreditNote(orderID), paymentFailedCreditKey(orderID))
}
