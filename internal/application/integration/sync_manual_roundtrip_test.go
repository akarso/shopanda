package integration

import (
	"context"
	"database/sql"
	"io"
	"sync"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

type syncMemQueue struct {
	mu       sync.Mutex
	enqueued []jobs.Job
}

func (q *syncMemQueue) Enqueue(_ context.Context, job jobs.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.enqueued = append(q.enqueued, job)
	return nil
}
func (q *syncMemQueue) Dequeue(context.Context) (*jobs.Job, error) { return nil, nil }
func (q *syncMemQueue) Complete(context.Context, string) error     { return nil }
func (q *syncMemQueue) Fail(context.Context, string, error) error  { return nil }

func TestManualEnqueue_ThroughWorkerHandler(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	queue := &syncMemQueue{}
	app := &plugin.App{
		Logger:    log,
		Bootstrap: &plugin.Bootstrap{DB: &sql.DB{}},
	}
	var got extapi.SyncJobContext
	if err := app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "webhook.ingest",
		Trigger: extapi.Manual(),
		Handler: func(_ context.Context, req extapi.SyncJobContext) error {
			got = req
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.BindQueue(queue); err != nil {
		t.Fatal(err)
	}
	worker := jobs.NewWorker(queue, log, 0)
	if err := RegisterSyncJobHandlers(app, worker); err != nil {
		t.Fatal(err)
	}
	if err := app.Integration("acme").Enqueue(context.Background(), "webhook.ingest", map[string]interface{}{
		"body": "hello",
	}); err != nil {
		t.Fatal(err)
	}
	queue.mu.Lock()
	enqueued := queue.enqueued[0]
	queue.mu.Unlock()

	h := newSyncJobHandler(app.SyncJobs()[0], app.Bootstrap.DB, log)
	if err := h.Handle(context.Background(), enqueued); err != nil {
		t.Fatal(err)
	}
	if got.Plugin != "acme" || got.Name != "webhook.ingest" || got.Trigger != extapi.SyncTriggerManual {
		t.Fatalf("got = %+v", got)
	}
	if got.Payload["body"] != "hello" || got.JobID != enqueued.ID || got.DB != app.Bootstrap.DB {
		t.Fatalf("got = %+v", got)
	}
}
