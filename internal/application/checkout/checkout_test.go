package checkout_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/metrics"
)

// --- Mock step ---

type mockStep struct {
	name string
	fn   func(ctx *checkout.Context) error
}

func (s *mockStep) Name() string                                           { return s.name }
func (s *mockStep) Execute(_ context.Context, ctx *checkout.Context) error { return s.fn(ctx) }

// --- Mock cart repository ---

type mockCartRepo struct {
	cart *cart.Cart
	err  error
}

func (r *mockCartRepo) FindByID(_ context.Context, _ string) (*cart.Cart, error) {
	return r.cart, r.err
}
func (r *mockCartRepo) FindActiveByCustomerID(_ context.Context, _ string) (*cart.Cart, error) {
	return nil, nil
}
func (r *mockCartRepo) Save(_ context.Context, _ *cart.Cart) error { return nil }
func (r *mockCartRepo) Delete(_ context.Context, _ string) error   { return nil }
func (r *mockCartRepo) FindRecoveryCandidates(_ context.Context, _ time.Time, _ int) ([]*cart.Cart, error) {
	return nil, nil
}
func (r *mockCartRepo) MarkRecoveryEmailSent(_ context.Context, _ string, _ time.Time) (bool, error) {
	return false, nil
}

// --- Helpers ---

func testLogger() logger.Logger {
	return logger.NewWithWriter(&bytes.Buffer{}, "error")
}

func testBus(t *testing.T) *event.Bus {
	t.Helper()
	return event.NewBus(testLogger())
}

func activeCart(t *testing.T, customerID string) *cart.Cart {
	t.Helper()
	c, err := cart.NewCart(id.New(), "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID(customerID); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	price := shared.MustNewMoney(1000, "EUR")
	if err := c.AddItem("var-1", 2, price); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	return &c
}

func activeGuestCart(t *testing.T) *cart.Cart {
	t.Helper()
	c, err := cart.NewCart(id.New(), "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	price := shared.MustNewMoney(1000, "EUR")
	if err := c.AddItem("var-1", 2, price); err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	return &c
}

func validCheckoutInput() checkout.Input {
	return checkout.Input{
		Address: checkout.Address{
			FirstName: "Ada",
			LastName:  "Lovelace",
			Street:    "1 Logic Lane",
			City:      "Berlin",
			Postcode:  "10115",
			Country:   "DE",
		},
		ContactEmail: "ada@example.com",
	}
}

// ============================================================
// Context tests
// ============================================================

func TestNewContext(t *testing.T) {
	ctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if ctx.CartID != "cart-1" {
		t.Errorf("CartID = %q, want cart-1", ctx.CartID)
	}
	if ctx.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want cust-1", ctx.CustomerID)
	}
	if ctx.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR", ctx.Currency)
	}
	if ctx.Meta == nil {
		t.Error("Meta should not be nil")
	}
}

func TestContext_Meta(t *testing.T) {
	ctx := checkout.NewContext("c", "cu", "EUR")
	ctx.SetMeta("created_order", true)
	v, ok := ctx.GetMeta("created_order")
	if !ok {
		t.Fatal("expected meta key to exist")
	}
	if v != true {
		t.Errorf("GetMeta = %v, want true", v)
	}
	_, ok = ctx.GetMeta("missing")
	if ok {
		t.Error("expected missing key to return false")
	}
}

// ============================================================
// Workflow tests
// ============================================================

