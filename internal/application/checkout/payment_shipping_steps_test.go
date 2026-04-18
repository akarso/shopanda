package checkout_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/id"
)

// ============================================================
// Mock shipping provider
// ============================================================

type mockShippingProvider047 struct {
	method shipping.ShippingMethod
	rate   shipping.ShippingRate
	err    error
}

func (p *mockShippingProvider047) Method() shipping.ShippingMethod { return p.method }

func (p *mockShippingProvider047) CalculateRate(_ context.Context, _ string, _ string, _ int) (shipping.ShippingRate, error) {
	return p.rate, p.err
}

// ============================================================
// Mock shipment repository
// ============================================================

type mockShipmentRepo047 struct {
	created *shipping.Shipment
	err     error
}

func (r *mockShipmentRepo047) FindByID(_ context.Context, _ string) (*shipping.Shipment, error) {
	return nil, nil
}
func (r *mockShipmentRepo047) FindByOrderID(_ context.Context, _ string) (*shipping.Shipment, error) {
	return nil, nil
}
func (r *mockShipmentRepo047) Create(_ context.Context, s *shipping.Shipment) error {
	if r.err != nil {
		return r.err
	}
	r.created = s
	return nil
}
func (r *mockShipmentRepo047) UpdateStatus(_ context.Context, _ *shipping.Shipment, _ time.Time) error {
	return nil
}

// ============================================================
// Mock payment provider
// ============================================================

type mockPaymentProvider047 struct {
	method payment.PaymentMethod
	result payment.ProviderResult
	err    error
}

func (p *mockPaymentProvider047) Method() payment.PaymentMethod { return p.method }

func (p *mockPaymentProvider047) Initiate(_ context.Context, _ *payment.Payment) (payment.ProviderResult, error) {
	return p.result, p.err
}

// ============================================================
// Mock payment repository
// ============================================================

type mockPaymentRepo047 struct {
	created      *payment.Payment
	createErr    error
	updateErr    error
	updateCalled bool
}

func (r *mockPaymentRepo047) FindByID(_ context.Context, _ string) (*payment.Payment, error) {
	return nil, nil
}
func (r *mockPaymentRepo047) FindByOrderID(_ context.Context, _ string) (*payment.Payment, error) {
	return nil, nil
}
func (r *mockPaymentRepo047) Create(_ context.Context, p *payment.Payment) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.created = p
	return nil
}
func (r *mockPaymentRepo047) UpdateStatus(_ context.Context, _ *payment.Payment, _ time.Time) error {
	r.updateCalled = true
	return r.updateErr
}

// ============================================================
// Helpers
// ============================================================

