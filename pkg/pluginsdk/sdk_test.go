package pluginsdk_test

import (
	"context"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

type memQueue struct {
	enqueued []jobs.Job
}

func (q *memQueue) Enqueue(_ context.Context, job jobs.Job) error {
	q.enqueued = append(q.enqueued, job)
	return nil
}
func (q *memQueue) Dequeue(context.Context) (*jobs.Job, error)                        { return nil, nil }
func (q *memQueue) Complete(context.Context, string, int) error                       { return nil }
func (q *memQueue) Fail(context.Context, string, int, error) error                    { return nil }
func (q *memQueue) Requeue(context.Context, string, int, ...jobs.RequeueOption) error { return nil }

func testApp() *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
	}
}

func TestAfterBeforePricingPosition(t *testing.T) {
	if got := string(pluginsdk.After("promotions")); got != "after:promotions" {
		t.Fatalf("After() = %q", got)
	}
	if got := string(pluginsdk.Before("tax")); got != "before:tax" {
		t.Fatalf("Before() = %q", got)
	}
	if got := string(pluginsdk.Replace("taxes")); got != "replace:taxes" {
		t.Fatalf("Replace() = %q", got)
	}
}

func TestCheckoutPositionHelpers(t *testing.T) {
	if got := string(pluginsdk.CheckoutStart()); got != "start" {
		t.Fatalf("CheckoutStart() = %q", got)
	}
	if got := string(pluginsdk.CheckoutEnd()); got != "end" {
		t.Fatalf("CheckoutEnd() = %q", got)
	}
	if got := string(pluginsdk.CheckoutBefore("order")); got != "before:order" {
		t.Fatalf("CheckoutBefore() = %q", got)
	}
}

func TestCheckout_Register(t *testing.T) {
	app := testApp()
	sdk := pluginsdk.New(app, "acme/demo")
	sdk.Checkout().Register(fakeCheckoutStep{name: "step-a"})
	sdk.Checkout().Register(fakeCheckoutStep{name: "step-b"}, pluginsdk.CheckoutBefore("create_order"))

	regs := app.CheckoutStepRegistrations()
	if len(regs) != 2 {
		t.Fatalf("registrations = %+v", regs)
	}
	if regs[0].Step.(named).Name() != "step-a" || regs[0].Position != "end" {
		t.Fatalf("first = %+v", regs[0])
	}
	if regs[1].Step.(named).Name() != "step-b" || regs[1].Position != "before:create_order" {
		t.Fatalf("second = %+v", regs[1])
	}
}

func TestPricing_Register(t *testing.T) {
	app := testApp()
	sdk := pluginsdk.New(app, "acme/demo")
	sdk.Pricing().Register(fakePricingStep{name: "step-a"})
	sdk.Pricing().Register(fakePricingStep{name: "step-b"}, pluginsdk.After("promotions"))

	regs := app.PricingStepRegistrations()
	if len(regs) != 2 {
		t.Fatalf("registrations = %+v", regs)
	}
	if regs[0].Step.(named).Name() != "step-a" || regs[0].Position != "after:base" {
		t.Fatalf("first = %+v", regs[0])
	}
	if regs[1].Step.(named).Name() != "step-b" || regs[1].Position != "after:promotions" {
		t.Fatalf("second = %+v", regs[1])
	}
}

func TestImport_RegisterRow(t *testing.T) {
	app := testApp()
	sdk := pluginsdk.New(app, "importdemo/reference")
	if err := sdk.Import().RegisterProductRow(100, func(*extapi.ImportRowContext) error {
		return nil
	}); err != nil {
		t.Fatalf("RegisterProductRow: %v", err)
	}
	if app.ImportRegistry() == nil {
		t.Fatal("expected import registry")
	}
}

func TestSyncJobs_RegisterCron(t *testing.T) {
	app := testApp()
	app.Bootstrap = &plugin.Bootstrap{}
	sdk := pluginsdk.New(app, "warehousedemo/reference")

	handler := func(context.Context, extapi.SyncJobContext) error { return nil }
	if err := sdk.Integration("warehousedemo").RegisterCron("warehouse.stock", "@every 5m", handler); err != nil {
		t.Fatalf("RegisterCron: %v", err)
	}
	jobs := app.SyncJobs()
	if len(jobs) != 1 || jobs[0].JobType != "integration.sync.warehousedemo.warehouse.stock" {
		t.Fatalf("SyncJobs() = %+v", jobs)
	}
}

