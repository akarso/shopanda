package checkout

import (
	"context"
	"fmt"
	"time"

	catalogapp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
)

// ReservationTTL is the default duration for inventory reservations.
const ReservationTTL = 15 * time.Minute

// reserveTimeout bounds the duration of Reserve/Release repository calls.
const reserveTimeout = 30 * time.Second

// ReserveInventoryStep creates inventory reservations for each cart item.
type ReserveInventoryStep struct {
	reservations inventory.ReservationRepository
	products     catalog.ProductRepository
	variants     catalog.VariantRepository
	components   catalog.BundleComponentRepository
	ttl          time.Duration

	// bus is set only by WithStockEventPublishing. variants is always set by
	// the constructor so a dynamic bundle can resolve component variants even
	// when stock events are off.
	bus *event.Bus
}

// ReserveOption configures a ReserveInventoryStep.
type ReserveOption func(*ReserveInventoryStep)

// WithTTL sets a custom reservation TTL (default: ReservationTTL).
func WithTTL(ttl time.Duration) ReserveOption {
	return func(s *ReserveInventoryStep) { s.ttl = ttl }
}

// WithStockEventPublishing enables publishing inventory.EventStockUpdated
// after each successful Reserve and Release (PR-1049: checkout is the
// highest-frequency stock-changing path in the system, and PR-1036's
// on-save search subscriber only ever had this wired for the interactive
// admin single-variant "Adjust" endpoint — so real customer purchases were
// leaving the search index's stock-derived fields stale until this).
// The constructor's variant repository resolves each reservation's VariantID
// to the ProductID/SKU the event payload needs; bus publishes it. Left unset
// (the default), this step publishes nothing — no event, no error — matching
// the optional-wiring pattern InventoryAdminHandler.SetBus already
// established. A nil bus panics rather than leaving incomplete stock-event
// configuration that would only fail later at publish time.
func WithStockEventPublishing(bus *event.Bus) ReserveOption {
	return func(s *ReserveInventoryStep) {
		if bus == nil {
			panic("checkout.WithStockEventPublishing: bus must not be nil")
		}
		s.bus = bus
	}
}

