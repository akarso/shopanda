package checkout_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// blockingVariantRepo waits until ctx is cancelled, then returns ctx.Err().
type blockingVariantRepo struct {
	saw context.Context
}

func (r *blockingVariantRepo) FindByID(ctx context.Context, _ string) (*catalog.Variant, error) {
	r.saw = ctx
	<-ctx.Done()
	return nil, ctx.Err()
}
func (r *blockingVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (r *blockingVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}
func (r *blockingVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (r *blockingVariantRepo) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (r *blockingVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (r *blockingVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

type blockingReservationRepo struct {
	saw context.Context
}

func (r *blockingReservationRepo) Reserve(ctx context.Context, _ *inventory.Reservation) error {
	r.saw = ctx
	<-ctx.Done()
	return ctx.Err()
}
func (r *blockingReservationRepo) Release(context.Context, string) error { return nil }
func (r *blockingReservationRepo) Confirm(context.Context, string) error { return nil }
func (r *blockingReservationRepo) FindByID(context.Context, string) (*inventory.Reservation, error) {
	return nil, nil
}
func (r *blockingReservationRepo) ListActiveByVariantID(context.Context, string) ([]inventory.Reservation, error) {
	return nil, nil
}
func (r *blockingReservationRepo) ReleaseExpiredBefore(context.Context, time.Time) (int, error) {
	return 0, nil
}

func canceledCheckoutCart(t *testing.T) *cart.Cart {
	t.Helper()
	c, err := cart.NewCart(id.New(), "EUR")
	if err != nil {
		t.Fatal(err)
	}
	money, err := shared.NewMoney(100, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AddItem(id.New(), 1, money); err != nil {
		t.Fatal(err)
	}
	return &c
}

func TestValidateCartStep_SurfacesCanceledContext(t *testing.T) {
	repo := &blockingVariantRepo{}
	step := checkout.NewValidateCartStep(repo)
	c := canceledCheckoutCart(t)
	cctx := &checkout.Context{CartID: c.ID, Cart: c, Currency: "EUR"}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- step.Execute(ctx, cctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if repo.saw == nil || !errors.Is(repo.saw.Err(), context.Canceled) {
			t.Fatalf("repo context not canceled: saw=%v", repo.saw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled step")
	}
}

func TestReserveInventoryStep_SurfacesCanceledContext(t *testing.T) {
	repo := &blockingReservationRepo{}
	step := checkout.NewReserveInventoryStep(repo)
	c := canceledCheckoutCart(t)
	cctx := &checkout.Context{CartID: c.ID, Cart: c, Currency: "EUR"}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- step.Execute(ctx, cctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if repo.saw == nil || !errors.Is(repo.saw.Err(), context.Canceled) {
			t.Fatalf("repo context not canceled: saw=%v", repo.saw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled step")
	}
}

func TestWorkflow_ReturnsCanceledErrorDetectable(t *testing.T) {
	repo := &blockingVariantRepo{}
	step := checkout.NewValidateCartStep(repo)
	log := logger.NewWithWriter(&bytes.Buffer{}, "error")
	wf := checkout.NewWorkflow([]checkout.Step{step}, event.NewBus(log), log)

	c := canceledCheckoutCart(t)
	cctx := &checkout.Context{CartID: c.ID, Cart: c, Currency: "EUR"}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- wf.Execute(ctx, cctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("workflow error = %v, want errors.Is(..., context.Canceled)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled workflow")
	}
}

// cancelAfterFirstReserveRepo succeeds once, then cancels the parent and fails
// subsequent Reserve calls with the (canceled) request context error.
type cancelAfterFirstReserveRepo struct {
	cancel      context.CancelFunc
	reserved    int
	releaseCtxs []context.Context
}

func (r *cancelAfterFirstReserveRepo) Reserve(ctx context.Context, _ *inventory.Reservation) error {
	r.reserved++
	if r.reserved == 1 {
		r.cancel()
		return nil
	}
	return ctx.Err()
}
func (r *cancelAfterFirstReserveRepo) Release(ctx context.Context, _ string) error {
	r.releaseCtxs = append(r.releaseCtxs, ctx)
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("release saw done context: %w", err)
	}
	return nil
}
func (r *cancelAfterFirstReserveRepo) Confirm(context.Context, string) error { return nil }
func (r *cancelAfterFirstReserveRepo) FindByID(context.Context, string) (*inventory.Reservation, error) {
	return nil, nil
}
func (r *cancelAfterFirstReserveRepo) ListActiveByVariantID(context.Context, string) ([]inventory.Reservation, error) {
	return nil, nil
}
func (r *cancelAfterFirstReserveRepo) ReleaseExpiredBefore(context.Context, time.Time) (int, error) {
	return 0, nil
}

func TestReserveInventoryStep_RollbackIgnoresCanceledRequestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo := &cancelAfterFirstReserveRepo{cancel: cancel}
	step := checkout.NewReserveInventoryStep(repo)

	c, err := cart.NewCart(id.New(), "EUR")
	if err != nil {
		t.Fatal(err)
	}
	money, err := shared.NewMoney(100, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AddItem(id.New(), 1, money); err != nil {
		t.Fatal(err)
	}
	if err := c.AddItem(id.New(), 1, money); err != nil {
		t.Fatal(err)
	}
	cctx := &checkout.Context{CartID: c.ID, Cart: &c, Currency: "EUR"}

	err = step.Execute(ctx, cctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context.Canceled", err)
	}
	if len(repo.releaseCtxs) != 1 {
		t.Fatalf("Release calls = %d, want 1", len(repo.releaseCtxs))
	}
	// Release must have succeeded with a live context (would return error if ctx was done mid-call).
}

type cancelAfterInitiateProvider struct {
	cancel context.CancelFunc
	method payment.PaymentMethod
}

func (p *cancelAfterInitiateProvider) Method() payment.PaymentMethod { return p.method }
func (p *cancelAfterInitiateProvider) Initiate(_ context.Context, _ *payment.Payment) (payment.ProviderResult, error) {
	p.cancel()
	return payment.ProviderResult{ProviderRef: "manual:ok", Success: true}, nil
}

type trackingPaymentRepo struct {
	updateSawDone bool
	updated       bool
}

func (r *trackingPaymentRepo) FindByID(context.Context, string) (*payment.Payment, error) {
	return nil, nil
}
func (r *trackingPaymentRepo) FindByOrderID(context.Context, string) (*payment.Payment, error) {
	return nil, nil
}
func (r *trackingPaymentRepo) Create(context.Context, *payment.Payment) error { return nil }
func (r *trackingPaymentRepo) UpdateStatus(ctx context.Context, _ *payment.Payment, _ time.Time) error {
	r.updated = true
	if ctx.Err() != nil {
		r.updateSawDone = true
		return ctx.Err()
	}
	return nil
}
func (r *trackingPaymentRepo) List(context.Context, payment.ListFilter) ([]payment.Payment, error) {
	return nil, nil
}

func TestInitiatePaymentStep_PersistIgnoresCanceledRequestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &cancelAfterInitiateProvider{cancel: cancel, method: payment.MethodManual}
	repo := &trackingPaymentRepo{}
	reg := payment.NewProviderRegistry()
	reg.Register(provider)
	step := checkout.NewInitiatePaymentStep(reg, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	money, err := shared.NewMoney(1000, "EUR")
	if err != nil {
		t.Fatal(err)
	}
	item, err := order.NewItem(id.New(), "SKU", "Item", 1, money)
	if err != nil {
		t.Fatal(err)
	}
	o, err := order.NewOrder(id.New(), "cust-1", "a@b.c", "EUR", []order.Item{item})
	if err != nil {
		t.Fatal(err)
	}
	cctx.Order = &o
	cctx.Input.PaymentMethod = string(payment.MethodManual)

	if err := step.Execute(ctx, cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !repo.updated {
		t.Fatal("expected UpdateStatus after provider success")
	}
	if repo.updateSawDone {
		t.Fatal("UpdateStatus saw a done context; persist must detach from request cancel")
	}
}
