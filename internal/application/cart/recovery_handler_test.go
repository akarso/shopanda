package cart_test

import (
	"context"
	"testing"
	"time"

	cartApp "github.com/akarso/shopanda/internal/application/cart"
	"github.com/akarso/shopanda/internal/application/notification"
	domainCart "github.com/akarso/shopanda/internal/domain/cart"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainCfg "github.com/akarso/shopanda/internal/domain/config"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/shared"
)

type recoveryCartRepo struct {
	domainCart.CartRepository
	candidates    []domainCart.Cart
	marked        []string
	markClaimable bool
}

func (s *recoveryCartRepo) FindRecoveryCandidates(_ context.Context, _ time.Time, _ int) ([]*domainCart.Cart, error) {
	out := make([]*domainCart.Cart, len(s.candidates))
	for i := range s.candidates {
		c := s.candidates[i]
		out[i] = &c
	}
	return out, nil
}

func (s *recoveryCartRepo) MarkRecoveryEmailSent(_ context.Context, cartID string, _ time.Time) (bool, error) {
	if !s.markClaimable {
		return false, nil
	}
	s.marked = append(s.marked, cartID)
	return true, nil
}

type recoveryCustomerRepo struct {
	customer.CustomerRepository
	byID map[string]*customer.Customer
}

func (s *recoveryCustomerRepo) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	return s.byID[id], nil
}

type recoveryVariantRepo struct {
	catalog.VariantRepository
	byID map[string]*catalog.Variant
}

func (s *recoveryVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return s.byID[id], nil
}

type recoveryProductRepo struct {
	catalog.ProductRepository
	byID map[string]*catalog.Product
}

func (s *recoveryProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	return s.byID[id], nil
}

type recoveryQueue struct {
	jobs.Queue
	enqueued []jobs.Job
}

func (s *recoveryQueue) Enqueue(_ context.Context, job jobs.Job) error {
	s.enqueued = append(s.enqueued, job)
	return nil
}

type recoveryLogger struct {
	infos []string
}

func (l *recoveryLogger) Info(msg string, _ map[string]interface{}) {
	l.infos = append(l.infos, msg)
}

func (l *recoveryLogger) Warn(_ string, _ map[string]interface{}) {}

func (l *recoveryLogger) Error(_ string, _ error, _ map[string]interface{}) {}

func testRecoveryHandler(t *testing.T, carts *recoveryCartRepo, customers *recoveryCustomerRepo) (*cartApp.RecoveryHandler, *recoveryQueue) {
	return testRecoveryHandlerWithSettings(t, carts, customers, nil)
}

func testRecoveryHandlerWithSettings(t *testing.T, carts *recoveryCartRepo, customers *recoveryCustomerRepo, settings domainCfg.Repository) (*cartApp.RecoveryHandler, *recoveryQueue) {
	t.Helper()
	templates := mail.NewTemplates()
	notification.RegisterTemplates(templates)
	queue := &recoveryQueue{}
	h := cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Carts:     carts,
		Customers: customers,
		Variants: &recoveryVariantRepo{byID: map[string]*catalog.Variant{
			"v1": {ID: "v1", ProductID: "p1", SKU: "SKU-1", Name: "Blue Tee"},
		}},
		Products:  &recoveryProductRepo{byID: map[string]*catalog.Product{}},
		Templates: templates,
		Queue:     queue,
		StoreURL:  "https://shop.example",
		Settings:  settings,
		Log:       &recoveryLogger{},
	})
	return h, queue
}

func TestRecoveryHandler_Type(t *testing.T) {
	h, _ := testRecoveryHandler(t, &recoveryCartRepo{}, &recoveryCustomerRepo{byID: map[string]*customer.Customer{}})
	if h.Type() != cartApp.RecoveryJobType {
		t.Errorf("Type() = %q, want %q", h.Type(), cartApp.RecoveryJobType)
	}
}

func TestRecoveryHandler_Handle_NoCandidates(t *testing.T) {
	h, queue := testRecoveryHandler(t, &recoveryCartRepo{}, &recoveryCustomerRepo{byID: map[string]*customer.Customer{}})
	err := h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("expected no enqueued jobs, got %d", len(queue.enqueued))
	}
}

