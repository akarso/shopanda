package plugin_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/platform/requestctx"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

type memQueue struct {
	mu       sync.Mutex
	enqueued []jobs.Job
	err      error
}

func (q *memQueue) Enqueue(_ context.Context, job jobs.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.enqueued = append(q.enqueued, job)
	return nil
}
func (q *memQueue) Dequeue(context.Context) (*jobs.Job, error)                        { return nil, nil }
func (q *memQueue) Complete(context.Context, string, int) error                       { return nil }
func (q *memQueue) Fail(context.Context, string, int, error) error                    { return nil }
func (q *memQueue) Requeue(context.Context, string, int, ...jobs.RequeueOption) error { return nil }

func (q *memQueue) snapshot() []jobs.Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]jobs.Job, len(q.enqueued))
	copy(out, q.enqueued)
	return out
}

func TestApp_Integration_RegisterSyncJob(t *testing.T) {
	app := &plugin.App{
		Logger:    logger.NewWithWriter(io.Discard, "error"),
		Bootstrap: &plugin.Bootstrap{},
	}
	called := false
	job := extapi.SyncJob{
		Name:    "warehouse.stock",
		Trigger: extapi.Cron("@every 5m"),
		Handler: func(ctx context.Context, req extapi.SyncJobContext) error {
			called = true
			return nil
		},
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("RegisterSyncJob: %v", err)
	}
	regs := app.SyncJobs()
	if len(regs) != 1 || regs[0].JobType != "integration.sync.acme.warehouse.stock" {
		t.Fatalf("SyncJobs() = %+v", regs)
	}
	if called {
		t.Fatal("handler should not run at registration time")
	}
}

func TestApp_Integration_RegisterSyncJob_Manual(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	job := extapi.SyncJob{
		Name:    "webhook.ingest",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("RegisterSyncJob Manual: %v", err)
	}
	if got := app.SyncJobs(); len(got) != 1 || got[0].Job.Trigger.Kind != extapi.SyncTriggerManual {
		t.Fatalf("SyncJobs() = %+v", got)
	}
}

func TestApp_Integration_RegisterSyncJob_DuplicateRejected(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	handler := func(context.Context, extapi.SyncJobContext) error { return nil }
	job := extapi.SyncJob{Name: "pull", Trigger: extapi.OnEvent("order.created"), Handler: handler}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatalf("first RegisterSyncJob: %v", err)
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err == nil {
		t.Fatal("expected duplicate sync job error")
	}
}

func TestApp_Integration_RegisterSyncJob_InvalidTrigger(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	err := app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "x",
		Trigger: extapi.SyncTrigger{Kind: "bogus"},
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	if err == nil {
		t.Fatal("expected trigger error")
	}
}

func TestApp_Integration_Enqueue_RequiresBind(t *testing.T) {
	app := &plugin.App{
		Logger:    logger.NewWithWriter(io.Discard, "error"),
		Bootstrap: &plugin.Bootstrap{},
	}
	app.RegisterQueue(&memQueue{}) // plugin provider must not unlock Enqueue
	job := extapi.SyncJob{
		Name:    "webhook.ingest",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatal(err)
	}
	err := app.Integration("acme").Enqueue(context.Background(), "webhook.ingest", map[string]interface{}{"k": "v"})
	if err == nil || !errors.Is(err, extapi.ErrSyncJobQueueUnbound) {
		t.Fatalf("err = %v, want ErrSyncJobQueueUnbound", err)
	}
}

func TestApp_Integration_Enqueue_UnknownName(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	queue := &memQueue{}
	if err := app.BindQueue(queue); err != nil {
		t.Fatal(err)
	}
	err := app.Integration("acme").Enqueue(context.Background(), "missing", nil)
	if err == nil || !errors.Is(err, extapi.ErrSyncJobNotFound) {
		t.Fatalf("err = %v, want ErrSyncJobNotFound", err)
	}
}