func TestSyncJobs_RegisterOnEvent(t *testing.T) {
	app := testApp()
	app.Bootstrap = &plugin.Bootstrap{}
	sdk := pluginsdk.New(app, "acme/reference")

	handler := func(context.Context, extapi.SyncJobContext) error { return nil }
	if err := sdk.Integration("acme").RegisterOnEvent("pim.enrich", "catalog.product.updated", handler, pluginsdk.MaxRetries(3)); err != nil {
		t.Fatalf("RegisterOnEvent: %v", err)
	}
	jobs := app.SyncJobs()
	if len(jobs) != 1 || jobs[0].Job.MaxRetries != 3 {
		t.Fatalf("SyncJobs() = %+v", jobs)
	}
}

func TestSyncJobs_WithEventJobID(t *testing.T) {
	app := testApp()
	app.Bootstrap = &plugin.Bootstrap{}
	sdk := pluginsdk.New(app, "acme/reference")
	handler := func(context.Context, extapi.SyncJobContext) error { return nil }
	if err := sdk.Integration("acme").RegisterOnEvent("snap.order", "order.created", handler, pluginsdk.WithEventJobID(func(payload map[string]interface{}) string {
		return "550e8400-e29b-41d4-a716-446655440000"
	})); err != nil {
		t.Fatalf("RegisterOnEvent: %v", err)
	}
	if app.SyncJobs()[0].Job.EventJobID == nil {
		t.Fatal("expected EventJobID on registration")
	}
	if err := sdk.Integration("acme").RegisterCron("nightly", "@every 1h", handler, pluginsdk.WithEventJobID(func(map[string]interface{}) string {
		return "550e8400-e29b-41d4-a716-446655440000"
	})); err == nil {
		t.Fatal("expected EventJobID rejected on cron")
	}
}