func TestRecoveryHandler_Handle_SendsRecoveryEmail(t *testing.T) {
	c, err := domainCart.NewCart("cart-1", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 2, shared.MustNewMoney(1999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {
			ID:        "cust-1",
			Email:     "buyer@example.com",
			FirstName: "Alex",
			Status:    customer.StatusActive,
		},
	}}
	h, queue := testRecoveryHandler(t, carts, customers)

	err = h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued jobs = %d, want 1", len(queue.enqueued))
	}
	job := queue.enqueued[0]
	if job.Type != notification.JobTypeEmailSend {
		t.Errorf("job type = %q, want %q", job.Type, notification.JobTypeEmailSend)
	}
	if got := job.Payload["to"]; got != "buyer@example.com" {
		t.Errorf("payload to = %v, want buyer@example.com", got)
	}
	if len(carts.marked) != 1 || carts.marked[0] != "cart-1" {
		t.Errorf("marked carts = %v, want [cart-1]", carts.marked)
	}
}

func TestRecoveryHandler_Handle_SkipsInactiveCustomer(t *testing.T) {
	c, err := domainCart.NewCart("cart-2", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-2"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-2": {ID: "cust-2", Email: "gone@example.com", Status: customer.StatusDisabled},
	}}
	h, queue := testRecoveryHandler(t, carts, customers)

	err = h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("expected no email jobs, got %d", len(queue.enqueued))
	}
	if len(carts.marked) != 1 || carts.marked[0] != "cart-2" {
		t.Fatalf("expected terminal skip mark for cart-2, got %v", carts.marked)
	}
}

func TestRecoveryHandler_Handle_SkipsAlreadyClaimedCart(t *testing.T) {
	c, err := domainCart.NewCart("cart-3", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-3"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: false}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-3": {
			ID:        "cust-3",
			Email:     "buyer@example.com",
			FirstName: "Alex",
			Status:    customer.StatusActive,
		},
	}}
	h, queue := testRecoveryHandler(t, carts, customers)

	err = h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("expected no email jobs when claim fails, got %d", len(queue.enqueued))
	}
	if len(carts.marked) != 0 {
		t.Fatalf("expected no mark when claim fails, got %v", carts.marked)
	}
}

func TestRecoveryHandler_Handle_SkipsWhenDisabled(t *testing.T) {
	c, err := domainCart.NewCart("cart-off", "EUR")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := c.SetCustomerID("cust-1"); err != nil {
		t.Fatalf("SetCustomerID: %v", err)
	}
	if err := c.AddItem("v1", 1, shared.MustNewMoney(999, "EUR")); err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	carts := &recoveryCartRepo{candidates: []domainCart.Cart{c}, markClaimable: true}
	customers := &recoveryCustomerRepo{byID: map[string]*customer.Customer{
		"cust-1": {ID: "cust-1", Email: "buyer@example.com", FirstName: "Alex", Status: customer.StatusActive},
	}}
	settings := &stubRecoveryConfigRepo{values: map[string]interface{}{
		cartApp.ConfigKeyCartRecoveryEnabled: false,
	}}
	h, queue := testRecoveryHandlerWithSettings(t, carts, customers, settings)

	err = h.Handle(context.Background(), jobs.Job{Type: cartApp.RecoveryJobType})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("expected no enqueued jobs when disabled, got %d", len(queue.enqueued))
	}
	if len(carts.marked) != 0 {
		t.Fatalf("expected no cart marks when disabled, got %v", carts.marked)
	}
}

func TestRecoveryHandler_PanicsOnNilCarts(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil carts")
		}
	}()
	cartApp.NewRecoveryHandler(cartApp.RecoveryHandlerConfig{
		Customers: &recoveryCustomerRepo{},
		Variants:  &recoveryVariantRepo{},
		Products:  &recoveryProductRepo{},
		Templates: mail.NewTemplates(),
		Queue:     &recoveryQueue{},
		Log:       &recoveryLogger{},
	})
}
