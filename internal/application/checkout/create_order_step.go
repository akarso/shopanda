package checkout

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	catalogapp "github.com/akarso/shopanda/internal/application/catalog"
	pricingapp "github.com/akarso/shopanda/internal/application/pricing"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// OrderLinesCouldNotBeCreatedMessage is the customer-facing text when exploded
// order lines cannot be built. Variant and component ids stay on Unwrap.
const OrderLinesCouldNotBeCreatedMessage = "order lines could not be created"

type storeCreditService interface {
	GetBalance(ctx context.Context, customerID, currency string) (shared.Money, error)
	Redeem(ctx context.Context, customerID, orderID string, amount shared.Money) error
	Issue(ctx context.Context, customerID string, amount shared.Money, note, idempotencyKey string) error
}

type extensionSnapshotter interface {
	SnapshotCartItemToOrderItem(ctx context.Context, cartID, orderID, variantID, updatedBy string) error
}

// CreateOrderStep builds and persists an order from the cart and pricing snapshot.
// orders, variants, products, components, and prices are required. A nil
// dependency panics at construction so a bundle with ExplodeOrderLines cannot
// silently stay one parent line. credits, extensions, and reservations may be
// nil. reservations releases Meta reservations when Execute fails before the
// order is saved.
type CreateOrderStep struct {
	orders       order.OrderRepository
	variants     catalog.VariantRepository
	credits      storeCreditService
	extensions   extensionSnapshotter
	products     catalog.ProductRepository
	components   catalog.BundleComponentRepository
	prices       pricing.PriceRepository
	reservations inventory.ReservationRepository
	bus          *event.Bus
	log          logger.Logger
}

// WithReservations releases checkout reservations when Execute fails before
// the order is saved. Nil skips that best-effort release.
func (s *CreateOrderStep) WithReservations(reservations inventory.ReservationRepository) *CreateOrderStep {
	if s == nil {
		return nil
	}
	s.reservations = reservations
	return s
}

// WithStockEventPublishing publishes inventory.EventStockUpdated when a
// failed order releases reservations, using the same variant and parent
// rollup path as ReserveInventoryStep. Release failures are logged.
func (s *CreateOrderStep) WithStockEventPublishing(bus *event.Bus, log logger.Logger) *CreateOrderStep {
	if s == nil {
		return nil
	}
	if bus == nil {
		panic("checkout: stock event bus must not be nil")
	}
	if log == nil {
		panic("checkout: logger must not be nil")
	}
	s.bus = bus
	s.log = log
	return s
}

// NewCreateOrderStep creates a CreateOrderStep.
// orders, variants, products, components, and prices must be non-nil.
// Production wiring passes the product, component, and price repositories.
func NewCreateOrderStep(
	orders order.OrderRepository,
	variants catalog.VariantRepository,
	credits storeCreditService,
	extensions extensionSnapshotter,
	products catalog.ProductRepository,
	components catalog.BundleComponentRepository,
	prices pricing.PriceRepository,
) *CreateOrderStep {
	if orders == nil {
		panic("checkout: orders must not be nil")
	}
	if variants == nil {
		panic("checkout: variants must not be nil")
	}
	if products == nil {
		panic("checkout: products must not be nil")
	}
	if components == nil {
		panic("checkout: bundle components must not be nil")
	}
	if prices == nil {
		panic("checkout: prices must not be nil")
	}
	return &CreateOrderStep{
		orders:     orders,
		variants:   variants,
		credits:    credits,
		extensions: extensions,
		products:   products,
		components: components,
		prices:     prices,
	}
}

func (s *CreateOrderStep) Name() string { return "create_order" }