func TestWorkflow_EmptySteps(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	wf := checkout.NewWorkflow(nil, bus, log)

	ctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if err := wf.Execute(context.Background(), ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(ctx.Trace) != 0 {
		t.Errorf("Trace len = %d, want 0", len(ctx.Trace))
	}
}

func TestWorkflow_ExecutesInOrder(t *testing.T) {
	bus := testBus(t)
	log := testLogger()

	var order []string
	stepA := &mockStep{name: "step_a", fn: func(_ *checkout.Context) error {
		order = append(order, "a")
		return nil
	}}
	stepB := &mockStep{name: "step_b", fn: func(_ *checkout.Context) error {
		order = append(order, "b")
		return nil
	}}

	wf := checkout.NewWorkflow([]checkout.Step{stepA, stepB}, bus, log)

	ctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if err := wf.Execute(context.Background(), ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Errorf("execution order = %v, want [a b]", order)
	}
	if len(ctx.Trace) != 2 {
		t.Fatalf("Trace len = %d, want 2", len(ctx.Trace))
	}
	if ctx.Trace[0].Step != "step_a" || ctx.Trace[0].Status != "ok" {
		t.Errorf("Trace[0] = %+v, want step_a/ok", ctx.Trace[0])
	}
	if ctx.Trace[1].Step != "step_b" || ctx.Trace[1].Status != "ok" {
		t.Errorf("Trace[1] = %+v, want step_b/ok", ctx.Trace[1])
	}
}

func TestWorkflow_StopsOnError(t *testing.T) {
	bus := testBus(t)
	log := testLogger()

	stepOK := &mockStep{name: "ok_step", fn: func(_ *checkout.Context) error {
		return nil
	}}
	stepFail := &mockStep{name: "fail_step", fn: func(_ *checkout.Context) error {
		return errors.New("boom")
	}}
	stepNever := &mockStep{name: "never_step", fn: func(_ *checkout.Context) error {
		t.Fatal("should not execute")
		return nil
	}}

	wf := checkout.NewWorkflow([]checkout.Step{stepOK, stepFail, stepNever}, bus, log)

	ctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	err := wf.Execute(context.Background(), ctx)
	if err == nil {
		t.Fatal("expected error from workflow")
	}
	if len(ctx.Trace) != 2 {
		t.Fatalf("Trace len = %d, want 2", len(ctx.Trace))
	}
	if ctx.Trace[0].Status != "ok" {
		t.Errorf("Trace[0].Status = %q, want ok", ctx.Trace[0].Status)
	}
	if ctx.Trace[1].Status != "error" {
		t.Errorf("Trace[1].Status = %q, want error", ctx.Trace[1].Status)
	}
	if ctx.Trace[1].Err != "boom" {
		t.Errorf("Trace[1].Err = %q, want boom", ctx.Trace[1].Err)
	}
}

func TestWorkflow_EmitsEvents(t *testing.T) {
	bus := testBus(t)
	log := testLogger()

	var events []string
	bus.On(checkout.EventStepStarted, func(_ context.Context, evt event.Event) error {
		events = append(events, "started:"+evt.Data.(checkout.StepStartedData).StepName)
		return nil
	})
	bus.On(checkout.EventStepCompleted, func(_ context.Context, evt event.Event) error {
		events = append(events, "completed:"+evt.Data.(checkout.StepCompletedData).StepName)
		return nil
	})
	bus.On(checkout.EventCheckoutCompleted, func(_ context.Context, _ event.Event) error {
		events = append(events, "checkout.completed")
		return nil
	})

	step := &mockStep{name: "my_step", fn: func(_ *checkout.Context) error { return nil }}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log)

	ctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if err := wf.Execute(context.Background(), ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := []string{"started:my_step", "completed:my_step", "checkout.completed"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("events[%d] = %q, want %q", i, events[i], want[i])
		}
	}
}

func TestWorkflow_EmitsFailedEvent(t *testing.T) {
	bus := testBus(t)
	log := testLogger()

	var failedCartID string
	bus.On(checkout.EventCheckoutFailed, func(_ context.Context, evt event.Event) error {
		failedCartID = evt.Data.(checkout.CheckoutFailedData).CartID
		return nil
	})

	step := &mockStep{name: "bad", fn: func(_ *checkout.Context) error {
		return errors.New("fail")
	}}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log)

	ctx := checkout.NewContext("cart-99", "cust-1", "EUR")
	_ = wf.Execute(context.Background(), ctx)
	if failedCartID != "cart-99" {
		t.Errorf("failed event CartID = %q, want cart-99", failedCartID)
	}
}

func TestWorkflow_StepMutatesContext(t *testing.T) {
	bus := testBus(t)
	log := testLogger()

	step := &mockStep{name: "setter", fn: func(ctx *checkout.Context) error {
		ctx.SetMeta("done", true)
		return nil
	}}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log)

	ctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if err := wf.Execute(context.Background(), ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	v, ok := ctx.GetMeta("done")
	if !ok || v != true {
		t.Error("expected meta key done=true after step execution")
	}
}

func TestWorkflow_StepsCount(t *testing.T) {
	bus := testBus(t)
	log := testLogger()

	wf := checkout.NewWorkflow(nil, bus, log)
	if wf.Steps() != 0 {
		t.Errorf("Steps() = %d, want 0", wf.Steps())
	}

	step := &mockStep{name: "a", fn: func(_ *checkout.Context) error { return nil }}
	wf2 := checkout.NewWorkflow([]checkout.Step{step}, bus, log)
	if wf2.Steps() != 1 {
		t.Errorf("Steps() = %d, want 1", wf2.Steps())
	}
}