func TestApp_Integration_Enqueue_EmptyName(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	_ = app.BindQueue(&memQueue{})
	err := app.Integration("acme").Enqueue(context.Background(), "  ", nil)
	if err == nil || !errors.Is(err, extapi.ErrSyncJobNameRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestApp_Integration_Enqueue_NilContext(t *testing.T) {
	app := &plugin.App{
		Logger:    logger.NewWithWriter(io.Discard, "error"),
		Bootstrap: &plugin.Bootstrap{},
	}
	queue := &memQueue{}
	_ = app.BindQueue(queue)
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "webhook.ingest",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	if err := app.Integration("acme").Enqueue(nil, "webhook.ingest", nil); err != nil {
		t.Fatalf("nil ctx: %v", err)
	}
	if len(queue.snapshot()) != 1 {
		t.Fatal("expected one enqueued job")
	}
}

func TestApp_Integration_Enqueue_BackendErrorWrapped(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	queue := &memQueue{err: errors.New("broker down")}
	_ = app.BindQueue(queue)
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "webhook.ingest",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	err := app.Integration("acme").Enqueue(context.Background(), "webhook.ingest", nil)
	if err == nil || !strings.Contains(err.Error(), "enqueue sync job") || !strings.Contains(err.Error(), "broker down") {
		t.Fatalf("err = %v", err)
	}
}

func TestApp_Integration_Enqueue_PayloadShape(t *testing.T) {
	app := &plugin.App{
		Logger:    logger.NewWithWriter(io.Discard, "error"),
		Bootstrap: &plugin.Bootstrap{},
	}
	queue := &memQueue{}
	if err := app.BindQueue(queue); err != nil {
		t.Fatal(err)
	}
	job := extapi.SyncJob{
		Name:       "webhook.ingest",
		Trigger:    extapi.Manual(),
		MaxRetries: 3,
		Handler:    func(context.Context, extapi.SyncJobContext) error { return nil },
	}
	if err := app.Integration("acme").RegisterSyncJob(job); err != nil {
		t.Fatal(err)
	}
	payload := map[string]interface{}{"order_id": "ord-1"}
	if err := app.Integration("acme").Enqueue(context.Background(), "webhook.ingest", payload); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	payload["order_id"] = "mutated"
	got := queue.snapshot()[0]
	if got.Type != "integration.sync.acme.webhook.ingest" {
		t.Fatalf("type = %q", got.Type)
	}
	if got.MaxRetries != 3 {
		t.Fatalf("MaxRetries = %d", got.MaxRetries)
	}
	if got.Payload["plugin"] != "acme" || got.Payload["name"] != "webhook.ingest" {
		t.Fatalf("payload meta = %#v", got.Payload)
	}
	if got.Payload["trigger"] != extapi.SyncTriggerManual {
		t.Fatalf("trigger = %#v", got.Payload["trigger"])
	}
	inner, _ := got.Payload["payload"].(map[string]interface{})
	if inner["order_id"] != "ord-1" {
		t.Fatalf("inner payload = %#v (want shallow copy)", inner)
	}
}

func TestApp_Integration_Enqueue_InjectsRequestID(t *testing.T) {
	app := &plugin.App{
		Logger:    logger.NewWithWriter(io.Discard, "error"),
		Bootstrap: &plugin.Bootstrap{},
	}
	queue := &memQueue{}
	if err := app.BindQueue(queue); err != nil {
		t.Fatal(err)
	}
	if err := app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "webhook.ingest",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithRequestID(context.Background(), "req-from-http")
	if err := app.Integration("acme").Enqueue(ctx, "webhook.ingest", map[string]interface{}{"k": 1}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	inner, _ := queue.snapshot()[0].Payload["payload"].(map[string]interface{})
	if inner["request_id"] != "req-from-http" {
		t.Fatalf("inner = %#v, want request_id injected", inner)
	}
}

func TestApp_Integration_Enqueue_PreservesExplicitRequestID(t *testing.T) {
	app := &plugin.App{
		Logger:    logger.NewWithWriter(io.Discard, "error"),
		Bootstrap: &plugin.Bootstrap{},
	}
	queue := &memQueue{}
	if err := app.BindQueue(queue); err != nil {
		t.Fatal(err)
	}
	if err := app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "webhook.ingest",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	}); err != nil {
		t.Fatal(err)
	}
	ctx := requestctx.WithRequestID(context.Background(), "req-from-http")
	if err := app.Integration("acme").Enqueue(ctx, "webhook.ingest", map[string]interface{}{
		"request_id": "already-set",
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	inner, _ := queue.snapshot()[0].Payload["payload"].(map[string]interface{})
	if inner["request_id"] != "already-set" {
		t.Fatalf("explicit request_id overwritten: %#v", inner)
	}
}

func TestInjectRequestID_NoCtxID(t *testing.T) {
	out := plugin.InjectRequestID(context.Background(), map[string]interface{}{"a": 1})
	if _, ok := out["request_id"]; ok {
		t.Fatalf("unexpected request_id: %#v", out)
	}
	if out["a"] != 1 {
		t.Fatalf("payload mutated: %#v", out)
	}
}

func TestInjectRequestID_OverwritesEmptyOrNonString(t *testing.T) {
	ctx := requestctx.WithRequestID(context.Background(), "req-http")
	for _, existing := range []interface{}{"", nil, 42} {
		out := plugin.InjectRequestID(ctx, map[string]interface{}{"request_id": existing})
		if out["request_id"] != "req-http" {
			t.Fatalf("existing=%#v → %#v, want req-http", existing, out)
		}
	}
}

func TestApp_Integration_Enqueue_CronJobRunNow(t *testing.T) {
	app := &plugin.App{
		Logger:    logger.NewWithWriter(io.Discard, "error"),
		Bootstrap: &plugin.Bootstrap{},
	}
	queue := &memQueue{}
	_ = app.BindQueue(queue)
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "warehouse.stock",
		Trigger: extapi.Cron("@every 5m"),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	if err := app.Integration("acme").Enqueue(context.Background(), "warehouse.stock", map[string]interface{}{"force": true}); err != nil {
		t.Fatalf("Enqueue cron job: %v", err)
	}
	got := queue.snapshot()[0]
	if got.Payload["trigger"] != extapi.SyncTriggerManual {
		t.Fatalf("run-now trigger = %#v, want manual", got.Payload["trigger"])
	}
}

func TestApp_Integration_Enqueue_NormalizesSlug(t *testing.T) {
	app := &plugin.App{
		Logger:    logger.NewWithWriter(io.Discard, "error"),
		Bootstrap: &plugin.Bootstrap{},
	}
	queue := &memQueue{}
	_ = app.BindQueue(queue)
	_ = app.Integration("ACME").RegisterSyncJob(extapi.SyncJob{
		Name:    "webhook.ingest",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	if err := app.Integration("acme").Enqueue(context.Background(), "webhook.ingest", nil); err != nil {
		t.Fatalf("Enqueue via lowercase slug: %v", err)
	}
	if queue.snapshot()[0].Payload["plugin"] != "acme" {
		t.Fatalf("plugin = %#v", queue.snapshot()[0].Payload["plugin"])
	}
}

func TestApp_Integration_Enqueue_WithJobID_PendingConflictNoOp(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	queue := &conflictQueue{byID: map[string]jobs.Job{}}
	_ = app.BindQueue(queue)
	port := &conflictJobsPort{q: queue}
	if err := app.BindJobsPort(port); err != nil {
		t.Fatal(err)
	}
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "gen.run",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	jobID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeee1"
	if err := app.Integration("acme").Enqueue(context.Background(), "gen.run", map[string]interface{}{"n": 1}, plugin.WithJobID(jobID)); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	if err := app.Integration("acme").Enqueue(context.Background(), "gen.run", map[string]interface{}{"n": 2}, plugin.WithJobID(jobID)); err != nil {
		t.Fatalf("pending Conflict must be idempotent success: %v", err)
	}
	if port.requeueCalls != 0 {
		t.Fatalf("pending must not Requeue, got %d", port.requeueCalls)
	}
	if len(queue.byID) != 1 {
		t.Fatalf("duplicate must not insert: %+v", queue.byID)
	}
	inner, _ := queue.byID[jobID].Payload["payload"].(map[string]interface{})
	if inner["n"] != 1 {
		t.Fatalf("second payload must not replace first: %#v", inner)
	}
}

func TestApp_Integration_Enqueue_WithJobID_TerminalRequeues(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	queue := &conflictQueue{byID: map[string]jobs.Job{}}
	_ = app.BindQueue(queue)
	port := &conflictJobsPort{q: queue}
	_ = app.BindJobsPort(port)
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "gen.run",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	jobID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeee2"
	if err := app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID(jobID)); err != nil {
		t.Fatal(err)
	}
	j := queue.byID[jobID]
	j.Status = jobs.StatusFailed
	j.Attempts = 3
	queue.byID[jobID] = j
	if err := app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID(jobID)); err != nil {
		t.Fatalf("terminal Conflict must Requeue: %v", err)
	}
	if port.requeueCalls != 1 || port.lastRequeueID != jobID || port.lastAttempt != 3 {
		t.Fatalf("requeue = (%d, %q, %d)", port.requeueCalls, port.lastRequeueID, port.lastAttempt)
	}
}

func TestApp_Integration_Enqueue_WithJobID_ConcurrentTerminalBothSucceed(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	queue := &conflictQueue{byID: map[string]jobs.Job{}}
	_ = app.BindQueue(queue)
	port := &conflictJobsPort{q: queue, mode: conflictRequeueRace}
	_ = app.BindJobsPort(port)
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "gen.run",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	jobID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeee5"
	const jobType = "integration.sync.acme.gen.run"
	queue.byID[jobID] = jobs.Job{ID: jobID, Type: jobType, Status: jobs.StatusFailed, Attempts: 2}

	const n = 2
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID(jobID))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Enqueue: %v", err)
		}
	}
	if port.requeueOK != 1 {
		t.Fatalf("requeueOK = %d, want exactly one successful terminal requeue", port.requeueOK)
	}
}

