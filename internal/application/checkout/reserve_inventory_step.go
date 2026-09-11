package checkout

import (
	"context"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
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
	ttl          time.Duration

	// variants and bus are both set together, only by WithStockEventPublishing
	// — see its own doc comment for why publishing inventory.EventStockUpdated
	// is optional here rather than a required constructor dependency.
	variants catalog.VariantRepository
	bus      *event.Bus
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
// variants resolves each reservation's VariantID to the ProductID/SKU the
// event payload needs; bus publishes it. Left unset (the default), this
// step works exactly as before — no event, no error, no new dependency —
// matching the optional-wiring pattern InventoryAdminHandler.SetBus
// already established. Both are set together, never independently: one
// without the other can't publish anything, so there is no partial state
// to guard against at publish time.
func WithStockEventPublishing(variants catalog.VariantRepository, bus *event.Bus) ReserveOption {
	return func(s *ReserveInventoryStep) {
		s.variants = variants
		s.bus = bus
	}
}

// NewReserveInventoryStep creates a ReserveInventoryStep.
func NewReserveInventoryStep(reservations inventory.ReservationRepository, opts ...ReserveOption) *ReserveInventoryStep {
	if reservations == nil {
		panic("checkout: reservations must not be nil")
	}
	s := &ReserveInventoryStep{reservations: reservations, ttl: ReservationTTL}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *ReserveInventoryStep) Name() string { return "reserve_inventory" }

// Execute reserves inventory for each cart item.
// Stores reservation IDs in Meta["reservations"].
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
	reserved := make([]inventory.Reservation, 0, len(cctx.Cart.Items))
	for _, item := range cctx.Cart.Items {
		res, err := inventory.NewReservation(id.New(), item.VariantID, item.Quantity, expiresAt)
		if err != nil {
			return fmt.Errorf("reserve_inventory: create reservation: %w", err)
		}
		rctx, rcancel := context.WithTimeout(ctx, reserveTimeout)
		rerr := s.reservations.Reserve(rctx, &res)
		rcancel()
		if rerr != nil {
			// Best-effort rollback: must not inherit a canceled request ctx or
			// prior reservations stay locked for ReservationTTL.
			for _, r := range reserved {
				rlctx, rlcancel := detachedTimeout(ctx, reserveTimeout)
				if releaseErr := s.reservations.Release(rlctx, r.ID); releaseErr == nil {
					s.publishStockUpdated(rlctx, r.VariantID, r.Quantity)
				}
				rlcancel()
			}
			return fmt.Errorf("reserve_inventory: variant %s: %w", item.VariantID, rerr)
		}
		reservationIDs = append(reservationIDs, res.ID)
		reserved = append(reserved, res)
		s.publishStockUpdated(ctx, res.VariantID, res.Quantity)
	}

	cctx.SetMeta("reservations", reservationIDs)
	cctx.SetMeta("reserved", true)
	return nil
}

// publishStockUpdated is a best-effort side channel, not part of this
// step's actual business logic: a no-op when WithStockEventPublishing
// wasn't used, and any failure (variant lookup, bus.Publish itself) is
// swallowed rather than failing checkout over a stale search index entry
// — the same tradeoff the rollback loop above already makes for its own
// Release calls, and this step has no logger of its own to at least
// record the swallowed error (unlike, e.g., returns.Service).
//
// quantity is the reservation's own quantity — the size of the change
// (reserved or restored), not GetStock's resulting on-hand total (unlike
// InventoryAdminHandler.Adjust's own publish site, which already has that
// value in hand). Fetching the post-change total here would cost an extra
// query per cart item purely for a field HandleStockUpdated (this event's
// only current consumer) doesn't read — it schedules a reindex by
// ProductID alone, which re-fetches the product's real current stock
// independently.
func (s *ReserveInventoryStep) publishStockUpdated(ctx context.Context, variantID string, quantity int) {
	if s.bus == nil {
		return
	}
	variant, err := s.variants.FindByID(ctx, variantID)
	if err != nil || variant == nil {
		return
	}
	_ = s.bus.Publish(ctx, event.New(inventory.EventStockUpdated, "checkout.reserve_inventory", inventory.StockUpdatedData{
		ProductID: variant.ProductID,
		VariantID: variant.ID,
		SKU:       variant.SKU,
		Quantity:  quantity,
	}))
}