// ============================================================
// Service tests
// ============================================================

func TestService_StartCheckout_Success(t *testing.T) {
	bus := testBus(t)
	log := testLogger()

	c := activeCart(t, "cust-1")
	repo := &mockCartRepo{cart: c}

	var executed bool
	step := &mockStep{name: "test_step", fn: func(_ *checkout.Context) error {
		executed = true
		return nil
	}}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log)
	svc := checkout.NewService(repo, wf, log)

	result, err := svc.StartCheckout(context.Background(), c.ID, "cust-1", validCheckoutInput())
	if err != nil {
		t.Fatalf("StartCheckout: %v", err)
	}
	if !executed {
		t.Error("workflow step was not executed")
	}
	if result.CartID != c.ID {
		t.Errorf("CartID = %q, want %q", result.CartID, c.ID)
	}
	if result.CustomerID != "cust-1" {
		t.Errorf("CustomerID = %q, want cust-1", result.CustomerID)
	}
	if result.Currency != "EUR" {
		t.Errorf("Currency = %q, want EUR", result.Currency)
	}
	if result.Cart == nil {
		t.Error("Cart should not be nil")
	}
}

func TestService_StartCheckout_PersistsInput(t *testing.T) {
	bus := testBus(t)
	log := testLogger()

	c := activeCart(t, "cust-1")
	repo := &mockCartRepo{cart: c}
	wf := checkout.NewWorkflow(nil, bus, log)
	svc := checkout.NewService(repo, wf, log)

	input := validCheckoutInput()
	input = checkout.Input{
		Address:        input.Address,
		ContactEmail:   "ada@example.com",
		ShippingMethod: "flat_rate",
		PaymentMethod:  "manual",
	}

	result, err := svc.StartCheckout(context.Background(), c.ID, "cust-1", input)
	if err != nil {
		t.Fatalf("StartCheckout: %v", err)
	}
	if result.Input != input {
		t.Fatalf("Input = %#v, want %#v", result.Input, input)
	}
	if raw, ok := result.GetMeta("checkout_address"); !ok || raw != input.Address {
		t.Fatalf("checkout_address meta = %#v, want %#v", raw, input.Address)
	}
	if raw, ok := result.GetMeta("checkout_shipping_method"); !ok || raw != input.ShippingMethod {
		t.Fatalf("checkout_shipping_method meta = %#v, want %q", raw, input.ShippingMethod)
	}
	if raw, ok := result.GetMeta("checkout_payment_method"); !ok || raw != input.PaymentMethod {
		t.Fatalf("checkout_payment_method meta = %#v, want %q", raw, input.PaymentMethod)
	}
	if raw, ok := result.GetMeta("checkout_contact_email"); !ok || raw != input.ContactEmail {
		t.Fatalf("checkout_contact_email meta = %#v, want %q", raw, input.ContactEmail)
	}
}

func TestService_StartCheckout_EmptyCartID(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	wf := checkout.NewWorkflow(nil, bus, log)
	svc := checkout.NewService(&mockCartRepo{}, wf, log)

	_, err := svc.StartCheckout(context.Background(), "", "cust-1", checkout.Input{})
	if err == nil {
		t.Fatal("expected error for empty cart id")
	}
}

func TestService_StartCheckout_EmptyCustomerIDForbiddenForCustomerCart(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	wf := checkout.NewWorkflow(nil, bus, log)

	c := activeCart(t, "cust-1")
	repo := &mockCartRepo{cart: c}
	svc := checkout.NewService(repo, wf, log)

	_, err := svc.StartCheckout(context.Background(), c.ID, "", validCheckoutInput())
	if err == nil {
		t.Fatal("expected forbidden error for mismatched customer cart")
	}
}