func TestApp_Integration_Enqueue_WithJobID_WrongAttemptRetries(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	queue := &conflictQueue{byID: map[string]jobs.Job{}}
	_ = app.BindQueue(queue)
	port := &conflictJobsPort{q: queue, mode: conflictRequeueStaleOnce}
	_ = app.BindJobsPort(port)
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "gen.run",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	jobID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeee6"
	queue.byID[jobID] = jobs.Job{
		ID: jobID, Type: "integration.sync.acme.gen.run",
		Status: jobs.StatusFailed, Attempts: 5,
	}
	if err := app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID(jobID)); err != nil {
		t.Fatalf("WrongAttempt reload+retry: %v", err)
	}
	if port.requeueCalls != 2 || port.requeueOK != 1 {
		t.Fatalf("requeueCalls=%d requeueOK=%d, want 2 calls / 1 ok", port.requeueCalls, port.requeueOK)
	}
}

func TestApp_Integration_Enqueue_WithJobID_WrongAttemptThenInFlight(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	queue := &conflictQueue{byID: map[string]jobs.Job{}}
	_ = app.BindQueue(queue)
	port := &conflictJobsPort{q: queue, mode: conflictRequeueWrongThenInFlight}
	_ = app.BindJobsPort(port)
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "gen.run",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	jobID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeee7"
	queue.byID[jobID] = jobs.Job{
		ID: jobID, Type: "integration.sync.acme.gen.run",
		Status: jobs.StatusFailed, Attempts: 2,
	}
	if err := app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID(jobID)); err != nil {
		t.Fatalf("WrongAttempt then in-flight: %v", err)
	}
	if port.requeueCalls != 1 || port.requeueOK != 0 {
		t.Fatalf("requeueCalls=%d requeueOK=%d, want 1 call / 0 ok (no second Requeue)", port.requeueCalls, port.requeueOK)
	}
	if got := queue.byID[jobID].Status; got != jobs.StatusPending {
		t.Fatalf("Get after WrongAttempt must observe pending, got %q", got)
	}
}