func orderForCheckout047(t *testing.T) *order.Order {
	t.Helper()
	item, err := order.NewItem("v1", "SKU-v1", "Variant v1", 2, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder(id.New(), "cust-1", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return &o
}

// ============================================================
// SelectShippingStep tests
// ============================================================

func TestSelectShippingStep_Name(t *testing.T) {
	step := checkout.NewSelectShippingStep(
		&mockShippingProvider047{method: shipping.MethodFlatRate},
		&mockShipmentRepo047{},
	)
	if step.Name() != "select_shipping" {
		t.Errorf("Name() = %q, want select_shipping", step.Name())
	}
}

func TestSelectShippingStep_Success(t *testing.T) {
	cost := shared.MustNewMoney(500, "EUR")
	provider := &mockShippingProvider047{
		method: shipping.MethodFlatRate,
		rate: shipping.ShippingRate{
			ProviderRef: "flat_rate:order-1",
			Cost:        cost,
			Label:       "Flat Rate Shipping",
		},
	}
	repo := &mockShipmentRepo047{}
	step := checkout.NewSelectShippingStep(provider, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Order = orderForCheckout047(t)

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.created == nil {
		t.Fatal("expected shipment to be saved")
	}
	if repo.created.OrderID != cctx.Order.ID {
		t.Errorf("OrderID = %q, want %q", repo.created.OrderID, cctx.Order.ID)
	}
	if repo.created.Method != shipping.MethodFlatRate {
		t.Errorf("Method = %q, want flat_rate", repo.created.Method)
	}
	if repo.created.Cost.Amount() != 500 {
		t.Errorf("Cost = %d, want 500", repo.created.Cost.Amount())
	}

	raw, ok := cctx.GetMeta("shipment")
	if !ok {
		t.Fatal("expected shipment in meta")
	}
	if _, ok := raw.(*shipping.Shipment); !ok {
		t.Fatalf("shipment meta is %T, want *shipping.Shipment", raw)
	}
	if v, ok := cctx.GetMeta("shipment_selected"); !ok || v != true {
		t.Error("expected shipment_selected=true in meta")
	}
}

func TestSelectShippingStep_CalculateRateError(t *testing.T) {
	provider := &mockShippingProvider047{
		method: shipping.MethodFlatRate,
		err:    errors.New("unsupported currency"),
	}
	step := checkout.NewSelectShippingStep(provider, &mockShipmentRepo047{})

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Order = orderForCheckout047(t)

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error from calculate rate")
	}
	if _, ok := cctx.GetMeta("shipment_selected"); ok {
		t.Error("shipment_selected should not be set on error")
	}
}

func TestSelectShippingStep_SaveError(t *testing.T) {
	cost := shared.MustNewMoney(500, "EUR")
	provider := &mockShippingProvider047{
		method: shipping.MethodFlatRate,
		rate:   shipping.ShippingRate{Cost: cost},
	}
	repo := &mockShipmentRepo047{err: errors.New("db down")}
	step := checkout.NewSelectShippingStep(provider, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Order = orderForCheckout047(t)

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error from save")
	}
	if _, ok := cctx.GetMeta("shipment_selected"); ok {
		t.Error("shipment_selected should not be set on error")
	}
}

func TestSelectShippingStep_NoOrder(t *testing.T) {
	provider := &mockShippingProvider047{method: shipping.MethodFlatRate}
	step := checkout.NewSelectShippingStep(provider, &mockShipmentRepo047{})

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error for missing order")
	}
}

func TestSelectShippingStep_NilContext(t *testing.T) {
	provider := &mockShippingProvider047{method: shipping.MethodFlatRate}
	step := checkout.NewSelectShippingStep(provider, &mockShipmentRepo047{})

	err := step.Execute(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestSelectShippingStep_Idempotent(t *testing.T) {
	cost := shared.MustNewMoney(500, "EUR")
	provider := &mockShippingProvider047{
		method: shipping.MethodFlatRate,
		rate:   shipping.ShippingRate{Cost: cost},
	}
	repo := &mockShipmentRepo047{}
	step := checkout.NewSelectShippingStep(provider, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Cart = cartWithItems037(t, "cust-1", "v1")
	cctx.Order = orderForCheckout047(t)

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	first := repo.created

	repo.err = errors.New("should not be called")
	if err := step.Execute(cctx); err != nil {
		t.Fatalf("second Execute should be idempotent: %v", err)
	}
	if repo.created != first {
		t.Error("repo Create called on second execution")
	}
}

// ============================================================
// InitiatePaymentStep tests
// ============================================================

func TestInitiatePaymentStep_Name(t *testing.T) {
	step := checkout.NewInitiatePaymentStep(
		&mockPaymentProvider047{method: payment.MethodManual},
		&mockPaymentRepo047{},
	)
	if step.Name() != "initiate_payment" {
		t.Errorf("Name() = %q, want initiate_payment", step.Name())
	}
}

func TestInitiatePaymentStep_Success(t *testing.T) {
	provider := &mockPaymentProvider047{
		method: payment.MethodManual,
		result: payment.ProviderResult{
			ProviderRef: "manual:pay-1",
			Success:     true,
		},
	}
	repo := &mockPaymentRepo047{}
	step := checkout.NewInitiatePaymentStep(provider, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = orderForCheckout047(t)

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.created == nil {
		t.Fatal("expected payment to be saved")
	}
	if repo.created.OrderID != cctx.Order.ID {
		t.Errorf("OrderID = %q, want %q", repo.created.OrderID, cctx.Order.ID)
	}
	if repo.created.Method != payment.MethodManual {
		t.Errorf("Method = %q, want manual", repo.created.Method)
	}
	if !repo.updateCalled {
		t.Error("expected UpdateStatus to be called")
	}

	raw, ok := cctx.GetMeta("payment")
	if !ok {
		t.Fatal("expected payment in meta")
	}
	py := raw.(*payment.Payment)
	if py.Status() != payment.StatusCompleted {
		t.Errorf("payment status = %v, want completed", py.Status())
	}
	if py.ProviderRef != "manual:pay-1" {
		t.Errorf("ProviderRef = %q, want manual:pay-1", py.ProviderRef)
	}
	if v, ok := cctx.GetMeta("payment_initiated"); !ok || v != true {
		t.Error("expected payment_initiated=true in meta")
	}
}

func TestInitiatePaymentStep_Declined(t *testing.T) {
	provider := &mockPaymentProvider047{
		method: payment.MethodManual,
		result: payment.ProviderResult{
			Success: false,
		},
	}
	repo := &mockPaymentRepo047{}
	step := checkout.NewInitiatePaymentStep(provider, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = orderForCheckout047(t)

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error for declined payment")
	}

	if !repo.updateCalled {
		t.Error("expected UpdateStatus to be called even on decline")
	}

	raw, ok := cctx.GetMeta("payment")
	if !ok {
		t.Fatal("expected payment in meta even on decline")
	}
	py := raw.(*payment.Payment)
	if py.Status() != payment.StatusFailed {
		t.Errorf("payment status = %v, want failed", py.Status())
	}
}

func TestInitiatePaymentStep_ProviderError(t *testing.T) {
	provider := &mockPaymentProvider047{
		method: payment.MethodManual,
		err:    errors.New("provider unreachable"),
	}
	repo := &mockPaymentRepo047{}
	step := checkout.NewInitiatePaymentStep(provider, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = orderForCheckout047(t)

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error from provider")
	}
	// Payment was created but not updated
	if repo.created == nil {
		t.Error("expected payment to be created before provider call")
	}
	if repo.updateCalled {
		t.Error("UpdateStatus should not be called on provider error")
	}
}

func TestInitiatePaymentStep_CreateError(t *testing.T) {
	provider := &mockPaymentProvider047{
		method: payment.MethodManual,
		result: payment.ProviderResult{Success: true},
	}
	repo := &mockPaymentRepo047{createErr: errors.New("db down")}
	step := checkout.NewInitiatePaymentStep(provider, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = orderForCheckout047(t)

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error from create")
	}
	if _, ok := cctx.GetMeta("payment_initiated"); ok {
		t.Error("payment_initiated should not be set on error")
	}
}

func TestInitiatePaymentStep_UpdateStatusError(t *testing.T) {
	provider := &mockPaymentProvider047{
		method: payment.MethodManual,
		result: payment.ProviderResult{ProviderRef: "ref", Success: true},
	}
	repo := &mockPaymentRepo047{updateErr: errors.New("conflict")}
	step := checkout.NewInitiatePaymentStep(provider, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = orderForCheckout047(t)

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error from update status")
	}
	if _, ok := cctx.GetMeta("payment_initiated"); ok {
		t.Error("payment_initiated should not be set on error")
	}
}

func TestInitiatePaymentStep_NoOrder(t *testing.T) {
	provider := &mockPaymentProvider047{method: payment.MethodManual}
	step := checkout.NewInitiatePaymentStep(provider, &mockPaymentRepo047{})

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error for missing order")
	}
}

func TestInitiatePaymentStep_NilContext(t *testing.T) {
	provider := &mockPaymentProvider047{method: payment.MethodManual}
	step := checkout.NewInitiatePaymentStep(provider, &mockPaymentRepo047{})

	err := step.Execute(nil)
	if err == nil {
		t.Fatal("expected error for nil context")
	}
}

func TestInitiatePaymentStep_Idempotent(t *testing.T) {
	provider := &mockPaymentProvider047{
		method: payment.MethodManual,
		result: payment.ProviderResult{ProviderRef: "ref", Success: true},
	}
	repo := &mockPaymentRepo047{}
	step := checkout.NewInitiatePaymentStep(provider, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = orderForCheckout047(t)

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	first := repo.created

	repo.createErr = errors.New("should not be called")
	if err := step.Execute(cctx); err != nil {
		t.Fatalf("second Execute should be idempotent: %v", err)
	}
	if repo.created != first {
		t.Error("repo Create called on second execution")
	}
}

func TestInitiatePaymentStep_Pending(t *testing.T) {
	provider := &mockPaymentProvider047{
		method: payment.MethodStripe,
		result: payment.ProviderResult{
			ProviderRef:  "pi_test_abc123",
			Pending:      true,
			ClientSecret: "pi_test_abc123_secret_xyz",
		},
	}
	repo := &mockPaymentRepo047{}
	step := checkout.NewInitiatePaymentStep(provider, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = orderForCheckout047(t)

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.created == nil {
		t.Fatal("expected payment to be saved")
	}
	if repo.created.Method != payment.MethodStripe {
		t.Errorf("Method = %q, want stripe", repo.created.Method)
	}
	if !repo.updateCalled {
		t.Error("expected UpdateStatus to be called")
	}

	raw, ok := cctx.GetMeta("payment")
	if !ok {
		t.Fatal("expected payment in meta")
	}
	py := raw.(*payment.Payment)
	if py.Status() != payment.StatusPending {
		t.Errorf("payment status = %v, want pending", py.Status())
	}
	if py.ProviderRef != "pi_test_abc123" {
		t.Errorf("ProviderRef = %q, want pi_test_abc123", py.ProviderRef)
	}

	if v, ok := cctx.GetMeta("payment_initiated"); !ok || v != true {
		t.Error("expected payment_initiated=true in meta")
	}
	cs, ok := cctx.GetMeta("client_secret")
	if !ok {
		t.Fatal("expected client_secret in meta")
	}
	if cs != "pi_test_abc123_secret_xyz" {
		t.Errorf("client_secret = %q, want pi_test_abc123_secret_xyz", cs)
	}
}

func TestInitiatePaymentStep_Pending_NoClientSecret(t *testing.T) {
	provider := &mockPaymentProvider047{
		method: payment.MethodStripe,
		result: payment.ProviderResult{
			ProviderRef: "pi_test_no_secret",
			Pending:     true,
		},
	}
	repo := &mockPaymentRepo047{}
	step := checkout.NewInitiatePaymentStep(provider, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = orderForCheckout047(t)

	if err := step.Execute(cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, ok := cctx.GetMeta("client_secret"); ok {
		t.Error("expected no client_secret in meta when ClientSecret is empty")
	}
	if v, ok := cctx.GetMeta("payment_initiated"); !ok || v != true {
		t.Error("expected payment_initiated=true in meta")
	}
}

func TestInitiatePaymentStep_Pending_EmptyProviderRef(t *testing.T) {
	provider := &mockPaymentProvider047{
		method: payment.MethodStripe,
		result: payment.ProviderResult{
			Pending:      true,
			ClientSecret: "pi_secret",
		},
	}
	repo := &mockPaymentRepo047{}
	step := checkout.NewInitiatePaymentStep(provider, repo)

	cctx := checkout.NewContext("cart-1", "cust-1", "EUR")
	cctx.Order = orderForCheckout047(t)

	err := step.Execute(cctx)
	if err == nil {
		t.Fatal("expected error for pending result with empty ProviderRef")
	}
}
