package runtime

import (
	"context"
	"time"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// RegisterAuditRetention registers the daily audit log retention task on sched.
func RegisterAuditRetention(jobQueue jobs.Queue, log logger.Logger, sched scheduler.Scheduler) {
	sched.Register("audit.retention", "0 3 * * *", func() {
		job, err := jobs.NewJob(id.New(), adminApp.RetentionJobType, nil)
		if err != nil {
			log.Error("audit.retention.schedule", err, nil)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := jobQueue.Enqueue(ctx, job); err != nil {
			log.Error("audit.retention.enqueue", err, nil)
		}
	})
}
