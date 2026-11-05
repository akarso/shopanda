package plugin_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
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
	jobs := app.SyncJobs()
	if len(jobs) != 1 || jobs[0].JobType != "integration.sync.acme.warehouse.stock" {
		t.Fatalf("SyncJobs() = %+v", jobs)
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
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %d", len(queue.enqueued))
	}
	got := queue.enqueued[0]
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
		t.Fatalf("inner payload = %#v", inner)
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
	if err := app.BindQueue(&memQueue{}); err == nil {
		t.Fatal("expected error binding different instance")
	}
}
