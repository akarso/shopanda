package runtime

import (
	"context"
	"time"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// RegisterCartRecovery registers the periodic abandoned cart recovery scan on sched.
func RegisterCartRecovery(jobQueue jobs.Queue, log logger.Logger, sched scheduler.Scheduler) {
	sched.Register("cart.recovery", "0 * * * *", func() {
		job, err := jobs.NewJob(id.New(), cartApp.RecoveryJobType, nil)
		if err != nil {
			log.Error("cart.recovery.schedule", err, nil)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := jobQueue.Enqueue(ctx, job); err != nil {
			log.Error("cart.recovery.enqueue", err, nil)
		}
	})
}