func TestSyncJobs_RegisterManualAndEnqueue(t *testing.T) {
	app := testApp()
	app.Bootstrap = &plugin.Bootstrap{}
	queue := &memQueue{}
	if err := app.BindQueue(queue); err != nil {
		t.Fatal(err)
	}
	sdk := pluginsdk.New(app, "example/demo")
	handler := func(context.Context, extapi.SyncJobContext) error { return nil }
	if err := sdk.Integration("example").RegisterManual("demo.ping", handler); err != nil {
		t.Fatalf("RegisterManual: %v", err)
	}
	jobs := app.SyncJobs()
	if len(jobs) != 1 || jobs[0].Job.Trigger.Kind != extapi.SyncTriggerManual {
		t.Fatalf("SyncJobs() = %+v", jobs)
	}
	if err := sdk.Integration("example").Enqueue(context.Background(), "demo.ping", map[string]interface{}{"k": 1}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type != "integration.sync.example.demo.ping" {
		t.Fatalf("enqueued = %+v", queue.enqueued)
	}
	got := queue.enqueued[0]
	if got.Payload["trigger"] != extapi.SyncTriggerManual {
		t.Fatalf("trigger = %#v, want manual", got.Payload["trigger"])
	}
	inner, _ := got.Payload["payload"].(map[string]interface{})
	if inner["k"] != 1 {
		t.Fatalf("payload = %#v", inner)
	}
}

// idempotentQueue stores by job id; duplicate Enqueue → Conflict.
type idempotentQueue struct {
	byID     map[string]jobs.Job
	requeued []string
}

func (q *idempotentQueue) Enqueue(_ context.Context, job jobs.Job) error {
	if q.byID == nil {
		q.byID = map[string]jobs.Job{}
	}
	if _, ok := q.byID[job.ID]; ok {
		return apperror.Conflict("job already enqueued")
	}
	if job.Status == "" {
		job.Status = jobs.StatusPending
	}
	q.byID[job.ID] = job
	return nil
}
func (q *idempotentQueue) Dequeue(context.Context) (*jobs.Job, error)     { return nil, nil }
func (q *idempotentQueue) Complete(context.Context, string, int) error    { return nil }
func (q *idempotentQueue) Fail(context.Context, string, int, error) error { return nil }
func (q *idempotentQueue) Requeue(_ context.Context, id string, _ int, _ ...jobs.RequeueOption) error {
	q.requeued = append(q.requeued, id)
	return nil
}
func (q *idempotentQueue) List(context.Context, jobs.ListFilter) ([]jobs.Summary, error) {
	return nil, nil
}
func (q *idempotentQueue) Get(_ context.Context, id string) (*jobs.Detail, error) {
	job, ok := q.byID[id]
	if !ok {
		return nil, nil
	}
	return &jobs.Detail{
		Summary: jobs.Summary{ID: job.ID, Type: job.Type, Status: job.Status, Attempts: job.Attempts},
		Payload: job.Payload,
	}, nil
}
func (q *idempotentQueue) CountsByStatus(context.Context) (map[jobs.Status]int, error) {
	return nil, nil
}
func (q *idempotentQueue) CountsByType(context.Context, string) (map[jobs.Status]int, error) {
	return nil, nil
}

func TestSyncJobs_Enqueue_WithJobID_PendingConflictNoOp(t *testing.T) {
	app := testApp()
	app.Bootstrap = &plugin.Bootstrap{}
	queue := &idempotentQueue{}
	if err := app.BindQueue(queue); err != nil {
		t.Fatal(err)
	}
	port, err := pluginsdk.NewJobsPort(queue, queue)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.BindJobsPort(port); err != nil {
		t.Fatal(err)
	}
	sdk := pluginsdk.New(app, "acme/gen")
	if err := sdk.Integration("acme").RegisterManual("gen.run", func(context.Context, extapi.SyncJobContext) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	jobID := "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
	ctx := context.Background()
	if err := sdk.Integration("acme").Enqueue(ctx, "gen.run", map[string]interface{}{"n": 1}, pluginsdk.WithJobID(jobID)); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	if err := sdk.Integration("acme").Enqueue(ctx, "gen.run", map[string]interface{}{"n": 2}, pluginsdk.WithJobID(jobID)); err != nil {
		t.Fatalf("pending Conflict must succeed without Requeue: %v", err)
	}
	if len(queue.requeued) != 0 {
		t.Fatalf("pending must not Requeue: %v", queue.requeued)
	}
}

func TestSyncJobs_Enqueue_WithJobID_TerminalRequeues(t *testing.T) {
	app := testApp()
	app.Bootstrap = &plugin.Bootstrap{}
	queue := &idempotentQueue{}
	_ = app.BindQueue(queue)
	port, err := pluginsdk.NewJobsPort(queue, queue)
	if err != nil {
		t.Fatal(err)
	}
	_ = app.BindJobsPort(port)
	sdk := pluginsdk.New(app, "acme/gen")
	_ = sdk.Integration("acme").RegisterManual("gen.run", func(context.Context, extapi.SyncJobContext) error { return nil })
	jobID := "cccccccc-dddd-4eee-8fff-000000000001"
	ctx := context.Background()
	if err := sdk.Integration("acme").Enqueue(ctx, "gen.run", nil, pluginsdk.WithJobID(jobID)); err != nil {
		t.Fatal(err)
	}
	j := queue.byID[jobID]
	j.Status = jobs.StatusFailed
	j.Attempts = 2
	queue.byID[jobID] = j
	if err := sdk.Integration("acme").Enqueue(ctx, "gen.run", nil, pluginsdk.WithJobID(jobID)); err != nil {
		t.Fatalf("terminal Conflict: %v", err)
	}
	if len(queue.requeued) != 1 || queue.requeued[0] != jobID {
		t.Fatalf("requeued = %v", queue.requeued)
	}
}

func TestNew_PanicsOnNilApp(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil app")
		}
	}()
	pluginsdk.New(nil, "x")
}

func TestAppHost_OnQueueBoundAndOnRequeued(t *testing.T) {
	app := testApp()
	h := pluginsdk.AsHostFor(app, "acme/fence")
	var wrapped bool
	h.OnQueueBound(func(queue any) any {
		wrapped = true
		return queue
	})
	var seen string
	var seenAttempt int
	h.OnRequeued(func(_ context.Context, jobID string, attempt int) error {
		seen = jobID
		seenAttempt = attempt
		return nil
	})

	q := &memQueue{}
	got, err := app.ApplyQueueBoundHooks(q)
	if err != nil {
		t.Fatal(err)
	}
	if !wrapped || got != q {
		t.Fatalf("OnQueueBound wrap applied=%v got=%v", wrapped, got)
	}
	nq := app.WrapRequeueNotify(q)
	if err := nq.Requeue(context.Background(), "job-9", 2); err != nil {
		t.Fatal(err)
	}
	if seen != "job-9" || seenAttempt != 2 {
		t.Fatalf("OnRequeued seen=%q attempt=%d", seen, seenAttempt)
	}
}