func TestApp_Integration_Enqueue_WithJobID_WrongAttemptThenForeignType(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	queue := &conflictQueue{byID: map[string]jobs.Job{}}
	_ = app.BindQueue(queue)
	port := &conflictJobsPort{q: queue, mode: conflictRequeueWrongThenForeign}
	_ = app.BindJobsPort(port)
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "gen.run",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	jobID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeea"
	queue.byID[jobID] = jobs.Job{
		ID: jobID, Type: "integration.sync.acme.gen.run",
		Status: jobs.StatusFailed, Attempts: 2,
	}
	err := app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID(jobID))
	if err == nil || !strings.Contains(err.Error(), "refusing to resume foreign job") {
		t.Fatalf("err = %v, want foreign job refusal after WrongAttempt reload", err)
	}
	if port.requeueCalls != 1 {
		t.Fatalf("requeueCalls = %d, want 1 (no retry after type mismatch)", port.requeueCalls)
	}
}

func TestApp_Integration_Enqueue_WithJobID_WrongAttemptRetryExhausted(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	queue := &conflictQueue{byID: map[string]jobs.Job{}}
	_ = app.BindQueue(queue)
	port := &conflictJobsPort{q: queue, mode: conflictRequeueAlwaysWrong}
	_ = app.BindJobsPort(port)
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "gen.run",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	jobID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeee8"
	queue.byID[jobID] = jobs.Job{
		ID: jobID, Type: "integration.sync.acme.gen.run",
		Status: jobs.StatusFailed, Attempts: 1,
	}
	err := app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID(jobID))
	if err == nil || !strings.Contains(err.Error(), "conflict requeue retry") {
		t.Fatalf("err = %v, want one-shot limit (conflict requeue retry)", err)
	}
	if port.requeueCalls != 2 {
		t.Fatalf("requeueCalls = %d, want 2 (initial + one retry)", port.requeueCalls)
	}
}

