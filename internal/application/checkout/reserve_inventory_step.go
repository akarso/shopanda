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

// ReserveInventoryStep creates inventory reservations for each cart item.
type ReserveInventoryStep struct {
	reservations inventory.ReservationRepository
	ttl          time.Duration
}

// NewReserveInventoryStep creates a ReserveInventoryStep.
func NewReserveInventoryStep(reservations inventory.ReservationRepository) *ReserveInventoryStep {
	if reservations == nil {
		panic("checkout: reservations must not be nil")
	}
	return &ReserveInventoryStep{reservations: reservations, ttl: ReservationTTL}
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
			return nil // idempotent
		}
	}

	if cctx.Cart == nil {
		return fmt.Errorf("reserve_inventory: cart not loaded")
	}

	ctx := context.Background()
	expiresAt := time.Now().UTC().Add(s.ttl)

	reservationIDs := make([]string, 0, len(cctx.Cart.Items))
	for _, item := range cctx.Cart.Items {
		res, err := inventory.NewReservation(id.New(), item.VariantID, item.Quantity, expiresAt)
		if err != nil {
			return fmt.Errorf("reserve_inventory: create reservation: %w", err)
		}
		if err := s.reservations.Reserve(ctx, &res); err != nil {
			// Best-effort rollback of prior successful reservations.
			for _, rid := range reservationIDs {
				_ = s.reservations.Release(ctx, rid)
			}
			return fmt.Errorf("reserve_inventory: variant %s: %w", item.VariantID, err)
		}
		reservationIDs = append(reservationIDs, res.ID)
	}

	cctx.SetMeta("reservations", reservationIDs)
	cctx.SetMeta("reserved", true)
	return nil
}
