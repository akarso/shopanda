package pluginsdk_test

import (
	"context"
	"testing"

	checkoutApp "github.com/akarso/shopanda/internal/application/checkout"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

type capturingCheckoutStep struct {
	got extapi.CheckoutContext
}

func (s *capturingCheckoutStep) Name() string { return "capture" }

func (s *capturingCheckoutStep) Execute(_ context.Context, cctx extapi.CheckoutContext) error {
	s.got = cctx
	return nil
}

func TestCheckoutContextView_AddressContactShippingPayment(t *testing.T) {
	cctx := checkoutApp.NewContext("cart-1", "cust-1", "EUR")
	cctx.Input = checkoutApp.Input{
		Address: checkoutApp.Address{
			FirstName: "Ada",
			LastName:  "Lovelace",
			Street:    "1 Analytical Engine Way",
			City:      "London",
			Postcode:  "SW1A",
			Country:   "GB",
		},
		ContactEmail:   "ada@example.com",
		ShippingMethod: "flat_rate",
		PaymentMethod:  "manual",
	}

	step := &capturingCheckoutStep{}
	domainStep := pluginsdk.AdaptCheckoutStep(step)
	if err := domainStep.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// PII is not exposed on the public checkout step contract: every step
	// registered on the workflow would otherwise see the full address/email
	// on every invocation, regardless of whether it needs PII at all.
	addr := step.got.Address()
	if addr != (extapi.Address{}) {
		t.Fatalf("Address() = %+v, want zero value (PII not exposed)", addr)
	}
	if got := step.got.ContactEmail(); got != "" {
		t.Errorf("ContactEmail() = %q, want empty (PII not exposed)", got)
	}
	if got := step.got.ShippingMethod(); got != "flat_rate" {
		t.Errorf("ShippingMethod() = %q", got)
	}
	if got := step.got.PaymentMethod(); got != "manual" {
		t.Errorf("PaymentMethod() = %q", got)
	}
	if got := step.got.OrderID(); got != "" {
		t.Errorf("OrderID() = %q, want empty before create_order", got)
	}
}

func TestCheckoutContextView_OrderIDAfterCreateOrder(t *testing.T) {
	cctx := checkoutApp.NewContext("cart-1", "cust-1", "EUR")
	item, err := order.NewItem("v1", "SKU-1", "Widget", 1, shared.MustNewMoney(500, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := order.NewOrder("order-123", "cust-1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	cctx.Order = &o

	step := &capturingCheckoutStep{}
	domainStep := pluginsdk.AdaptCheckoutStep(step)
	if err := domainStep.Execute(context.Background(), cctx); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := step.got.OrderID(); got != "order-123" {
		t.Errorf("OrderID() = %q, want order-123", got)
	}
}
