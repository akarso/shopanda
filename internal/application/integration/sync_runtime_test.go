package integration_test

import (
	"context"
	"database/sql"
	"io"
	"sync"
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
	mu       sync.Mutex
	enqueued []jobs.Job
}

func (q *memQueue) Enqueue(_ context.Context, job jobs.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.enqueued = append(q.enqueued, job)
	return nil
}
func (q *memQueue) Dequeue(context.Context) (*jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.enqueued) == 0 {
		return nil, nil
	}
	job := q.enqueued[0]
	q.enqueued = q.enqueued[1:]
	return &job, nil
}
func (q *memQueue) Complete(context.Context, string) error    { return nil }
func (q *memQueue) Fail(context.Context, string, error) error { return nil }

func (q *memQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.enqueued)
}

func (q *memQueue) first() jobs.Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.enqueued[0]
}

type recordingScheduler struct {
	mu    sync.Mutex
	names []string
}

func (s *recordingScheduler) Register(name, _ string, _ func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.names = append(s.names, name)
}
func (s *recordingScheduler) Start(context.Context) {}
func (s *recordingScheduler) Stop()                 {}

func (s *recordingScheduler) registered() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.names))
	copy(out, s.names)
	return out
}

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

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			if queue.len() > 0 {
				close(done)
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		close(done)
	}()

	evt := event.Event{Name: "order.created", Data: map[string]interface{}{"order_id": "ord-1"}}
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	<-done
	if queue.len() != 1 {
		t.Fatalf("enqueued = %d", queue.len())
	}
	if queue.first().Type != "integration.sync.acme.export.order" {
		t.Fatalf("job type = %q", queue.first().Type)
	}
	payload, _ := queue.first().Payload["payload"].(map[string]interface{})
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

func TestRegisterSyncJobTriggers_SkipManual(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	queue := &memQueue{}
	sched := &recordingScheduler{}
	app := &plugin.App{Logger: log, Bus: bus, Bootstrap: &plugin.Bootstrap{}}

	// EventName is ignored for Manual(), but if the Kind != event guard were
	// removed, this subscription would fire on order.created — so an empty
	// EventName would not prove the skip.
	manual := extapi.SyncJob{
		Name: "webhook.ingest",
		Trigger: extapi.SyncTrigger{
			Kind:      extapi.SyncTriggerManual,
			EventName: "order.created",
		},
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	}
	eventJob := extapi.SyncJob{
		Name:    "export.order",
		Trigger: extapi.OnEvent("order.created"),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	}
	cronJob := extapi.SyncJob{
		Name:    "warehouse.stock",
		Trigger: extapi.Cron("@every 5m"),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	}
	for _, job := range []extapi.SyncJob{manual, eventJob, cronJob} {
		if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
			t.Fatal(err)
		}
	}
	if err := integrationApp.RegisterSyncJobEventTriggers(app, bus, queue, log); err != nil {
		t.Fatal(err)
	}
	if err := integrationApp.RegisterSyncJobCronTriggers(app, queue, sched, log); err != nil {
		t.Fatal(err)
	}

	names := sched.registered()
	if len(names) != 1 || names[0] != "integration.sync.acme.warehouse.stock" {
		t.Fatalf("cron registrations = %v, want only warehouse.stock", names)
	}

	if err := bus.Publish(context.Background(), event.Event{Name: "order.created"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	var n int
	for time.Now().Before(deadline) {
		n = queue.len()
		if n >= 1 {
			// Settle briefly so a late second enqueue from a bad manual
			// subscription would still be visible.
			time.Sleep(20 * time.Millisecond)
			n = queue.len()
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if n != 1 {
		t.Fatalf("enqueued = %d, want only the event job (manual must not subscribe)", n)
	}
	if queue.first().Type != "integration.sync.acme.export.order" {
		t.Fatalf("job type = %q", queue.first().Type)
	}
}
