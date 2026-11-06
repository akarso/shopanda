package plugin_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/platform/requestctx"
	"github.com/akarso/shopanda/pkg/extapi"
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
func (q *memQueue) Dequeue(context.Context) (*jobs.Job, error) { return nil, nil }
func (q *memQueue) Complete(context.Context, string) error     { return nil }
func (q *memQueue) Fail(context.Context, string, error) error  { return nil }

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