func TestService_StartCheckout_GuestCartSuccess(t *testing.T) {
	bus := testBus(t)
	log := testLogger()

	c := activeGuestCart(t)
	repo := &mockCartRepo{cart: c}

	var executed bool
	step := &mockStep{name: "test_step", fn: func(_ *checkout.Context) error {
		executed = true
		return nil
	}}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log)
	svc := checkout.NewService(repo, wf, log)

	result, err := svc.StartCheckout(context.Background(), c.ID, "", validCheckoutInput())
	if err != nil {
		t.Fatalf("StartCheckout: %v", err)
	}
	if !executed {
		t.Error("workflow step was not executed")
	}
	if result.CustomerID != "" {
		t.Errorf("CustomerID = %q, want empty guest id", result.CustomerID)
	}
	if result.Input.ContactEmail == "" {
		t.Fatal("guest checkout should persist contact email")
	}
}

func TestService_StartCheckout_GuestCartMissingContactEmail(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	wf := checkout.NewWorkflow(nil, bus, log)

	c := activeGuestCart(t)
	repo := &mockCartRepo{cart: c}
	svc := checkout.NewService(repo, wf, log)

	input := validCheckoutInput()
	input.ContactEmail = ""
	_, err := svc.StartCheckout(context.Background(), c.ID, "", input)
	if err == nil {
		t.Fatal("expected error for missing guest contact email")
	}
}

func TestService_StartCheckout_MissingAddress(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	wf := checkout.NewWorkflow(nil, bus, log)

	c := activeCart(t, "cust-1")
	repo := &mockCartRepo{cart: c}
	svc := checkout.NewService(repo, wf, log)

	_, err := svc.StartCheckout(context.Background(), c.ID, "cust-1", checkout.Input{})
	if err == nil {
		t.Fatal("expected error for missing address")
	}
}

func TestService_StartCheckout_InvalidCountry(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	wf := checkout.NewWorkflow(nil, bus, log)

	c := activeCart(t, "cust-1")
	repo := &mockCartRepo{cart: c}
	svc := checkout.NewService(repo, wf, log)

	input := validCheckoutInput()
	input.Address.Country = "Germany"
	_, err := svc.StartCheckout(context.Background(), c.ID, "cust-1", input)
	if err == nil {
		t.Fatal("expected error for non-ISO country")
	}
}

func TestService_StartCheckout_CartNotFound(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	wf := checkout.NewWorkflow(nil, bus, log)
	repo := &mockCartRepo{cart: nil}
	svc := checkout.NewService(repo, wf, log)

	_, err := svc.StartCheckout(context.Background(), "nonexistent", "cust-1", validCheckoutInput())
	if err == nil {
		t.Fatal("expected error for missing cart")
	}
}

func TestService_StartCheckout_InactiveCart(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	wf := checkout.NewWorkflow(nil, bus, log)

	c := activeCart(t, "cust-1")
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	repo := &mockCartRepo{cart: c}
	svc := checkout.NewService(repo, wf, log)

	_, err := svc.StartCheckout(context.Background(), c.ID, "cust-1", validCheckoutInput())
	if err == nil {
		t.Fatal("expected error for inactive cart")
	}
}

func TestService_StartCheckout_WrongCustomer(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	wf := checkout.NewWorkflow(nil, bus, log)

	c := activeCart(t, "cust-1")
	repo := &mockCartRepo{cart: c}
	svc := checkout.NewService(repo, wf, log)

	_, err := svc.StartCheckout(context.Background(), c.ID, "cust-OTHER", validCheckoutInput())
	if err == nil {
		t.Fatal("expected error for wrong customer")
	}
}

func TestService_StartCheckout_EmptyCart(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	wf := checkout.NewWorkflow(nil, bus, log)

	c, err := cart.NewCart(id.New(), "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	repo := &mockCartRepo{cart: &c}
	svc := checkout.NewService(repo, wf, log)

	_, err = svc.StartCheckout(context.Background(), c.ID, "cust-1", validCheckoutInput())
	if err == nil {
		t.Fatal("expected error for empty cart")
	}
}

func TestService_StartCheckout_WorkflowError(t *testing.T) {
	bus := testBus(t)
	log := testLogger()

	c := activeCart(t, "cust-1")
	repo := &mockCartRepo{cart: c}

	step := &mockStep{name: "fail_step", fn: func(_ *checkout.Context) error {
		return errors.New("workflow failure")
	}}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log)
	svc := checkout.NewService(repo, wf, log)

	result, err := svc.StartCheckout(context.Background(), c.ID, "cust-1", validCheckoutInput())
	if err == nil {
		t.Fatal("expected error from workflow")
	}
	if result == nil {
		t.Fatal("expected context even on workflow error")
	}
	if len(result.Trace) != 1 {
		t.Fatalf("Trace len = %d, want 1", len(result.Trace))
	}
	if result.Trace[0].Status != "error" {
		t.Errorf("Trace[0].Status = %q, want error", result.Trace[0].Status)
	}
}

