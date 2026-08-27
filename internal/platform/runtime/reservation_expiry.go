package runtime

import (
	"context"
	"time"

	inventoryApp "github.com/akarso/shopanda/internal/application/inventory"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// RegisterReservationExpiry registers the periodic inventory reservation
// expiry sweep on sched. The 15-minute cadence matches the reservation TTL
// documented in RUNBOOK.md's "Checkout cancel and timeouts" section, so an
// expired reservation is released within one TTL window of expiring.
func RegisterReservationExpiry(jobQueue jobs.Queue, log logger.Logger, sched scheduler.Scheduler) {
	sched.Register("inventory.reservation_expiry", "*/15 * * * *", func() {
		job, err := jobs.NewJob(id.New(), inventoryApp.ReservationExpiryJobType, nil)
		if err != nil {
			log.Error("inventory.reservation_expiry.schedule", err, nil)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := jobQueue.Enqueue(ctx, job); err != nil {
			log.Error("inventory.reservation_expiry.enqueue", err, nil)
		}
	})
}
