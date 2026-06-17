package runtime

import (
	"context"
	"time"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// RegisterCacheCleanup registers the periodic cache cleanup task on sched.
func RegisterCacheCleanup(jobQueue jobs.Queue, jobType string, log logger.Logger, sched scheduler.Scheduler) {
	sched.Register("cache.cleanup", "*/5 * * * *", func() {
		job, err := jobs.NewJob(id.New(), jobType, nil)
		if err != nil {
			log.Error("cache.cleanup.schedule", err, nil)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := jobQueue.Enqueue(ctx, job); err != nil {
			log.Error("cache.cleanup.enqueue", err, nil)
		}
	})
}
