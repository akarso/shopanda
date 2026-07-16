package integration_test

import (
	"context"
	"database/sql"
	"io"
	"sync/atomic"
	"testing"
	"time"

	integrationApp "github.com/akarso/shopanda/internal/application/integration"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/infrastructure/cron"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
)

type memQueue struct {
	enqueued []jobs.Job
}

func (q *memQueue) Enqueue(_ context.Context, job jobs.Job) error {
	q.enqueued = append(q.enqueued, job)
	return nil
}
func (q *memQueue) Dequeue(context.Context) (*jobs.Job, error) { return nil, nil }
func (q *memQueue) Complete(context.Context, string) error     { return nil }
func (q *memQueue) Fail(context.Context, string, error) error  { return nil }

func TestRegisterSyncJobEventTriggers_EnqueuesOnPublish(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	queue := &memQueue{}
	app := &plugin.App{
		Logger:    log,
		Bus:       bus,
		Bootstrap: &plugin.Bootstrap{},
	}
	job := extapi.SyncJob{
		Name:    "export.order",
		Trigger: extapi.OnEvent("order.created"),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("RegisterSyncJob: %v", err)
	}
	if err := integrationApp.RegisterSyncJobEventTriggers(app, bus, queue, log); err != nil {
		t.Fatalf("RegisterSyncJobEventTriggers: %v", err)
	}

	evt := event.Event{Name: "order.created", Data: map[string]interface{}{"order_id": "ord-1"}}
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %d", len(queue.enqueued))
	}
	if queue.enqueued[0].Type != "integration.sync.acme.export.order" {
		t.Fatalf("job type = %q", queue.enqueued[0].Type)
	}
	payload, _ := queue.enqueued[0].Payload["payload"].(map[string]interface{})
	if payload["order_id"] != "ord-1" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestRegisterSyncJobHandlers_RegistersWorkerHandler(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	var calls atomic.Int32
	app := &plugin.App{
		Logger:    log,
		Bootstrap: &plugin.Bootstrap{DB: &sql.DB{}},
	}
	job := extapi.SyncJob{
		Name:    "pull.stock",
		Trigger: extapi.Cron("@every 5m"),
		Handler: func(_ context.Context, req extapi.SyncJobContext) error {
			if req.Plugin != "acme" || req.Name != "pull.stock" {
				t.Fatalf("req = %+v", req)
			}
			calls.Add(1)
			return nil
		},
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("RegisterSyncJob: %v", err)
	}

	worker := jobs.NewWorker(&memQueue{}, log, time.Second)
	if err := integrationApp.RegisterSyncJobHandlers(app, worker); err != nil {
		t.Fatalf("RegisterSyncJobHandlers: %v", err)
	}

	if err := app.SyncJobs()[0].Job.Handler(context.Background(), extapi.SyncJobContext{
		JobID: "job-1", Plugin: "acme", Name: "pull.stock", Trigger: extapi.SyncTriggerCron,
		DB: app.Bootstrap.DB, Logger: log,
	}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestRegisterSyncJobCronTriggers_RegistersSchedulerTask(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	queue := &memQueue{}
	sched := cron.New(log)
	app := &plugin.App{Logger: log, Bootstrap: &plugin.Bootstrap{}}
	job := extapi.SyncJob{
		Name:    "warehouse.stock",
		Trigger: extapi.Cron("@every 5m"),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("RegisterSyncJob: %v", err)
	}
	if err := integrationApp.RegisterSyncJobCronTriggers(app, queue, sched, log); err != nil {
		t.Fatalf("RegisterSyncJobCronTriggers: %v", err)
	}
}
