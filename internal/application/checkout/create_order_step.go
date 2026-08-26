package checkout

import (
	"context"
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

type storeCreditService interface {
	GetBalance(ctx context.Context, customerID, currency string) (shared.Money, error)
	Redeem(ctx context.Context, customerID, orderID string, amount shared.Money) error
	Issue(ctx context.Context, customerID string, amount shared.Money, note, idempotencyKey string) error
}

type extensionSnapshotter interface {
	SnapshotCartItemToOrderItem(ctx context.Context, cartID, orderID, variantID, updatedBy string) error
}

// CreateOrderStep builds and persists an order from the cart and pricing snapshot.
type CreateOrderStep struct {
	orders     order.OrderRepository
	variants   catalog.VariantRepository
	credits    storeCreditService
	extensions extensionSnapshotter
}

// NewCreateOrderStep creates a CreateOrderStep.
func NewCreateOrderStep(orders order.OrderRepository, variants catalog.VariantRepository, credits storeCreditService, extensions extensionSnapshotter) *CreateOrderStep {
	if orders == nil {
		panic("checkout: orders must not be nil")
	}
	if variants == nil {
		panic("checkout: variants must not be nil")
	}
	return &CreateOrderStep{orders: orders, variants: variants, credits: credits, extensions: extensions}
}

func (s *CreateOrderStep) Name() string { return "create_order" }

// Execute creates an order with items sourced from the pricing snapshot.
// Sets cctx.Order and stores order ID in Meta["created_order_id"].
func (s *CreateOrderStep) Execute(ctx context.Context, cctx *Context) error {
	if cctx == nil {
		return fmt.Errorf("create_order: checkout context must not be nil")
	}
	if v, ok := cctx.GetMeta("created_order_id"); ok {
		if _, isStr := v.(string); isStr && v.(string) != "" {
			return nil // idempotent
		}
	}

	if cctx.Cart == nil {
		return fmt.Errorf("create_order: cart not loaded")
	}

	raw, ok := cctx.GetMeta("pricing")
	if !ok {
		return fmt.Errorf("create_order: pricing context not found in meta")
	}
	pctx, ok := raw.(*pricing.PricingContext)
	if !ok {
		return fmt.Errorf("create_order: invalid pricing context type")
	}

	priceByVariant := make(map[string]pricing.PricingItem, len(pctx.Items))
	for _, pi := range pctx.Items {
		priceByVariant[pi.VariantID] = pi
	}

	items := make([]order.Item, 0, len(cctx.Cart.Items))
	for _, ci := range cctx.Cart.Items {
		pi, found := priceByVariant[ci.VariantID]
		if !found {
			return fmt.Errorf("create_order: no pricing for variant %s", ci.VariantID)
		}

		v, err := s.variants.FindByID(ctx, ci.VariantID)
		if err != nil {
			return fmt.Errorf("create_order: lookup variant %s: %w", ci.VariantID, err)
		}
		if v == nil {
			return fmt.Errorf("create_order: variant %s not found", ci.VariantID)
		}

		oi, err := order.NewItem(ci.VariantID, v.SKU, v.Name, ci.Quantity, pi.UnitPrice)
		if err != nil {
			return fmt.Errorf("create_order: item %s: %w", ci.VariantID, err)
		}
		items = append(items, oi)
	}

	o, err := order.NewOrder(id.New(), cctx.CustomerID, cctx.Input.ContactEmail, cctx.Currency, items)
	if err != nil {
		return fmt.Errorf("create_order: %w", err)
	}

	if err := o.SetTaxSnapshot(cctx.Input.Address.Country, pctx.TaxTotal); err != nil {
		return fmt.Errorf("create_order: tax snapshot: %w", err)
	}

	if err := o.SetTaxSnapshot(cctx.Input.Address.Country, pctx.TaxTotal); err != nil {
		return fmt.Errorf("create_order: tax snapshot: %w", err)
	}

	appliedCredit, err := s.applyStoreCredit(ctx, cctx, &o)
	if err != nil {
		return err
	}

	if err := s.orders.Save(ctx, &o); err != nil {
		if appliedCredit != nil && s.credits != nil {
			rbctx, rbcancel := detachedTimeout(ctx, compensateTimeout)
			// No idempotency key here — see the package-level comment
			// above applyStoreCredit for why neither o.ID (changes every
			// attempt, so it can never collide) nor cctx.CartID (stable,
			// but unsafe: see that comment) is a correct choice without a
			// bigger fix this rollback call alone can't provide.
			rollbackErr := s.credits.Issue(rbctx, cctx.CustomerID, *appliedCredit, fmt.Sprintf("create_order rollback: order save failed (%s)", o.ID), "")
			rbcancel()
			if rollbackErr != nil {
				return fmt.Errorf("create_order: save: %w (store credit rollback failed: %v)", err, rollbackErr)
			}
		}
		return fmt.Errorf("create_order: save: %w", err)
	}

	cctx.Order = &o
	cctx.SetMeta("created_order_id", o.ID)

	if s.extensions != nil {
		updatedBy := strings.TrimSpace(cctx.CustomerID)
		if updatedBy == "" {
			updatedBy = "system"
		}
		// Order already committed — snapshot even if the request ctx is canceled.
		snapCtx, snapCancel := detachedTimeout(ctx, compensateTimeout)
		defer snapCancel()
		for _, ci := range cctx.Cart.Items {
			if err := s.extensions.SnapshotCartItemToOrderItem(snapCtx, cctx.Cart.ID, o.ID, ci.VariantID, updatedBy); err != nil {
				return fmt.Errorf("create_order: snapshot extensions for variant %s: %w", ci.VariantID, err)
			}
		}
	}

	return nil
}

// applyStoreCredit redeems store credit toward o's total, with a
// compensating Issue if applying it to the order subsequently fails (see
// Execute's own rollback on Save failure, above).
//
// Neither Redeem nor these compensating Issue calls carry idempotency
// protection against a process crash between Redeem committing and the
// compensating/final step completing — a retried checkout after such a
// crash can double-debit the customer. This was flagged in review; a naive
// fix (key Redeem/the rollback Issue by a stable identifier like
// cctx.CartID, since a cart is single-use and its ID doesn't change across
// retries the way a freshly generated order ID does) is NOT safe, and
// actively introduces a worse bug: once ANY key is reused across two
// GENUINELY DIFFERENT attempts on the same cart (e.g. attempt 1 redeems,
// Save fails, rollback correctly refunds — then the customer retries and
// attempt 2 redeems again), the second Redeem would be deduplicated
// against the first (now-already-refunded) one and silently skip debiting
// the balance at all, while the order still records the store credit as
// applied — the customer gets a real discount that was never actually
// paid for, on every legitimate retry-after-failure, not just crashes.
//
// A correct fix needs to distinguish "retry of the exact same in-flight
// attempt" (same key must dedupe) from "a new attempt after the previous
// one was fully resolved, successfully or via rollback" (must NOT dedupe)
// — which requires state that survives a crash and is explicitly
// invalidated when an attempt resolves, e.g. a pending-order-id persisted
// on the cart record itself, set before Redeem and cleared after Save
// succeeds or a rollback completes. That's a real feature (schema change
// + repo changes + careful ordering), not a one-line idempotency key, so
// it's deliberately not implemented here — see the follow-up task this
// review generated instead of guessing at that design under review
// pressure.
func (s *CreateOrderStep) applyStoreCredit(ctx context.Context, cctx *Context, o *order.Order) (*shared.Money, error) {
	requested := cctx.Input.StoreCreditAmount
	if requested <= 0 {
		return nil, nil
	}
	if cctx.CustomerID == "" {
		return nil, apperror.Validation("store credit requires an authenticated customer")
	}
	if s.credits == nil {
		return nil, apperror.Validation("store credit is not available")
	}

	balance, err := s.credits.GetBalance(ctx, cctx.CustomerID, cctx.Currency)
	if err != nil {
		return nil, fmt.Errorf("create_order: store credit balance: %w", err)
	}

	applyAmount := requested
	if balance.Amount() < applyAmount {
		applyAmount = balance.Amount()
	}
	if o.TotalAmount.Amount() < applyAmount {
		applyAmount = o.TotalAmount.Amount()
	}
	if applyAmount <= 0 {
		return nil, nil
	}

	creditMoney, err := shared.NewMoney(applyAmount, cctx.Currency)
	if err != nil {
		return nil, fmt.Errorf("create_order: store credit amount: %w", err)
	}
	if err := s.credits.Redeem(ctx, cctx.CustomerID, o.ID, creditMoney); err != nil {
		return nil, fmt.Errorf("create_order: redeem store credit: %w", err)
	}
	if err := o.ApplyStoreCredit(creditMoney); err != nil {
		rbctx, rbcancel := detachedTimeout(ctx, compensateTimeout)
		// No idempotency key — see this function's doc comment above.
		rollbackErr := s.credits.Issue(rbctx, cctx.CustomerID, creditMoney, fmt.Sprintf("create_order rollback: apply failed (%s)", o.ID), "")
		rbcancel()
		if rollbackErr != nil {
			return nil, fmt.Errorf("create_order: apply store credit: %w (rollback failed: %v)", err, rollbackErr)
		}
		return nil, fmt.Errorf("create_order: apply store credit: %w", err)
	}
	return &creditMoney, nil
}
