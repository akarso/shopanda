package checkout

import (
	"context"
	"fmt"
	"time"

	"github.com/akarso/shopanda/internal/domain/inventory"
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
}

// ReserveOption configures a ReserveInventoryStep.
type ReserveOption func(*ReserveInventoryStep)

// WithTTL sets a custom reservation TTL (default: ReservationTTL).
func WithTTL(ttl time.Duration) ReserveOption {
	return func(s *ReserveInventoryStep) { s.ttl = ttl }
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
func (s *ReserveInventoryStep) Execute(cctx *Context) error {
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
	for _, item := range cctx.Cart.Items {
		res, err := inventory.NewReservation(id.New(), item.VariantID, item.Quantity, expiresAt)
		if err != nil {
			return fmt.Errorf("reserve_inventory: create reservation: %w", err)
		}
		rctx, rcancel := context.WithTimeout(context.Background(), reserveTimeout)
		rerr := s.reservations.Reserve(rctx, &res)
		rcancel()
		if rerr != nil {
			// Best-effort rollback of prior successful reservations.
			for _, rid := range reservationIDs {
				rlctx, rlcancel := context.WithTimeout(context.Background(), reserveTimeout)
				_ = s.reservations.Release(rlctx, rid)
				rlcancel()
			}
			return fmt.Errorf("reserve_inventory: variant %s: %w", item.VariantID, rerr)
		}
		reservationIDs = append(reservationIDs, res.ID)
	}

	cctx.SetMeta("reservations", reservationIDs)
	cctx.SetMeta("reserved", true)
	return nil
}
