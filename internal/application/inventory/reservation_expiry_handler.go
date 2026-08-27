package inventory

import (
	"context"
	"errors"
	"time"

	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/jobs"
)

// ReservationExpiryJobType is the job type string for the reservation
// expiry sweep.
const ReservationExpiryJobType = "inventory.reservation_expiry"

// sweepTimeout bounds a single job invocation, well under the 15-minute
// cron interval so a normal (small) sweep never runs anywhere near it, but
// long enough to make real progress across many batches on a large
// backlog. If a sweep hits this deadline before draining everything
// expired as of cutoff, it stops cleanly and returns what it released so
// far — the next scheduled invocation picks up the remainder (cutoff is
// recomputed fresh each time), so this is a normal "still catching up"
// tick, not a failure.
const sweepTimeout = 10 * time.Minute

// ExpiredReservationReleaser releases active reservations that expired
// before a cutoff, restoring their reserved quantity to stock.
type ExpiredReservationReleaser interface {
	ReleaseExpiredBefore(ctx context.Context, cutoff time.Time) (int, error)
}

// Logger is the logging interface used by inventory application services.
type Logger interface {
	Info(msg string, fields map[string]interface{})
	Error(msg string, err error, fields map[string]interface{})
}

// ReservationExpiryHandler processes inventory.reservation_expiry jobs by
// releasing reservations whose TTL has elapsed. Stock availability is
// already correct without this (queries filter expires_at > now() at read
// time) — this exists so expired reservations don't stay "active" forever,
// which would otherwise make that status meaningless for anything that
// later wants to report on or clean up reservation history.
type ReservationExpiryHandler struct {
	releaser ExpiredReservationReleaser
	log      Logger
}

// NewReservationExpiryHandler creates a handler for
// inventory.reservation_expiry jobs.
func NewReservationExpiryHandler(releaser ExpiredReservationReleaser, log Logger) *ReservationExpiryHandler {
	if releaser == nil {
		panic("inventory.NewReservationExpiryHandler: nil releaser")
	}
	if log == nil {
		panic("inventory.NewReservationExpiryHandler: nil logger")
	}
	return &ReservationExpiryHandler{releaser: releaser, log: log}
}

// Type returns the job type this handler processes.
func (h *ReservationExpiryHandler) Type() string { return ReservationExpiryJobType }

// Handle releases reservations expired as of now and logs the result.
func (h *ReservationExpiryHandler) Handle(ctx context.Context, _ jobs.Job) error {
	start := time.Now()
	sweepCtx, cancel := context.WithTimeout(ctx, sweepTimeout)
	defer cancel()

	released, err := h.releaser.ReleaseExpiredBefore(sweepCtx, time.Now())
	if err != nil {
		// An orphaned-stock-restore warning means the releases themselves
		// still committed successfully — log it and continue to the
		// success log below, rather than failing (and retrying) a job that
		// actually did its job. Anything else is a genuine failure.
		var orphanErr *inventory.OrphanedStockRestoreError
		if !errors.As(err, &orphanErr) {
			return err
		}
		h.log.Error("job.reservation_expiry.orphaned_stock_restore", orphanErr, map[string]interface{}{
			"count":           orphanErr.Count,
			"reservation_ids": orphanErr.ReservationIDs,
		})
	}

	// sweepCtx.Err() is checked, not ctx.Err(): any non-nil error here
	// (DeadlineExceeded from sweepTimeout, or Canceled propagated from the
	// caller's own ctx — e.g. worker shutdown) means ReleaseExpiredBefore
	// stopped early (see its doc comment) and more of the backlog likely
	// remains. Both cases get the same true: whether the next attempt to
	// continue is 15 minutes away (next scheduled tick) or delayed until
	// the process restarts (shutdown), the underlying fact — this
	// invocation did not finish draining everything expired as of its
	// cutoff — is the same, and errors.Is(..., DeadlineExceeded) alone
	// would have missed the shutdown case.
	h.log.Info("job.reservation_expiry.released", map[string]interface{}{
		"released":       released,
		"duration":       time.Since(start).String(),
		"more_remaining": sweepCtx.Err() != nil,
	})
	return nil
}