// Execute creates an order with items sourced from the pricing snapshot.
// Sets cctx.Order and stores order ID in Meta["created_order_id"].
// A failure before that id is stored releases Meta["reservations"] when
// reserve_inventory set Meta["reserved"] and a reservation repository is wired.
func (s *CreateOrderStep) Execute(ctx context.Context, cctx *Context) (err error) {
	defer func() {
		if err != nil {
			s.releaseReservations(ctx, cctx)
		}
	}()
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
	storeID := pricingStoreID(cctx, pctx)

	groups := make([][]order.Item, 0, len(cctx.Cart.Items))
	anyExploded := false
	explodedReserved := make(map[string]int)
	kept := make([]cart.Item, 0, len(cctx.Cart.Items))
	for _, ci := range cctx.Cart.Items {
		pi, found := priceByVariant[ci.VariantID]
		if !found {
			return orderLinesValidation(fmt.Sprintf("no pricing for variant %s", ci.VariantID))
		}
		lines, exploded, err := s.linesForCartItem(ctx, cctx, ci, pi, storeID)
		if err != nil {
			return err
		}
		if exploded {
			anyExploded = true
			for _, item := range lines {
				if item.Restockable {
					explodedReserved[item.VariantID] += item.Quantity
				}
			}
		} else {
			kept = append(kept, ci)
		}
		groups = append(groups, lines)
	}
	items, err := mergeOrderItems(groups)
	if err != nil {
		return err
	}
	if err := s.assertExplodedReservations(ctx, cctx, anyExploded, explodedReserved, kept); err != nil {
		return err
	}

	o, err := order.NewOrder(id.New(), cctx.CustomerID, cctx.Input.ContactEmail, cctx.Currency, items)
	if err != nil {
		return fmt.Errorf("create_order: %w", err)
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
		// An exploded cart parent has no order line with that variant id.
		// Skip it so the snapshot is not an orphan pointing at a line that
		// was never written. Other cart lines still snapshot.
		snapCtx, snapCancel := detachedTimeout(ctx, compensateTimeout)
		defer snapCancel()
		orderVariants := make(map[string]struct{}, len(items))
		for _, item := range items {
			orderVariants[item.VariantID] = struct{}{}
		}
		for _, ci := range cctx.Cart.Items {
			if _, onOrder := orderVariants[ci.VariantID]; !onOrder {
				continue
			}
			if err := s.extensions.SnapshotCartItemToOrderItem(snapCtx, cctx.Cart.ID, o.ID, ci.VariantID, updatedBy); err != nil {
				return fmt.Errorf("create_order: snapshot extensions for variant %s: %w", ci.VariantID, err)
			}
		}
	}

	return nil
}

func pricingStoreID(cctx *Context, pctx *pricing.PricingContext) string {
	if pctx != nil && pctx.Meta != nil {
		if storeID, ok := pctx.Meta["store_id"].(string); ok && storeID != "" {
			return storeID
		}
	}
	if cctx != nil {
		if raw, ok := cctx.GetMeta("store_id"); ok {
			if storeID, ok := raw.(string); ok {
				return storeID
			}
		}
	}
	return ""
}

