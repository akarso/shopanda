package runtime

import (
	"context"
	"time"

	searchApp "github.com/akarso/shopanda/internal/application/search"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/scheduler"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// RegisterReindexReconcile registers the periodic search-index-run
// reconciliation sweep on sched. Every 30 minutes comfortably catches a
// run stuck "processing" past ReconcileHandler's own staleAfter (2 hours)
// well within one working shift, without adding meaningful load — the
// sweep is a single bounded query plus, normally, zero follow-up work.
func RegisterReindexReconcile(jobQueue jobs.Queue, log logger.Logger, sched scheduler.Scheduler) {
	sched.Register("search.reindex.reconcile", "*/30 * * * *", func() {
		job, err := jobs.NewJob(id.New(), searchApp.ReconcileJobType, nil)
		if err != nil {
			log.Error("search.reindex.reconcile.schedule", err, nil)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := jobQueue.Enqueue(ctx, job); err != nil {
			log.Error("search.reindex.reconcile.enqueue", err, nil)
		}
	})
}