func TestApp_Integration_Enqueue_WithJobID_NotTerminalAloneSucceeds(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	queue := &conflictQueue{byID: map[string]jobs.Job{}}
	_ = app.BindQueue(queue)
	port := &conflictJobsPort{q: queue, mode: conflictRequeueNotTerminal}
	_ = app.BindJobsPort(port)
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "gen.run",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	jobID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeee9"
	queue.byID[jobID] = jobs.Job{
		ID: jobID, Type: "integration.sync.acme.gen.run",
		Status: jobs.StatusFailed, Attempts: 0,
	}
	if err := app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID(jobID)); err != nil {
		t.Fatalf("NotTerminal alone: %v", err)
	}
	if port.requeueCalls != 1 || port.requeueOK != 0 {
		t.Fatalf("requeueCalls=%d requeueOK=%d, want 1 call / 0 ok", port.requeueCalls, port.requeueOK)
	}
}

func TestApp_Integration_Enqueue_WithJobID_WrongTypeFails(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	queue := &conflictQueue{byID: map[string]jobs.Job{}}
	_ = app.BindQueue(queue)
	_ = app.BindJobsPort(&conflictJobsPort{q: queue})
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "gen.run",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	jobID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeee3"
	queue.byID[jobID] = jobs.Job{ID: jobID, Type: "integration.sync.other.job", Status: jobs.StatusPending}
	err := app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID(jobID))
	if err == nil || !strings.Contains(err.Error(), "refusing to resume foreign job") {
		t.Fatalf("err = %v, want foreign job refusal", err)
	}
}

func TestApp_Integration_Enqueue_WithJobID_ConflictWithoutJobsPort(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	queue := &conflictQueue{byID: map[string]jobs.Job{}}
	_ = app.BindQueue(queue)
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "gen.run",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	jobID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeee4"
	if err := app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID(jobID)); err != nil {
		t.Fatal(err)
	}
	err := app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID(jobID))
	if err == nil || !apperror.Is(err, apperror.CodeConflict) {
		t.Fatalf("err = %v, want wrapped Conflict (IsConflict)", err)
	}
	if !strings.Contains(err.Error(), "JobsPort required") {
		t.Fatalf("err = %v, want JobsPort required", err)
	}
}

func TestApp_Integration_Enqueue_WithJobID_NonUUIDFailsClosed(t *testing.T) {
	app := &plugin.App{Bootstrap: &plugin.Bootstrap{}}
	_ = app.BindQueue(&memQueue{})
	_ = app.Integration("acme").RegisterSyncJob(extapi.SyncJob{
		Name:    "gen.run",
		Trigger: extapi.Manual(),
		Handler: func(context.Context, extapi.SyncJobContext) error { return nil },
	})
	err := app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID("entity-gen-1"))
	if err == nil || !strings.Contains(err.Error(), "not a UUID") {
		t.Fatalf("err = %v, want UUID fail-closed", err)
	}
	err = app.Integration("acme").Enqueue(context.Background(), "gen.run", nil, plugin.WithJobID("  "))
	if err == nil || !strings.Contains(err.Error(), "WithJobID") {
		t.Fatalf("err = %v, want empty WithJobID fail-closed", err)
	}
}

