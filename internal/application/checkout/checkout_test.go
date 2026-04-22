package checkout_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// --- Mock step ---

type mockStep struct {
	name string
	fn   func(ctx *checkout.Context) error
}

func (s *mockStep) Name() string                        { return s.name }
func (s *mockStep) Execute(ctx *checkout.Context) error { return s.fn(ctx) }

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

	result, err := svc.StartCheckout(context.Background(), c.ID, "cust-1", checkout.Input{})
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

	input := checkout.Input{
		Address: checkout.Address{
			FirstName: "Ada",
			LastName:  "Lovelace",
			Street:    "1 Logic Lane",
			City:      "Berlin",
			Postcode:  "10115",
			Country:   "DE",
		},
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

func TestService_StartCheckout_EmptyCustomerID(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	wf := checkout.NewWorkflow(nil, bus, log)
	svc := checkout.NewService(&mockCartRepo{}, wf, log)

	_, err := svc.StartCheckout(context.Background(), "cart-1", "", checkout.Input{})
	if err == nil {
		t.Fatal("expected error for empty customer id")
	}
}

func TestService_StartCheckout_CartNotFound(t *testing.T) {
	bus := testBus(t)
	log := testLogger()
	wf := checkout.NewWorkflow(nil, bus, log)
	repo := &mockCartRepo{cart: nil}
	svc := checkout.NewService(repo, wf, log)

	_, err := svc.StartCheckout(context.Background(), "nonexistent", "cust-1", checkout.Input{})
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

	_, err := svc.StartCheckout(context.Background(), c.ID, "cust-1", checkout.Input{})
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

	_, err := svc.StartCheckout(context.Background(), c.ID, "cust-OTHER", checkout.Input{})
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

	_, err = svc.StartCheckout(context.Background(), c.ID, "cust-1", checkout.Input{})
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

	result, err := svc.StartCheckout(context.Background(), c.ID, "cust-1", checkout.Input{})
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
