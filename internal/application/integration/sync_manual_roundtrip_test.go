package integration

import (
	"context"
	"database/sql"
	"io"
	"sync"
	"testing"
	"time"

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

func (q *syncMemQueue) Dequeue(context.Context) (*jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.enqueued) == 0 {
		return nil, nil
	}
	job := q.enqueued[0]
	q.enqueued = q.enqueued[1:]
	return &job, nil
}

func (q *syncMemQueue) Complete(context.Context, string) error    { return nil }
func (q *syncMemQueue) Fail(context.Context, string, error) error { return nil }

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
	worker := jobs.NewWorker(queue, log, time.Second)
	if err := RegisterSyncJobHandlers(app, worker); err != nil {
		t.Fatal(err)
	}
	if err := app.Integration("acme").Enqueue(context.Background(), "webhook.ingest", map[string]interface{}{
		"body": "hello",
	}); err != nil {
		t.Fatal(err)
	}

	worker.ProcessOne(context.Background())

	if got.Plugin != "acme" || got.Name != "webhook.ingest" || got.Trigger != extapi.SyncTriggerManual {
		t.Fatalf("got = %+v", got)
	}
	if got.Payload["body"] != "hello" || got.DB != app.Bootstrap.DB || got.JobID == "" {
		t.Fatalf("got = %+v", got)
	}
}
