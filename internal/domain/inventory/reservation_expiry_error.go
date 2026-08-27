package inventory

import "fmt"

// OrphanedStockRestoreError reports that ReleaseExpiredBefore released one
// or more reservations whose stock row no longer existed to receive the
// restored quantity back (e.g. the variant was deleted after the
// reservation was created). The releases themselves still committed — a
// released reservation's hold no longer applies to anything regardless of
// whether its quantity had somewhere to go back to — so this is a warning
// about lost quantity to log, not a failure of the release operation. A
// caller that gets this back from ReleaseExpiredBefore should still treat
// the returned count as successfully released.
type OrphanedStockRestoreError struct {
	Count int
	// ReservationIDs is capped (see reservation_repo.go) so a very large
	// backlog of orphaned rows doesn't produce an unbounded log line.
	ReservationIDs []string
}

func (e *OrphanedStockRestoreError) Error() string {
	return fmt.Sprintf(
		"inventory: %d reservation(s) released with no stock row to restore quantity to (variant not found): %v",
		e.Count, e.ReservationIDs,
	)
}
