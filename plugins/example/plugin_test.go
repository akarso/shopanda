package example_test

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	integrationApp "github.com/akarso/shopanda/internal/application/integration"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/example"
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
func (q *memQueue) Dequeue(context.Context) (*jobs.Job, error) { return nil, nil }
func (q *memQueue) Complete(context.Context, string) error     { return nil }
func (q *memQueue) Fail(context.Context, string, error) error  { return nil }

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

func testApp(cfg *config.Config) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	app.SetPermissionRegistry(rbac.NewRegistry())
	return app
}

func TestPlugin_Name(t *testing.T) {
	if got := example.New().Name(); got != "example/demo" {
		t.Fatalf("Name() = %q, want example/demo", got)
	}
}

func TestPlugin_Init_DisabledConfigReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: false},
		},
	}
	if err := example.New().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when plugins.example.enabled is false")
	}
}

func TestPlugin_Init_RegistersPricingStepPermissionAndListener(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: true, FeeMinorUnits: 100},
		},
	}
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(example.New())
	app := testApp(cfg)
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v, want 1 initialized and 0 failed", summary)
	}
	app.FreezePermissionRegistry()
	rbac.BindRuntime(app.PermissionRegistry())
	t.Cleanup(rbac.UnbindRuntime)

	steps := app.PricingSteps()
	if len(steps) != 1 {
		t.Fatalf("PricingSteps() len = %d, want 1", len(steps))
	}
	step, ok := steps[0].(pricing.PricingStep)
	if !ok {
		t.Fatalf("PricingSteps()[0] type = %T, want pricing.PricingStep", steps[0])
	}
	if step.Name() != "example.fee" {
		t.Fatalf("step name = %q, want example.fee", step.Name())
	}

	jobs := app.SyncJobs()
	if len(jobs) != 1 || jobs[0].JobType != "integration.sync.example.demo.order_created" {
		t.Fatalf("SyncJobs() = %+v, want event demo.order_created", jobs)
	}
	if jobs[0].Job.Trigger.Kind != "event" || jobs[0].Job.Trigger.EventName != order.EventOrderCreated {
		t.Fatalf("trigger = %+v, want event %s", jobs[0].Job.Trigger, order.EventOrderCreated)
	}
	// Event→queue wiring is composition-root (RegisterSyncJobEventTriggers), not Init.
	if app.Bus.Handlers(order.EventOrderCreated) != 0 {
		t.Fatalf("order.created handlers = %d, want 0 until serve wires event triggers", app.Bus.Handlers(order.EventOrderCreated))
	}

	if !rbac.HasPermission(identity.RoleAdmin, example.PermissionReportsRead) {
		t.Fatal("expected example.reports.read for admin role")
	}
}

func TestPlugin_Init_RegistersCLICommand(t *testing.T) {
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(example.New())
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: true},
		},
	}
	if summary := reg.InitAll(testApp(cfg)); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v, want 1 initialized and 0 failed", summary)
	}

	cmds := reg.CLIRegistry().List()
	if len(cmds) != 1 || cmds[0].Name != example.CommandPing {
		t.Fatalf("CLI commands = %#v, want %q", cmds, example.CommandPing)
	}
}

func TestPlugin_Init_RegistersAdminConfig(t *testing.T) {
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(example.New())
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: true, FeeMinorUnits: 100},
		},
	}
	app := testApp(cfg)
	summary := reg.InitAll(app)
	if summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v, want 1 initialized and 0 failed", summary)
	}
	keys := reg.ConfigRegistry().Keys()
	if len(keys) != 1 || keys[0] != "plugins.example.fee_minor_units" {
		t.Fatalf("ConfigRegistry keys = %v, want [plugins.example.fee_minor_units]", keys)
	}
}