// linesForCartItem returns the order lines for one cart row and whether the
// parent line was exploded. A bundle with ExplodeOrderLines writes one line
// per required leaf and drops the parent. Every other row stays one line at
// the pricing snapshot unit price. Exploded line totals must equal the cart
// line total. Leaf unit prices come from one batch of stored component prices.
func (s *CreateOrderStep) linesForCartItem(ctx context.Context, cctx *Context, ci cart.Item, pi pricing.PricingItem, storeID string) ([]order.Item, bool, error) {
	v := cartVariantFromMeta(cctx, ci.VariantID)
	if v == nil {
		var err error
		v, err = s.variants.FindByID(ctx, ci.VariantID)
		if err != nil {
			return nil, false, fmt.Errorf("create_order: lookup variant %s: %w", ci.VariantID, err)
		}
		if v == nil {
			return nil, false, orderLinesValidation(fmt.Sprintf("variant %s not found", ci.VariantID))
		}
	}

	product := cartProductFromMeta(cctx, v.ProductID)
	if product == nil {
		var err error
		product, err = s.products.FindByID(ctx, v.ProductID)
		if err != nil {
			return nil, false, fmt.Errorf("create_order: lookup product %s: %w", v.ProductID, err)
		}
		if product == nil {
			return nil, false, orderLinesValidation(fmt.Sprintf("product %s not found", v.ProductID))
		}
	}
	if product.Type != catalog.TypeBundle || !product.ExplodeOrderLines {
		oi, err := order.NewItem(ci.VariantID, v.SKU, v.Name, ci.Quantity, pi.UnitPrice)
		if err != nil {
			return nil, false, orderLinesValidation(fmt.Sprintf("item %s: %v", ci.VariantID, err))
		}
		oi.Restockable = orderLineRestockable(product.Type)
		return []order.Item{oi}, false, nil
	}
	if product.PricingMode != catalog.PricingModeDynamic || product.StockMode != catalog.StockModeDynamic {
		return nil, false, apperror.Wrap(
			apperror.CodeValidation,
			catalog.ExplodeOrderLinesRequiresDynamicMessage,
			fmt.Errorf("product %s", product.ID),
		)
	}
	leaves, err := catalogapp.RequiredLeafVariants(ctx, s.products, s.variants, s.components, product.ID)
	if err != nil {
		if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) || apperror.Is(err, apperror.CodeConflict) {
			return nil, false, catalogapp.BundleCustomerError(err)
		}
		return nil, false, fmt.Errorf("create_order: %w", err)
	}
	variantIDs := make([]string, len(leaves))
	for i, leaf := range leaves {
		variantIDs[i] = leaf.Variant.ID
	}
	priced, err := pricingapp.LookupComponentPrices(ctx, s.prices, variantIDs, cctx.Currency, storeID)
	if err != nil {
		return nil, false, fmt.Errorf("create_order: component prices: %w", err)
	}
	leafLines := make([]order.Item, 0, len(leaves))
	for i, leaf := range leaves {
		qty, err := checkedMulQty(leaf.Component.Quantity, ci.Quantity)
		if err != nil {
			return nil, false, orderLinesValidation(fmt.Sprintf("component %s quantity: %v", leaf.Component.ComponentProductID, err))
		}
		price := priced[variantIDs[i]]
		if price == nil {
			return nil, false, orderLinesValidation(fmt.Sprintf("component %s has no price in %s", leaf.Component.ComponentProductID, cctx.Currency))
		}
		if price.Amount.Currency() != cctx.Currency {
			return nil, false, orderLinesValidation(fmt.Sprintf("component %s currency mismatch", leaf.Variant.ID))
		}
		item, err := order.NewItem(leaf.Variant.ID, leaf.Variant.SKU, leaf.Variant.Name, qty, price.Amount)
		if err != nil {
			return nil, false, orderLinesValidation(fmt.Sprintf("item %s: %v", leaf.Variant.ID, err))
		}
		if leaf.Product == nil {
			return nil, false, orderLinesValidation(fmt.Sprintf("component %s product is missing", leaf.Component.ComponentProductID))
		}
		item.Restockable = orderLineRestockable(leaf.Product.Type)
		leafLines = append(leafLines, item)
	}
	merged, err := mergeOrderItems([][]order.Item{leafLines})
	if err != nil {
		return nil, false, err
	}
	sum, err := shared.Zero(cctx.Currency)
	if err != nil {
		return nil, false, orderLinesValidation(err.Error())
	}
	for _, item := range merged {
		lineTotal, err := item.LineTotal()
		if err != nil {
			return nil, false, orderLinesValidation(fmt.Sprintf("line total %s: %v", item.VariantID, err))
		}
		sum, err = sum.AddChecked(lineTotal)
		if err != nil {
			return nil, false, orderLinesValidation(fmt.Sprintf("exploded total %s: %v", ci.VariantID, err))
		}
	}
	if !sum.Equal(pi.Total) {
		return nil, false, orderLinesValidation(fmt.Sprintf("exploded total for variant %s does not match cart line total", ci.VariantID))
	}
	return merged, true, nil
}

func orderLineRestockable(productType catalog.Type) bool {
	switch productType {
	case catalog.TypeVirtual, catalog.TypeDownloadable:
		return false
	default:
		return true
	}
}

func orderLinesValidation(detail string) error {
	return apperror.Wrap(apperror.CodeValidation, OrderLinesCouldNotBeCreatedMessage, errors.New(detail))
}