// --- Metrics ---

type mockMetricsRecorder struct {
	checkoutOutcomes []string
}

func (m *mockMetricsRecorder) HTTPRequest(string, string, string, time.Duration) {}
func (m *mockMetricsRecorder) CheckoutResult(outcome string) {
	m.checkoutOutcomes = append(m.checkoutOutcomes, outcome)
}
func (m *mockMetricsRecorder) JobFailure(string)      {}
func (m *mockMetricsRecorder) WebhookDelivery(string) {}

func TestWorkflow_WithMetrics_RecordsSuccess(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	m := &mockMetricsRecorder{}

	step := &mockStep{name: "ok_step", fn: func(_ *checkout.Context) error { return nil }}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log).WithMetrics(m)

	ctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if err := wf.Execute(context.Background(), ctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(m.checkoutOutcomes) != 1 || m.checkoutOutcomes[0] != "success" {
		t.Errorf("checkoutOutcomes = %v, want [success]", m.checkoutOutcomes)
	}
}

func TestWorkflow_WithMetrics_RecordsFailure(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	m := &mockMetricsRecorder{}

	step := &mockStep{name: "fail_step", fn: func(_ *checkout.Context) error { return errors.New("boom") }}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log).WithMetrics(m)

	ctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if err := wf.Execute(context.Background(), ctx); err == nil {
		t.Fatal("expected error from workflow")
	}
	if len(m.checkoutOutcomes) != 1 || m.checkoutOutcomes[0] != "failed" {
		t.Errorf("checkoutOutcomes = %v, want [failed]", m.checkoutOutcomes)
	}
}

func TestWorkflow_WithMetrics_RecordsPanicAsFailure(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	m := &mockMetricsRecorder{}

	step := &panicStep{name: "panic_step"}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log).WithMetrics(m)

	ctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	panicked := false
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic to propagate")
			}
			panicked = true
		}()
		_ = wf.Execute(context.Background(), ctx)
	}()
	if !panicked {
		t.Fatal("expected panic from step")
	}
	if len(m.checkoutOutcomes) != 1 || m.checkoutOutcomes[0] != "failed" {
		t.Errorf("checkoutOutcomes = %v, want [failed] for panicking step", m.checkoutOutcomes)
	}
}

// TestWorkflow_Execute_NilContextPanicIsCaughtByRecover pins a bug where the
// tracing span's start (reading cctx.CartID) ran ahead of the recover
// defer's registration. A nil *checkout.Context still panics inside the
// loop body (cctx.CartID is read unconditionally for logging before any
// Step runs — pre-existing, not itself the bug), but that panic must be
// caught by the SAME recover every other panic source in this function
// goes through: metrics recorded, span closed with an error status, then
// re-panicked. Before the fix, the span-start's own cctx.CartID access
// panicked before the recover defer was even registered — an unrecovered
// crash with no metrics or span cleanup at all, unlike this one.
func TestWorkflow_Execute_NilContextPanicIsCaughtByRecover(t *testing.T) {
	exporter := withTestTracerProvider(t)
	bus := testBus(t)
	log := testLogger()
	m := &mockMetricsRecorder{}

	step := &mockStep{name: "unreached_step", fn: func(_ *checkout.Context) error { return nil }}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log).WithMetrics(m)

	panicked := false
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected a nil *checkout.Context to panic (cctx.CartID access predates any step)")
			}
			panicked = true
		}()
		_ = wf.Execute(context.Background(), nil)
	}()
	if !panicked {
		t.Fatal("expected panic")
	}

	if len(m.checkoutOutcomes) != 1 || m.checkoutOutcomes[0] != metrics.OutcomeFailed {
		t.Errorf("checkoutOutcomes = %v, want [%s] — the recover must still run metrics before re-panicking", m.checkoutOutcomes, metrics.OutcomeFailed)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "checkout.execute" {
		t.Fatalf("expected exactly one closed checkout.execute span, got %+v", spans)
	}
	if spans[0].Status.Code != codes.Error {
		t.Errorf("span status = %v, want Error — the recover must close the span before re-panicking", spans[0].Status.Code)
	}
}

type panicStep struct{ name string }