func TestExampleFeeStep_AddsFeeToPipeline(t *testing.T) {
	fee := int64(100)
	step := example.NewExampleFeeStep(&fee)
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	unit, err := shared.NewMoney(1000, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	item, err := pricing.NewPricingItem("v1", 1, unit)
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	pctx.Items = []pricing.PricingItem{item}

	pipe := pricing.NewPipeline(step, pricing.NewFinalizeStep())
	if err := pipe.Execute(context.Background(), &pctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if pctx.FeesTotal.Amount() != 100 {
		t.Fatalf("FeesTotal = %d, want 100", pctx.FeesTotal.Amount())
	}
	if pctx.GrandTotal.Amount() != 1100 {
		t.Fatalf("GrandTotal = %d, want 1100", pctx.GrandTotal.Amount())
	}
}

func TestExampleFeeStep_ReflectsRuntimeConfigChange(t *testing.T) {
	fee := int64(100)
	step := example.NewExampleFeeStep(&fee)
	pctx, err := pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	unit, err := shared.NewMoney(1000, "EUR")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	item, err := pricing.NewPricingItem("v1", 1, unit)
	if err != nil {
		t.Fatalf("NewPricingItem: %v", err)
	}
	pctx.Items = []pricing.PricingItem{item}
	pipe := pricing.NewPipeline(step, pricing.NewFinalizeStep())

	if err := pipe.Execute(context.Background(), &pctx); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if pctx.FeesTotal.Amount() != 100 {
		t.Fatalf("FeesTotal after first run = %d, want 100", pctx.FeesTotal.Amount())
	}

	fee = 250
	pctx, err = pricing.NewPricingContext("EUR")
	if err != nil {
		t.Fatalf("NewPricingContext: %v", err)
	}
	pctx.Items = []pricing.PricingItem{item}
	if err := pipe.Execute(context.Background(), &pctx); err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if pctx.FeesTotal.Amount() != 250 {
		t.Fatalf("FeesTotal after second run = %d, want 250", pctx.FeesTotal.Amount())
	}
}

func TestPlugin_OrderCreated_EnqueuesEventSyncJob(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: true},
		},
	}
	reg := plugin.NewRegistry(log)
	reg.Register(example.New())
	app := testApp(cfg)
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() = %+v", summary)
	}
	queue := &memQueue{}
	// Event triggers enqueue via the queue argument, not app.boundQueue.
	if err := integrationApp.RegisterSyncJobEventTriggers(app, app.Bus, queue, log); err != nil {
		t.Fatal(err)
	}

	evt := event.Event{
		Name: order.EventOrderCreated,
		ID:   "evt-1",
		Data: order.OrderCreatedData{OrderID: "ord-1", ItemCount: 2},
	}
	if err := app.Bus.Publish(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && queue.len() < 1 {
		time.Sleep(2 * time.Millisecond)
	}
	if queue.len() != 1 {
		t.Fatalf("enqueued = %d, want 1", queue.len())
	}
	got := queue.first()
	if got.Type != "integration.sync.example.demo.order_created" {
		t.Fatalf("type = %q", got.Type)
	}
	if got.Payload["trigger"] != extapi.SyncTriggerEvent {
		t.Fatalf("trigger = %#v, want event", got.Payload["trigger"])
	}
	// Real queues JSON-encode the envelope; assert the post-round-trip map shape.
	raw, err := json.Marshal(got.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	payload, _ := envelope["payload"].(map[string]interface{})
	data, _ := payload["data"].(map[string]interface{})
	if data["order_id"] != "ord-1" {
		t.Fatalf("data after JSON round-trip = %#v, want order_id=ord-1", data)
	}
	if data["item_count"] != float64(2) {
		t.Fatalf("item_count = %#v, want 2", data["item_count"])
	}
}

func TestPlugin_Enqueue_RunNowUsesBoundQueue(t *testing.T) {
	app := testApp(&config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: true},
		},
	})
	queue := &memQueue{}
	if err := app.BindQueue(queue); err != nil {
		t.Fatal(err)
	}
	sdk := pluginsdk.New(app, "example/demo")
	if err := sdk.Integration("example").RegisterManual("demo.ping", func(context.Context, extapi.SyncJobContext) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := sdk.Integration("example").Enqueue(context.Background(), "demo.ping", map[string]interface{}{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if queue.len() != 1 {
		t.Fatalf("enqueued = %d", queue.len())
	}
	got := queue.first()
	if got.Payload["trigger"] != extapi.SyncTriggerManual {
		t.Fatalf("trigger = %#v", got.Payload["trigger"])
	}
}