func mergeOrderItems(groups [][]order.Item) ([]order.Item, error) {
	merged := make([]order.Item, 0)
	index := make(map[string]int)
	for _, group := range groups {
		for _, item := range group {
			if i, ok := index[item.VariantID]; ok {
				if !merged[i].UnitPrice.Equal(item.UnitPrice) {
					return nil, orderLinesValidation(fmt.Sprintf("variant %s has conflicting unit prices", item.VariantID))
				}
				if merged[i].Restockable != item.Restockable {
					return nil, orderLinesValidation(fmt.Sprintf("variant %s has conflicting restockable flags", item.VariantID))
				}
				qty, err := checkedAddQty(merged[i].Quantity, item.Quantity)
				if err != nil {
					return nil, orderLinesValidation(fmt.Sprintf("variant %s: %v", item.VariantID, err))
				}
				merged[i].Quantity = qty
				continue
			}
			index[item.VariantID] = len(merged)
			merged = append(merged, item)
		}
	}
	return merged, nil
}

// assertExplodedReservations checks that Meta reserved quantities are what
// ReserveInventoryStep held. The check runs only when a cart line exploded
// and that meta is set. Exploded lines contribute restockable leaf
// quantities (simple products). Digital exploded leaves are not reserved.
// A non-exploded line contributes the same targets reserve uses: the cart
// variant and its quantity (including virtual and downloadable, which
// reserve still holds), or each simple leaf quantity of a dynamic-stock
// bundle rather than its parent. A quantity mismatch is validation.
func (s *CreateOrderStep) assertExplodedReservations(ctx context.Context, cctx *Context, anyExploded bool, explodedReserved map[string]int, kept []cart.Item) error {
	if !anyExploded || cctx == nil {
		return nil
	}
	raw, ok := cctx.GetMeta(ReservedVariantQtyMeta)
	if !ok {
		if reserved, exists := cctx.GetMeta("reserved"); exists {
			if flag, isBool := reserved.(bool); isBool && flag {
				return orderLinesValidation("reserved variant quantities are missing")
			}
		}
		return nil
	}
	got, ok := raw.(map[string]int)
	if !ok || got == nil {
		return orderLinesValidation("reserved variant quantities are missing")
	}
	want := make(map[string]int, len(explodedReserved)+len(kept))
	for id, qty := range explodedReserved {
		want[id] += qty
	}
	for _, ci := range kept {
		held, err := s.keptReservedQty(ctx, cctx, ci)
		if err != nil {
			return err
		}
		for id, qty := range held {
			want[id] += qty
		}
	}
	if !reservedQtyMatch(got, want) {
		return orderLinesValidation(fmt.Sprintf("reserved quantities %v do not match the quantities reserve held %v", want, got))
	}
	return nil
}

func reservedQtyMatch(got, want map[string]int) bool {
	if len(got) != len(want) {
		return false
	}
	for id, qty := range want {
		if got[id] != qty {
			return false
		}
	}
	return true
}

// keptReservedQty is the quantity map ReserveInventoryStep.reserveTargets
// stores for a cart line that was not exploded. A dynamic-stock bundle
// reserves each required simple leaf for component.quantity * line quantity
// and skips digital leaves. Every other line reserves the cart variant for
// the cart quantity, including virtual and downloadable.
func (s *CreateOrderStep) keptReservedQty(ctx context.Context, cctx *Context, ci cart.Item) (map[string]int, error) {
	v := cartVariantFromMeta(cctx, ci.VariantID)
	if v == nil {
		var err error
		v, err = s.variants.FindByID(ctx, ci.VariantID)
		if err != nil {
			return nil, fmt.Errorf("create_order: lookup variant %s: %w", ci.VariantID, err)
		}
		if v == nil {
			return nil, orderLinesValidation(fmt.Sprintf("variant %s not found", ci.VariantID))
		}
	}
	product := cartProductFromMeta(cctx, v.ProductID)
	if product == nil {
		var err error
		product, err = s.products.FindByID(ctx, v.ProductID)
		if err != nil {
			return nil, fmt.Errorf("create_order: lookup product %s: %w", v.ProductID, err)
		}
		if product == nil {
			return nil, orderLinesValidation(fmt.Sprintf("product %s not found", v.ProductID))
		}
	}
	if product.Type == catalog.TypeBundle && product.StockMode == catalog.StockModeDynamic {
		return s.dynamicBundleReservedQty(ctx, product.ID, ci.Quantity)
	}
	return map[string]int{ci.VariantID: ci.Quantity}, nil
}