func (panicStep) Name() string { return "panic_step" }
func (panicStep) Execute(_ context.Context, _ *checkout.Context) error {
	panic("checkout step blew up")
}

// panicValueStep panics with an arbitrary caller-supplied value, used to
// pin that the span recording of a panic doesn't leak that value.
type panicValueStep struct {
	name  string
	value any
}

func (s panicValueStep) Name() string { return s.name }
func (s panicValueStep) Execute(_ context.Context, _ *checkout.Context) error {
	panic(s.value)
}

// TestWorkflow_WithMetrics_RecordsSucceededEventFailed pins the distinction
// between a real checkout failure and every step succeeding but the final
// EventCheckoutCompleted publish failing — the order/payment side is fine,
// so it must not show up in the same failure-rate bucket as a broken step.
//
// Execute must also NOT return an error in this case (fixed in review):
// checkout.Service.Checkout propagates Execute's error as-is to its
// caller, which would otherwise report a fully successful checkout (order
// created, payment captured) as a failure — the HTTP layer would tell the
// customer to retry, risking a genuine duplicate order, over what is
// actually just a downstream notification/event-bus problem.
// succeededEventFailed (and the distinct metrics outcome below) is what
// keeps this visible to operators instead.
func TestWorkflow_WithMetrics_RecordsSucceededEventFailed(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	m := &mockMetricsRecorder{}

	bus.On(checkout.EventCheckoutCompleted, func(_ context.Context, _ event.Event) error {
		return errors.New("notification service unreachable")
	})

	step := &mockStep{name: "ok_step", fn: func(_ *checkout.Context) error { return nil }}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log).WithMetrics(m)

	ctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if err := wf.Execute(context.Background(), ctx); err != nil {
		t.Fatalf("Execute returned %v, want nil — the order/payment succeeded, only the completion notification failed", err)
	}
	if len(m.checkoutOutcomes) != 1 || m.checkoutOutcomes[0] != metrics.OutcomeSucceededEventFailed {
		t.Errorf("checkoutOutcomes = %v, want [%s]", m.checkoutOutcomes, metrics.OutcomeSucceededEventFailed)
	}
}

func TestWorkflow_WithoutMetrics_DoesNotPanic(t *testing.T) {
	bus := testBus(t)
	log := testLogger()

	step := &mockStep{name: "fail_step", fn: func(_ *checkout.Context) error { return errors.New("boom") }}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log) // no WithMetrics call

	ctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	_ = wf.Execute(context.Background(), ctx)
}

// ============================================================
// Tracing tests
// ============================================================

// withTestTracerProvider installs an SDK provider backed by an in-memory
// exporter for the duration of the test, restoring the previous global
// provider on cleanup. checkout.NewWorkflow resolves its tracer via
// otel.Tracer(...) at construction time, so the provider must be installed
// before NewWorkflow is called in each test below, not after.
func withTestTracerProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return exporter
}

func TestWorkflow_Execute_RecordsSpansPerStepUnderRootSpan(t *testing.T) {
	exporter := withTestTracerProvider(t)
	bus := testBus(t)
	log := testLogger()

	step1 := &mockStep{name: "validate", fn: func(_ *checkout.Context) error { return nil }}
	step2 := &mockStep{name: "reserve", fn: func(_ *checkout.Context) error { return nil }}
	wf := checkout.NewWorkflow([]checkout.Step{step1, step2}, bus, log)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if err := wf.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	spans := exporter.GetSpans()
	byName := map[string]tracetest.SpanStub{}
	for _, s := range spans {
		byName[s.Name] = s
	}
	if _, ok := byName["checkout.execute"]; !ok {
		t.Fatalf("expected a checkout.execute root span, got %+v", byName)
	}
	if _, ok := byName["checkout.step.validate"]; !ok {
		t.Errorf("expected a checkout.step.validate span, got %+v", byName)
	}
	if _, ok := byName["checkout.step.reserve"]; !ok {
		t.Errorf("expected a checkout.step.reserve span, got %+v", byName)
	}

	root := byName["checkout.execute"]
	for name, s := range byName {
		if name == "checkout.execute" {
			continue
		}
		if s.Parent.SpanID() != root.SpanContext.SpanID() {
			t.Errorf("span %q parent = %v, want root span %v", name, s.Parent.SpanID(), root.SpanContext.SpanID())
		}
	}
	if root.Status.Code != codes.Ok {
		t.Errorf("root span status = %v, want Ok for a successful checkout", root.Status.Code)
	}
}