// conflictQueue rejects duplicate ids with Conflict and stores jobs for Get.
type conflictQueue struct {
	mu   sync.Mutex
	byID map[string]jobs.Job
}

func (q *conflictQueue) Enqueue(_ context.Context, job jobs.Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.byID[job.ID]; ok {
		return apperror.Conflict("job already enqueued")
	}
	if job.Status == "" {
		job.Status = jobs.StatusPending
	}
	q.byID[job.ID] = job
	return nil
}
func (q *conflictQueue) Dequeue(context.Context) (*jobs.Job, error)     { return nil, nil }
func (q *conflictQueue) Complete(context.Context, string, int) error    { return nil }
func (q *conflictQueue) Fail(context.Context, string, int, error) error { return nil }
func (q *conflictQueue) Requeue(context.Context, string, int, ...jobs.RequeueOption) error {
	return nil
}

// conflictRequeueMode selects a single Requeue behaviour for conflictJobsPort.
// Modes are mutually exclusive — set exactly one via the mode field.
type conflictRequeueMode int

const (
	conflictRequeueOK                conflictRequeueMode = iota
	conflictRequeueRace                                  // terminal→pending once; later calls → NotTerminal
	conflictRequeueStaleOnce                             // first call WrongAttempt + bump Attempts; second OK
	conflictRequeueWrongThenInFlight                     // Requeue→WrongAttempt; Get after that observes pending (peer won)
	conflictRequeueWrongThenForeign                      // Requeue→WrongAttempt; Get after that observes foreign Type
	conflictRequeueAlwaysWrong                           // every call WrongAttempt (documents one-shot limit)
	conflictRequeueNotTerminal                           // every call NotTerminal
)

type conflictJobsPort struct {
	q             *conflictQueue
	mode          conflictRequeueMode
	requeueCalls  int
	requeueOK     int
	lastRequeueID string
	lastAttempt   int
	staleFired    bool
}

var _ pluginsdk.JobsPort = (*conflictJobsPort)(nil)

func (p *conflictJobsPort) Get(_ context.Context, id string) (*jobs.Detail, error) {
	p.q.mu.Lock()
	defer p.q.mu.Unlock()
	job, ok := p.q.byID[id]
	if !ok {
		return nil, nil
	}
	// Driver-faithful race: Requeue only returned WrongAttempt; the peer's
	// change is visible on the subsequent Get.
	if p.staleFired {
		switch p.mode {
		case conflictRequeueWrongThenInFlight:
			job.Status = jobs.StatusPending
			p.q.byID[id] = job
		case conflictRequeueWrongThenForeign:
			job.Type = "integration.sync.other.job"
			p.q.byID[id] = job
		}
	}
	return &jobs.Detail{
		Summary: jobs.Summary{ID: job.ID, Type: job.Type, Status: job.Status, Attempts: job.Attempts},
		Payload: job.Payload,
	}, nil
}
func (p *conflictJobsPort) Requeue(_ context.Context, id string, attempt int, _ ...jobs.RequeueOption) error {
	p.q.mu.Lock()
	defer p.q.mu.Unlock()
	p.requeueCalls++
	p.lastRequeueID = id
	p.lastAttempt = attempt
	job, ok := p.q.byID[id]
	if !ok {
		return jobs.ErrRequeueNotFound
	}
	switch p.mode {
	case conflictRequeueNotTerminal:
		return jobs.ErrRequeueNotTerminal
	case conflictRequeueAlwaysWrong:
		job.Attempts = attempt + 1
		p.q.byID[id] = job
		return jobs.ErrRequeueWrongAttempt
	case conflictRequeueWrongThenInFlight, conflictRequeueWrongThenForeign:
		// Status/type stay as-is until Get (matches Postgres: WrongAttempt
		// under FOR UPDATE does not mutate the row for the loser).
		p.staleFired = true
		return jobs.ErrRequeueWrongAttempt
	case conflictRequeueStaleOnce:
		if !p.staleFired {
			p.staleFired = true
			job.Attempts = attempt + 1
			p.q.byID[id] = job
			return jobs.ErrRequeueWrongAttempt
		}
		p.requeueOK++
		return nil
	case conflictRequeueRace:
		switch job.Status {
		case jobs.StatusFailed, jobs.StatusDone, jobs.StatusCancelled:
			if attempt != job.Attempts {
				return jobs.ErrRequeueWrongAttempt
			}
			job.Status = jobs.StatusPending
			p.q.byID[id] = job
			p.requeueOK++
			return nil
		default:
			return jobs.ErrRequeueNotTerminal
		}
	default: // conflictRequeueOK
		p.requeueOK++
		return nil
	}
}
func (p *conflictJobsPort) CountsByStatus(context.Context) (map[jobs.Status]int, error) {
	return map[jobs.Status]int{}, nil
}
func (p *conflictJobsPort) CountsByType(_ context.Context, jobType string) (map[jobs.Status]int, error) {
	if strings.TrimSpace(jobType) == "" {
		return nil, errors.New("conflictJobsPort: empty job type")
	}
	return map[jobs.Status]int{}, nil
}

