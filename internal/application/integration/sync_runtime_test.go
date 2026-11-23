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
	domainorder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/infrastructure/cron"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/platform/requestctx"
	"github.com/akarso/shopanda/pkg/extapi"
)

type memQueue struct {
	mu       sync.Mutex
	enqueued []jobs.Job
}

func (q *memQueue) Enqueue(_ context.Context, job jobs.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, existing := range q.enqueued {
		if existing.ID == job.ID {
			return apperror.New(apperror.CodeConflict, "job id already enqueued")
		}
	}
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
	job.Status = jobs.StatusProcessing
	job.Attempts++
	return &job, nil
}
func (q *memQueue) Complete(context.Context, string, int) error                       { return nil }
func (q *memQueue) Fail(context.Context, string, int, error) error                    { return nil }
func (q *memQueue) Requeue(context.Context, string, int, ...jobs.RequeueOption) error { return nil }

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

func TestRegisterSyncJobEventTriggers_EventJobIDFromOrderCreatedData(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	queue := &memQueue{}
	app := &plugin.App{
		Logger:    log,
		Bus:       bus,
		Bootstrap: &plugin.Bootstrap{},
	}
	var sawOrderID string
	var sawRequestID bool
	var sawItemCount interface{}
	fixedNS := id.New()
	job := extapi.SyncJob{
		Name:    "snap.order",
		Trigger: extapi.OnEvent(domainorder.EventOrderCreated),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
		EventJobID: func(payload map[string]interface{}) string {
			sawOrderID, _ = payload["order_id"].(string)
			_, sawRequestID = payload["request_id"]
			sawItemCount = payload["item_count"]
			if sawOrderID == "" {
				return ""
			}
			return fixedNS
		},
		EventJobIDSet: true,
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("RegisterSyncJob: %v", err)
	}
	if err := integrationApp.RegisterSyncJobEventTriggers(app, bus, queue, log); err != nil {
		t.Fatalf("RegisterSyncJobEventTriggers: %v", err)
	}
	publishCtx := requestctx.WithRequestID(context.Background(), "req-typed-1")
	if err := bus.Publish(publishCtx, event.Event{
		Name: domainorder.EventOrderCreated,
		Data: domainorder.OrderCreatedData{OrderID: "ord-typed-1", Currency: "EUR", ItemCount: 2},
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && queue.len() == 0 {
		time.Sleep(2 * time.Millisecond)
	}
	if queue.len() != 1 {
		t.Fatalf("enqueued = %d, sawOrderID=%q", queue.len(), sawOrderID)
	}
	if sawOrderID != "ord-typed-1" {
		t.Fatalf("EventJobID saw order_id=%q, want ord-typed-1", sawOrderID)
	}
	if sawRequestID {
		t.Fatal("EventJobID payload must not include request_id yet")
	}
	// JSON round-trip of typed structs yields float64 for numbers.
	if _, ok := sawItemCount.(float64); !ok {
		t.Fatalf("item_count type = %T (%v), want float64", sawItemCount, sawItemCount)
	}
	if queue.first().ID != fixedNS {
		t.Fatalf("job id = %q, want derived %q", queue.first().ID, fixedNS)
	}
	payload, _ := queue.first().Payload["payload"].(map[string]interface{})
	if payload["order_id"] != "ord-typed-1" {
		t.Fatalf("stored payload = %#v", payload)
	}
	if payload["request_id"] != "req-typed-1" {
		t.Fatalf("stored request_id = %#v", payload["request_id"])
	}
}

func TestRegisterSyncJobEventTriggers_EventJobIDIdempotent(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	queue := &memQueue{}
	app := &plugin.App{
		Logger:    log,
		Bus:       bus,
		Bootstrap: &plugin.Bootstrap{},
	}
	fixedID := id.New()
	job := extapi.SyncJob{
		Name:    "export.order",
		Trigger: extapi.OnEvent("order.created"),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
		EventJobID: func(payload map[string]interface{}) string {
			return fixedID
		},
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("RegisterSyncJob: %v", err)
	}
	// JobsPort required for Conflict resolve — bind a pending detail getter.
	if err := app.BindJobsPort(&eventJobConflictPort{
		detail: &jobs.Detail{Summary: jobs.Summary{
			ID: fixedID, Type: "integration.sync.acme.export.order", Status: jobs.StatusPending,
		}},
	}); err != nil {
		t.Fatalf("BindJobsPort: %v", err)
	}
	if err := integrationApp.RegisterSyncJobEventTriggers(app, bus, queue, log); err != nil {
		t.Fatalf("RegisterSyncJobEventTriggers: %v", err)
	}

	waitEnqueued := func(n int) {
		t.Helper()
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			if queue.len() >= n {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatalf("enqueued = %d, want >= %d", queue.len(), n)
	}

	evt := event.Event{Name: "order.created", Data: map[string]interface{}{"order_id": "ord-1"}}
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish 1: %v", err)
	}
	waitEnqueued(1)
	if queue.first().ID != fixedID {
		t.Fatalf("job id = %q, want %q", queue.first().ID, fixedID)
	}
	// Second delivery: Conflict → ResolveEnqueueConflict → pending no-op.
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish 2: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if queue.len() != 1 {
		t.Fatalf("enqueued after duplicate = %d, want 1", queue.len())
	}
}

type eventJobConflictPort struct {
	detail *jobs.Detail
}

func (p *eventJobConflictPort) Get(context.Context, string) (*jobs.Detail, error) {
	return p.detail, nil
}
func (p *eventJobConflictPort) Requeue(context.Context, string, int, ...jobs.RequeueOption) error {
	return nil
}

func TestRegisterSyncJobEventTriggers_PropagatesRequestIDFromPublish(t *testing.T) {
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

	deadline := time.Now().Add(500 * time.Millisecond)
	publishCtx := requestctx.WithRequestID(context.Background(), "req-http-checkout")
	if err := bus.Publish(publishCtx, event.Event{
		Name: "order.created",
		Data: map[string]interface{}{"order_id": "ord-1"},
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	for time.Now().Before(deadline) {
		if queue.len() >= 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if queue.len() != 1 {
		t.Fatalf("enqueued = %d", queue.len())
	}
	payload, _ := queue.first().Payload["payload"].(map[string]interface{})
	if payload["request_id"] != "req-http-checkout" {
		t.Fatalf("payload = %#v, want request_id from Publish ctx", payload)
	}
	if payload["order_id"] != "ord-1" {
		t.Fatalf("payload = %#v, want order_id preserved", payload)
	}
}

func TestRegisterSyncJobHandlers_ReinjectsRequestIDOntoCtx(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	var gotCtxID string
	app := &plugin.App{
		Logger:    log,
		Bootstrap: &plugin.Bootstrap{DB: &sql.DB{}},
	}
	job := extapi.SyncJob{
		Name:    "pull.stock",
		Trigger: extapi.Cron("@every 5m"),
		Handler: func(ctx context.Context, _ extapi.SyncJobContext) error {
			gotCtxID = requestctx.RequestID(ctx)
			return nil
		},
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("RegisterSyncJob: %v", err)
	}

	queue := &memQueue{}
	worker := jobs.NewWorker(queue, log, time.Second)
	if err := integrationApp.RegisterSyncJobHandlers(app, worker); err != nil {
		t.Fatalf("RegisterSyncJobHandlers: %v", err)
	}

	envelope, err := plugin.NewSyncJobEnvelope(app.SyncJobs()[0], extapi.SyncTriggerCron, map[string]interface{}{
		"request_id": "req-from-payload",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	if !worker.ProcessOne(context.Background()) {
		t.Fatal("expected job processed")
	}
	if gotCtxID != "req-from-payload" {
		t.Fatalf("ctx request_id = %q, want req-from-payload", gotCtxID)
	}
}

func TestRegisterSyncJobHandlers_KeepsExistingCtxRequestID(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	var gotCtxID string
	app := &plugin.App{
		Logger:    log,
		Bootstrap: &plugin.Bootstrap{DB: &sql.DB{}},
	}
	job := extapi.SyncJob{
		Name:    "pull.stock",
		Trigger: extapi.Cron("@every 5m"),
		Handler: func(ctx context.Context, _ extapi.SyncJobContext) error {
			gotCtxID = requestctx.RequestID(ctx)
			return nil
		},
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("RegisterSyncJob: %v", err)
	}

	queue := &memQueue{}
	worker := jobs.NewWorker(queue, log, time.Second)
	if err := integrationApp.RegisterSyncJobHandlers(app, worker); err != nil {
		t.Fatalf("RegisterSyncJobHandlers: %v", err)
	}

	envelope, err := plugin.NewSyncJobEnvelope(app.SyncJobs()[0], extapi.SyncTriggerCron, map[string]interface{}{
		"request_id": "req-from-payload",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithRequestID(context.Background(), "req-already-on-ctx")
	if !worker.ProcessOne(ctx) {
		t.Fatal("expected job processed")
	}
	if gotCtxID != "req-already-on-ctx" {
		t.Fatalf("ctx request_id = %q, want existing ctx value", gotCtxID)
	}
}

func TestRegisterSyncJobHandlers_AttemptsFromDequeuedJob(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	var gotAttempts int
	app := &plugin.App{
		Logger:    log,
		Bootstrap: &plugin.Bootstrap{DB: &sql.DB{}},
	}
	job := extapi.SyncJob{
		Name:    "pull.stock",
		Trigger: extapi.Cron("@every 5m"),
		Handler: func(_ context.Context, req extapi.SyncJobContext) error {
			gotAttempts = req.Attempts
			return nil
		},
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("RegisterSyncJob: %v", err)
	}

	queue := &memQueue{}
	worker := jobs.NewWorker(queue, log, time.Second)
	if err := integrationApp.RegisterSyncJobHandlers(app, worker); err != nil {
		t.Fatalf("RegisterSyncJobHandlers: %v", err)
	}

	envelope, err := plugin.NewSyncJobEnvelope(app.SyncJobs()[0], extapi.SyncTriggerCron, map[string]interface{}{"k": 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	if !worker.ProcessOne(context.Background()) {
		t.Fatal("expected job processed")
	}
	// memQueue.Dequeue increments Attempts like the real queue.
	if gotAttempts != 1 {
		t.Fatalf("Attempts = %d, want 1 after dequeue", gotAttempts)
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