func (s *CreateOrderStep) dynamicBundleReservedQty(ctx context.Context, bundleProductID string, itemQty int) (map[string]int, error) {
	leaves, err := catalogapp.RequiredLeafVariants(ctx, s.products, s.variants, s.components, bundleProductID)
	if err != nil {
		if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) || apperror.Is(err, apperror.CodeConflict) {
			return nil, catalogapp.BundleCustomerError(err)
		}
		return nil, fmt.Errorf("create_order: %w", err)
	}
	qty := make(map[string]int)
	for _, leaf := range leaves {
		if leaf.Product == nil {
			return nil, orderLinesValidation(fmt.Sprintf("component %s product is missing", leaf.Component.ComponentProductID))
		}
		switch leaf.Product.Type {
		case catalog.TypeVirtual, catalog.TypeDownloadable:
			continue
		case catalog.TypeSimple:
			need, err := checkedMulQty(leaf.Component.Quantity, itemQty)
			if err != nil {
				return nil, orderLinesValidation(fmt.Sprintf("component %s quantity: %v", leaf.Component.ComponentProductID, err))
			}
			qty[leaf.Variant.ID] += need
		default:
			return nil, catalogapp.BundleCustomerError(apperror.Validation(fmt.Sprintf(
				"dynamic bundle component %s must be a simple, virtual, or downloadable product",
				leaf.Component.ComponentProductID,
			)))
		}
	}
	return qty, nil
}

// releaseReservations returns stock held by this checkout when the order was
// not saved. created_order_id is set only after Save, and a later snapshot
// failure must keep the reservation for the order that already exists.
// Release uses the same publish path as ReserveInventoryStep: a failed
// Release is logged, and a successful one publishes EventStockUpdated for
// the variant and the dynamic-bundle parent when the hold recorded one.
func (s *CreateOrderStep) releaseReservations(ctx context.Context, cctx *Context) {
	if s == nil || s.reservations == nil || cctx == nil {
		return
	}
	if raw, ok := cctx.GetMeta("created_order_id"); ok {
		if id, isStr := raw.(string); isStr && id != "" {
			return
		}
	}
	reserved, ok := cctx.GetMeta("reserved")
	if !ok {
		return
	}
	flag, isBool := reserved.(bool)
	if !isBool || !flag {
		return
	}
	holds := reservationHoldsFromMeta(cctx)
	if holds == nil {
		rawIDs, ok := cctx.GetMeta("reservations")
		if !ok {
			return
		}
		ids, ok := rawIDs.([]string)
		if !ok {
			return
		}
		// No variant id and no quantity on this fallback. Release still runs.
		// releaseCheckoutHolds skips EventStockUpdated for an empty variant
		// or a zero quantity so the payload is not empty.
		holds = make([]ReservationHold, len(ids))
		for i, id := range ids {
			holds[i] = ReservationHold{ID: id}
		}
	}
	releaseCheckoutHolds(ctx, compensateTimeout, s.reservations, s.variants, s.bus, s.log, cctx, holds, "checkout.create_order")
}

func reservationHoldsFromMeta(cctx *Context) []ReservationHold {
	if cctx == nil {
		return nil
	}
	raw, ok := cctx.GetMeta(ReservationHoldsMeta)
	if !ok {
		return nil
	}
	holds, ok := raw.([]ReservationHold)
	if !ok {
		return nil
	}
	return holds
}

func checkedMulQty(a, b int) (int, error) {
	if a <= 0 || b <= 0 {
		return 0, fmt.Errorf("quantity must be positive")
	}
	if a > math.MaxInt/b {
		return 0, fmt.Errorf("quantity overflow")
	}
	return a * b, nil
}

func checkedAddQty(a, b int) (int, error) {
	if a <= 0 || b <= 0 {
		return 0, fmt.Errorf("quantity must be positive")
	}
	if a > math.MaxInt-b {
		return 0, fmt.Errorf("quantity overflow")
	}
	return a + b, nil
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
