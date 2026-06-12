package order_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/order"
	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/id"
)

type mockOrderRepository struct {
	orders  map[string]*domainOrder.Order
	linkErr error
}

func newMockOrderRepository() *mockOrderRepository {
	return &mockOrderRepository{
		orders: make(map[string]*domainOrder.Order),
	}
}

func (r *mockOrderRepository) FindByID(ctx context.Context, id string) (*domainOrder.Order, error) {
	o, ok := r.orders[id]
	if !ok {
		return nil, nil
	}
	clone := *o
	return &clone, nil
}

func (r *mockOrderRepository) FindByCustomerID(ctx context.Context, customerID string) ([]domainOrder.Order, error) {
	var orders []domainOrder.Order
	for _, o := range r.orders {
		if o.CustomerID == customerID {
			orders = append(orders, *o)
		}
	}
	return orders, nil
}

func (r *mockOrderRepository) FindByContactEmail(ctx context.Context, contactEmail string) ([]domainOrder.Order, error) {
	contactEmailNorm := strings.ToLower(strings.TrimSpace(contactEmail))
	var orders []domainOrder.Order
	for _, o := range r.orders {
		if o.CustomerID == "" && strings.ToLower(strings.TrimSpace(o.ContactEmail)) == contactEmailNorm {
			orders = append(orders, *o)
		}
	}
	return orders, nil
}

func (r *mockOrderRepository) List(ctx context.Context, offset, limit int) ([]domainOrder.Order, error) {
	return []domainOrder.Order{}, nil
}

func (r *mockOrderRepository) Save(ctx context.Context, o *domainOrder.Order) error {
	clone := *o
	r.orders[o.ID] = &clone
	return nil
}

func (r *mockOrderRepository) UpdateStatus(ctx context.Context, o *domainOrder.Order) error {
	return nil
}

func (r *mockOrderRepository) LinkToCustomer(ctx context.Context, o *domainOrder.Order) error {
	if r.linkErr != nil {
		return r.linkErr
	}
	stored, ok := r.orders[o.ID]
	if !ok {
		return errors.New("mock: link to customer: order not found")
	}
	if stored.CustomerID != "" {
		return errors.New("mock: link to customer: already linked")
	}
	clone := *o
	r.orders[o.ID] = &clone
	return nil
}

func mustNewTestGuestOrder(t *testing.T, contactEmail string) domainOrder.Order {
	t.Helper()
	price := shared.MustNewMoney(1000, "EUR")
	item, err := domainOrder.NewItem("var-1", "SKU-001", "Test Product", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := domainOrder.NewOrder(id.New(), "", contactEmail, "EUR", []domainOrder.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return o
}

func TestClaimService_SearchGuestOrders(t *testing.T) {
	repo := newMockOrderRepository()
	svc := order.NewClaimService(repo)

	contactEmail := "guest@example.com"

	// Save guest orders
	o1 := mustNewTestGuestOrder(t, contactEmail)
	if err := repo.Save(context.Background(), &o1); err != nil {
		t.Fatalf("Save o1: %v", err)
	}

	o2 := mustNewTestGuestOrder(t, contactEmail)
	if err := repo.Save(context.Background(), &o2); err != nil {
		t.Fatalf("Save o2: %v", err)
	}

	// Search for orders
	orders, err := svc.SearchGuestOrders(context.Background(), contactEmail)
	if err != nil {
		t.Fatalf("SearchGuestOrders: %v", err)
	}

	if len(orders) != 2 {
		t.Errorf("len(orders) = %d, want 2", len(orders))
	}

	for _, o := range orders {
		if o.ContactEmail != contactEmail {
			t.Errorf("ContactEmail mismatch: got %q, want %q", o.ContactEmail, contactEmail)
		}
	}
}

func TestClaimService_VerifyOrderBelongsToEmail(t *testing.T) {
	repo := newMockOrderRepository()
	svc := order.NewClaimService(repo)

	contactEmail := "guest@example.com"
	o := mustNewTestGuestOrder(t, contactEmail)

	if err := repo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify order belongs to email
	found, err := svc.VerifyOrderBelongsToEmail(context.Background(), o.ID, contactEmail)
	if err != nil {
		t.Fatalf("VerifyOrderBelongsToEmail: %v", err)
	}

	if found == nil {
		t.Fatalf("expected order, got nil")
	}

	if found.ID != o.ID {
		t.Errorf("ID mismatch: got %q, want %q", found.ID, o.ID)
	}
}

func TestClaimService_VerifyOrderBelongsToEmail_WrongEmail(t *testing.T) {
	repo := newMockOrderRepository()
	svc := order.NewClaimService(repo)

	contactEmail := "guest@example.com"
	o := mustNewTestGuestOrder(t, contactEmail)

	if err := repo.Save(context.Background(), &o); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Try to verify with wrong email
	_, err := svc.VerifyOrderBelongsToEmail(context.Background(), o.ID, "wrong@example.com")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestClaimService_VerifyOrderBelongsToEmail_NotFound(t *testing.T) {
	repo := newMockOrderRepository()
	svc := order.NewClaimService(repo)

	// Try to verify nonexistent order
	_, err := svc.VerifyOrderBelongsToEmail(context.Background(), "nonexistent", "guest@example.com")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}