// NewReserveInventoryStep creates a ReserveInventoryStep.
// reservations, products, variants, and components are required. A dynamic
// bundle reserves each required component's leaf variant; every other product
// reserves the cart line's variant.
func NewReserveInventoryStep(
	reservations inventory.ReservationRepository,
	products catalog.ProductRepository,
	variants catalog.VariantRepository,
	components catalog.BundleComponentRepository,
	opts ...ReserveOption,
) *ReserveInventoryStep {
	if reservations == nil {
		panic("checkout: reservations must not be nil")
	}
	if products == nil {
		panic("checkout: products must not be nil")
	}
	if variants == nil {
		panic("checkout: variants must not be nil")
	}
	if components == nil {
		panic("checkout: bundle components must not be nil")
	}
	s := &ReserveInventoryStep{
		reservations: reservations,
		products:     products,
		variants:     variants,
		components:   components,
		ttl:          ReservationTTL,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *ReserveInventoryStep) Name() string { return "reserve_inventory" }

type reserveTarget struct {
	variantID       string
	quantity        int
	bundleProductID string
}

type heldReserve struct {
	res             inventory.Reservation
	bundleProductID string
}

// Execute reserves inventory for each cart item.
// Stores reservation IDs in Meta["reservations"].
// A dynamic-stock bundle reserves each required simple component for
// component.quantity * line quantity and does not reserve the bundle variant.
// Virtual and downloadable required leaves are skipped. A digital-only
// required set reserves nothing for that line.
// Every other line reserves item.VariantID for item.Quantity.
func (s *ReserveInventoryStep) Execute(ctx context.Context, cctx *Context) error {
	if cctx == nil {
		return fmt.Errorf("reserve_inventory: checkout context must not be nil")
	}
	if v, ok := cctx.GetMeta("reserved"); ok {
		if b, isBool := v.(bool); isBool && b {
			return nil // idempotency
		}
	}

	if cctx.Cart == nil {
		return fmt.Errorf("reserve_inventory: cart not loaded")
	}

	expiresAt := time.Now().UTC().Add(s.ttl)

	reservationIDs := make([]string, 0, len(cctx.Cart.Items))
	reserved := make([]heldReserve, 0, len(cctx.Cart.Items))
	for _, item := range cctx.Cart.Items {
		targets, err := s.reserveTargets(ctx, cctx, item)
		if err != nil {
			s.releaseReserved(ctx, cctx, reserved)
			return err
		}
		var bundleProductID string
		for _, target := range targets {
			res, err := inventory.NewReservation(id.New(), target.variantID, target.quantity, expiresAt)
			if err != nil {
				s.releaseReserved(ctx, cctx, reserved)
				return fmt.Errorf("reserve_inventory: create reservation: %w", err)
			}
			rctx, rcancel := context.WithTimeout(ctx, reserveTimeout)
			rerr := s.reservations.Reserve(rctx, &res)
			rcancel()
			if rerr != nil {
				s.releaseReserved(ctx, cctx, reserved)
				if target.bundleProductID != "" {
					return bundleReserveError(target.bundleProductID, fmt.Errorf("variant %s: %w", target.variantID, rerr))
				}
				return fmt.Errorf("reserve_inventory: variant %s: %w", target.variantID, rerr)
			}
			reservationIDs = append(reservationIDs, res.ID)
			reserved = append(reserved, heldReserve{res: res, bundleProductID: target.bundleProductID})
			s.publishStockUpdated(ctx, cctx, res.VariantID, -res.Quantity)
			if target.bundleProductID != "" {
				bundleProductID = target.bundleProductID
			}
		}
		if bundleProductID != "" {
			s.publishParentStockUpdated(ctx, bundleProductID)
		}
	}

	cctx.SetMeta("reservations", reservationIDs)
	cctx.SetMeta("reserved", true)
	return nil
}

func (s *ReserveInventoryStep) releaseReserved(ctx context.Context, cctx *Context, reserved []heldReserve) {
	// Best-effort rollback: must not inherit a canceled request ctx or
	// prior reservations stay locked for ReservationTTL.
	parents := make(map[string]struct{})
	for _, held := range reserved {
		r := held.res
		rlctx, rlcancel := detachedTimeout(ctx, reserveTimeout)
		if releaseErr := s.reservations.Release(rlctx, r.ID); releaseErr == nil {
			s.publishStockUpdated(rlctx, cctx, r.VariantID, r.Quantity)
			if held.bundleProductID != "" {
				parents[held.bundleProductID] = struct{}{}
			}
		}
		rlcancel()
	}
	if len(parents) == 0 {
		return
	}
	pctx, pcancel := detachedTimeout(ctx, reserveTimeout)
	defer pcancel()
	for productID := range parents {
		s.publishParentStockUpdated(pctx, productID)
	}
}

func (s *ReserveInventoryStep) reserveTargets(ctx context.Context, cctx *Context, item cart.Item) ([]reserveTarget, error) {
	variant := cartVariantFromMeta(cctx, item.VariantID)
	if variant == nil {
		var err error
		variant, err = s.variants.FindByID(ctx, item.VariantID)
		if err != nil {
			return nil, fmt.Errorf("reserve_inventory: lookup variant %s: %w", item.VariantID, err)
		}
		if variant == nil {
			return nil, fmt.Errorf("reserve_inventory: variant %s no longer exists", item.VariantID)
		}
	}
	product := cartProductFromMeta(cctx, variant.ProductID)
	if product == nil {
		var err error
		product, err = s.products.FindByID(ctx, variant.ProductID)
		if err != nil {
			return nil, fmt.Errorf("reserve_inventory: lookup product %s: %w", variant.ProductID, err)
		}
		if product == nil {
			return nil, fmt.Errorf("reserve_inventory: product %s not found", variant.ProductID)
		}
	}
	if product.Type == catalog.TypeBundle && product.StockMode == catalog.StockModeDynamic {
		return s.dynamicBundleTargets(ctx, product.ID, item.Quantity)
	}
	return []reserveTarget{{variantID: item.VariantID, quantity: item.Quantity}}, nil
}

// dynamicBundleTargets reserves each required simple leaf for
// component.quantity * line quantity. Optional components are ignored.
// Virtual and downloadable required leaves are skipped, matching
// ResolveAvailableQty: they are not stock-gated and have no reservation
// target. A required set that is only digital returns an empty list and
// does not reserve the bundle variant. The bundle variant itself is never
// reserved. Leaf rules come from catalogapp.RequiredLeafVariants.
// Customer-facing failures use catalog.BundleCannotBeReservedMessage; the
// leaf-walk or component Reserve error stays wrapped for logs.
func (s *ReserveInventoryStep) dynamicBundleTargets(ctx context.Context, bundleProductID string, itemQty int) ([]reserveTarget, error) {
	leaves, err := catalogapp.RequiredLeafVariants(ctx, s.products, s.variants, s.components, bundleProductID)
	if err != nil {
		return nil, bundleReserveError(bundleProductID, err)
	}
	targets := make([]reserveTarget, 0, len(leaves))
	for _, leaf := range leaves {
		switch leaf.Product.Type {
		case catalog.TypeVirtual, catalog.TypeDownloadable:
			// Digital leaves are not stock-gated. They are not reserve targets.
			continue
		case catalog.TypeSimple:
			need := leaf.Component.Quantity * itemQty
			if itemQty < 1 || leaf.Component.Quantity < 1 || need/leaf.Component.Quantity != itemQty {
				return nil, bundleReserveError(bundleProductID, apperror.Validation(fmt.Sprintf(
					"dynamic bundle component %s reserve quantity is invalid",
					leaf.Component.ComponentProductID,
				)))
			}
			targets = append(targets, reserveTarget{
				variantID:       leaf.Variant.ID,
				quantity:        need,
				bundleProductID: bundleProductID,
			})
		default:
			return nil, bundleReserveError(bundleProductID, apperror.Validation(fmt.Sprintf(
				"dynamic bundle component %s must be a simple, virtual, or downloadable product",
				leaf.Component.ComponentProductID,
			)))
		}
	}
	return targets, nil
}

func bundleReserveError(bundleProductID string, err error) error {
	if apperror.Is(err, apperror.CodeValidation) || apperror.Is(err, apperror.CodeNotFound) || apperror.Is(err, apperror.CodeConflict) {
		return catalogapp.BundleCustomerError(err)
	}
	return fmt.Errorf("reserve_inventory: bundle %s: %w", bundleProductID, err)
}

// publishStockUpdated is a best-effort side channel, not part of this
// step's actual business logic: a no-op when WithStockEventPublishing
// wasn't used, and any failure (variant lookup, bus.Publish itself) is
// swallowed rather than failing checkout over a stale search index entry
// — the same tradeoff the rollback loop above already makes for its own
// Release calls, and this step has no logger of its own to at least
// record the swallowed error (unlike, e.g., returns.Service).
//
// Checkout is the highest-frequency stock-changing path in the system, so
// this reuses the variant ValidateCartStep already resolved (via cctx's
// cart_variants meta) instead of re-querying VariantRepository per item —
// a step or two earlier in the same pipeline, ValidateCartStep already
// paid for exactly this lookup to confirm the variant still exists.
// Falls back to a direct FindByID only if that metadata is absent (e.g. a
// caller wiring this step without ValidateCartStep ahead of it, as some
// tests do) — never a correctness dependency, just an optimization.
// Component reservations are not in that map; those fall back to FindByID.
// A dynamic bundle also publishes one event for the parent product so search
// can refresh the rollup. That parent event carries ProductID only.
//
// delta is the signed size of the change (negative for Reserve, positive
// for a rollback Release), not GetStock's resulting on-hand total.
// Fetching the post-change total here would cost an extra query per cart
// item purely for a field HandleStockUpdated (this event's only current
// consumer) doesn't read — it schedules a reindex by ProductID alone,
// which re-fetches the product's real current stock independently.
func (s *ReserveInventoryStep) publishStockUpdated(ctx context.Context, cctx *Context, variantID string, delta int) {
	if s.bus == nil {
		return
	}
	variant := cartVariantFromMeta(cctx, variantID)
	if variant == nil {
		var err error
		variant, err = s.variants.FindByID(ctx, variantID)
		if err != nil || variant == nil {
			return
		}
	}
	_ = s.bus.Publish(ctx, event.New(inventory.EventStockUpdated, "checkout.reserve_inventory", inventory.StockUpdatedData{
		ProductID: variant.ProductID,
		VariantID: variant.ID,
		SKU:       variant.SKU,
		Delta:     inventory.Qty(delta),
	}))
}

func (s *ReserveInventoryStep) publishParentStockUpdated(ctx context.Context, productID string) {
	if s.bus == nil || productID == "" {
		return
	}
	_ = s.bus.Publish(ctx, event.New(inventory.EventStockUpdated, "checkout.reserve_inventory", inventory.StockUpdatedData{
		ProductID: productID,
	}))
}