func TestWorkflow_Execute_RecordsErrorStatusOnStepFailure(t *testing.T) {
	exporter := withTestTracerProvider(t)
	bus := testBus(t)
	log := testLogger()

	step := &mockStep{name: "reserve", fn: func(_ *checkout.Context) error { return errors.New("out of stock") }}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected an error from the failing step")
	}

	var rootStatus, stepStatus codes.Code
	found := 0
	for _, s := range exporter.GetSpans() {
		switch s.Name {
		case "checkout.execute":
			rootStatus = s.Status.Code
			found++
		case "checkout.step.reserve":
			stepStatus = s.Status.Code
			found++
		}
	}
	if found != 2 {
		t.Fatalf("expected both root and step spans, found %d", found)
	}
	if rootStatus != codes.Error {
		t.Errorf("root span status = %v, want Error", rootStatus)
	}
	if stepStatus != codes.Error {
		t.Errorf("step span status = %v, want Error", stepStatus)
	}
}

// TestWorkflow_Execute_PanicInStepStillClosesStepSpan pins a leak where a
// panicking step never reached the straight-line stepSpan.End() call that
// used to sit after step.Execute — only the root span (via Execute's own
// recover) got closed, and the step's own span stayed open forever. The
// fix runs the step through a helper that ends the span via defer, so it
// closes (with an error status reflecting the panic) no matter how
// step.Execute exits.
func TestWorkflow_Execute_PanicInStepStillClosesStepSpan(t *testing.T) {
	exporter := withTestTracerProvider(t)
	bus := testBus(t)
	log := testLogger()

	step := &panicStep{name: "reserve"}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected the panic to propagate out of Execute")
			}
		}()
		_ = wf.Execute(context.Background(), cctx)
	}()

	var stepSpanFound bool
	var stepStatus codes.Code
	for _, s := range exporter.GetSpans() {
		if s.Name == "checkout.step.panic_step" {
			stepSpanFound = true
			stepStatus = s.Status.Code
		}
	}
	if !stepSpanFound {
		t.Fatal("checkout.step.panic_step span was never recorded — it leaked without ever being ended")
	}
	if stepStatus != codes.Error {
		t.Errorf("step span status = %v, want Error for a panicking step", stepStatus)
	}
}

// TestWorkflow_Execute_PanicSpanRedactsPanicValue pins the fix for
// recording a recovered panic's raw value (fmt.Errorf("panic: %v", r)) on
// the step span: a panic value is caller-controlled and may carry
// customer or payment-processor detail, the same risk spanSafeError
// already guards against for normal errors. The span must record a fixed
// "panic" message instead, while the original value still propagates via
// the re-panic (verified via recover() below).
func TestWorkflow_Execute_PanicSpanRedactsPanicValue(t *testing.T) {
	exporter := withTestTracerProvider(t)
	bus := testBus(t)
	log := testLogger()

	const sensitive = "customer jane.doe@example.com has an invalid card"
	step := panicValueStep{name: "reserve", value: sensitive}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("expected the panic to propagate out of Execute")
			}
			if r != sensitive {
				t.Errorf("recovered panic value = %v, want the original value %q to still propagate", r, sensitive)
			}
		}()
		_ = wf.Execute(context.Background(), cctx)
	}()

	for _, s := range exporter.GetSpans() {
		if strings.Contains(s.Status.Description, sensitive) {
			t.Fatalf("span %q status description leaked the raw panic value: %q", s.Name, s.Status.Description)
		}
		for _, ev := range s.Events {
			for _, attr := range ev.Attributes {
				if strings.Contains(attr.Value.AsString(), sensitive) {
					t.Fatalf("span %q event attribute %q leaked the raw panic value", s.Name, attr.Key)
				}
			}
		}
	}
}