func TestApp_BindQueue_MustMatchRegisterQueue(t *testing.T) {
	app := &plugin.App{}
	pluginQ := &memQueue{}
	app.RegisterQueue(pluginQ)
	if err := app.BindQueue(&memQueue{}); err == nil {
		t.Fatal("expected error binding a different instance than RegisterQueue")
	}
	if err := app.BindQueue(pluginQ); err != nil {
		t.Fatalf("BindQueue same instance: %v", err)
	}
	if q, ok := app.Queue(); !ok || q != pluginQ {
		t.Fatal("Queue() must still report the plugin-registered instance")
	}
	if err := app.BindQueue(pluginQ); err != nil {
		t.Fatalf("idempotent re-bind: %v", err)
	}
}

func TestApp_BindQueue_AcceptsNotifyWrapper(t *testing.T) {
	app := &plugin.App{}
	pluginQ := &memQueue{}
	app.RegisterQueue(pluginQ)
	wrapped := app.WrapRequeueNotify(pluginQ)
	if err := app.BindQueue(wrapped); err != nil {
		t.Fatalf("BindQueue notify wrap of registered queue: %v", err)
	}
}

func TestApp_BindQueue_NilGuards(t *testing.T) {
	if err := (*plugin.App)(nil).BindQueue(&memQueue{}); err == nil {
		t.Fatal("expected nil app error")
	}
	if err := (&plugin.App{}).BindQueue(nil); err == nil {
		t.Fatal("expected nil queue error")
	}
}

func TestApp_BindQueue_IdempotentSameInstance(t *testing.T) {
	app := &plugin.App{}
	q := &memQueue{}
	if err := app.BindQueue(q); err != nil {
		t.Fatal(err)
	}
	if err := app.BindQueue(q); err != nil {
		t.Fatalf("re-bind same instance: %v", err)
	}
}

func TestApp_BoundQueue_PrefersBound(t *testing.T) {
	boundOnly := &plugin.App{}
	q := &memQueue{}
	if err := boundOnly.BindQueue(q); err != nil {
		t.Fatal(err)
	}
	got, ok := boundOnly.BoundQueue()
	if !ok || got != q {
		t.Fatalf("bound-only BoundQueue() = (%v, %v)", got, ok)
	}
	if _, ok := boundOnly.Queue(); ok {
		t.Fatal("Queue() should be empty when only BindQueue ran")
	}

	registered := &plugin.App{}
	r := &memQueue{}
	registered.RegisterQueue(r)
	if _, ok = registered.BoundQueue(); ok {
		t.Fatal("BoundQueue must be empty before BindQueue")
	}
	if err := registered.BindQueue(r); err != nil {
		t.Fatal(err)
	}
	got, ok = registered.BoundQueue()
	if !ok || got != r {
		t.Fatalf("after bind BoundQueue() = (%v, %v)", got, ok)
	}
}