// TestWorkflow_Execute_SpanRedactsErrorMessage pins the fix for recording
// raw err.Error() strings on checkout spans: an error message is not
// guaranteed free of customer or payment-processor detail, and these
// spans may export to a third-party OTLP collector. An *apperror.Error's
// bounded Code must appear on the span instead of its free-text message.
func TestWorkflow_Execute_SpanRedactsErrorMessage(t *testing.T) {
	exporter := withTestTracerProvider(t)
	bus := testBus(t)
	log := testLogger()

	const sensitive = "card ending 4242 declined for customer jane.doe@example.com"
	step := &mockStep{name: "pay", fn: func(_ *checkout.Context) error {
		return apperror.Wrap(apperror.CodeValidation, sensitive, errors.New(sensitive))
	}}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected an error from the failing step")
	}

	for _, s := range exporter.GetSpans() {
		if s.Status.Description == "" {
			continue
		}
		if strings.Contains(s.Status.Description, sensitive) {
			t.Fatalf("span %q status description leaked the raw error message: %q", s.Name, s.Status.Description)
		}
		for _, ev := range s.Events {
			for _, attr := range ev.Attributes {
				if strings.Contains(attr.Value.AsString(), sensitive) {
					t.Fatalf("span %q event attribute %q leaked the raw error message", s.Name, attr.Key)
				}
			}
		}
	}

	var stepStatusDesc string
	for _, s := range exporter.GetSpans() {
		if s.Name == "checkout.step.pay" {
			stepStatusDesc = s.Status.Description
		}
	}
	if stepStatusDesc != string(apperror.CodeValidation) {
		t.Errorf("step span status description = %q, want the bounded apperror code %q", stepStatusDesc, apperror.CodeValidation)
	}
}

// TestWorkflow_Execute_SpanPreservesContextErrorMessage confirms the
// redaction above doesn't over-apply: context.Canceled/DeadlineExceeded
// are fixed, safe strings with no PII risk, and stay legible on the span.
func TestWorkflow_Execute_SpanPreservesContextErrorMessage(t *testing.T) {
	exporter := withTestTracerProvider(t)
	bus := testBus(t)
	log := testLogger()

	step := &mockStep{name: "slow", fn: func(_ *checkout.Context) error { return nil }}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if err := wf.Execute(ctx, cctx); err == nil {
		t.Fatal("expected a context-cancellation error")
	}

	var rootStatusDesc string
	for _, s := range exporter.GetSpans() {
		if s.Name == "checkout.execute" {
			rootStatusDesc = s.Status.Description
		}
	}
	if rootStatusDesc != context.Canceled.Error() {
		t.Errorf("root span status description = %q, want the unredacted context.Canceled message %q", rootStatusDesc, context.Canceled.Error())
	}
}

// TestWorkflow_Execute_SpanRedactsWrappedContextErrorPrefix pins the fix
// for a step that wraps context.Canceled/DeadlineExceeded with its own
// message (e.g. "apply payment for customer ...: %w", ctx.Err()):
// errors.Is still matches through the wrapping, but the previous code
// returned the wrapped error as-is, letting whatever the step's prefix
// said reach the span unbounded. The fix returns the bare sentinel.
func TestWorkflow_Execute_SpanRedactsWrappedContextErrorPrefix(t *testing.T) {
	exporter := withTestTracerProvider(t)
	bus := testBus(t)
	log := testLogger()

	const sensitive = "customer jane.doe@example.com"
	step := &mockStep{name: "pay", fn: func(_ *checkout.Context) error {
		return fmt.Errorf("apply payment for %s: %w", sensitive, context.Canceled)
	}}
	wf := checkout.NewWorkflow([]checkout.Step{step}, bus, log)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	if err := wf.Execute(context.Background(), cctx); err == nil {
		t.Fatal("expected an error from the failing step")
	}

	for _, s := range exporter.GetSpans() {
		if strings.Contains(s.Status.Description, sensitive) {
			t.Fatalf("span %q status description leaked the wrapping prefix: %q", s.Name, s.Status.Description)
		}
	}

	var stepStatusDesc string
	for _, s := range exporter.GetSpans() {
		if s.Name == "checkout.step.pay" {
			stepStatusDesc = s.Status.Description
		}
	}
	if stepStatusDesc != context.Canceled.Error() {
		t.Errorf("step span status description = %q, want the bare sentinel %q", stepStatusDesc, context.Canceled.Error())
	}
}

// ============================================================
// Event constant tests
// ============================================================

func TestEventConstants(t *testing.T) {
	cases := []struct {
		got  string
		want string
	}{
		{checkout.EventStepStarted, "checkout.step.started"},
		{checkout.EventStepCompleted, "checkout.step.completed"},
		{checkout.EventCheckoutFailed, "checkout.failed"},
		{checkout.EventCheckoutCompleted, "checkout.completed"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("event = %q, want %q", tc.got, tc.want)
		}
	}
}
