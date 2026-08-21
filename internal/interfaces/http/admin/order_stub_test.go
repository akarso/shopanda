package admin_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// fixedTime duplicates the storefront page_test.go fixture — unexported, so
// it can't be shared across the http_test/admin_test package boundary
// created by the admin package split.
var fixedTime = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// stubOrderRepo is a duplicate of the storefront order_test.go stub — it's
// unexported, so it can't be shared across the http_test/admin_test package
// boundary created by the admin package split.
type stubOrderRepo struct {
	orders map[string]*order.Order
}

func newStubOrderRepo() *stubOrderRepo {
	return &stubOrderRepo{orders: make(map[string]*order.Order)}
}

func (r *stubOrderRepo) FindByID(_ context.Context, id string) (*order.Order, error) {
	o, ok := r.orders[id]
	if !ok {
		return nil, nil
	}
	return o, nil
}

func (r *stubOrderRepo) FindByCustomerID(_ context.Context, customerID string) ([]order.Order, error) {
	var out []order.Order
	for _, o := range r.orders {
		if o.CustomerID == customerID {
			out = append(out, *o)
		}
	}
	return out, nil
}

func (r *stubOrderRepo) FindByContactEmail(_ context.Context, contactEmail string) ([]order.Order, error) {
	var out []order.Order
	contactEmailNorm := strings.ToLower(strings.TrimSpace(contactEmail))
	for _, o := range r.orders {
		if strings.ToLower(strings.TrimSpace(o.ContactEmail)) == contactEmailNorm {
			out = append(out, *o)
		}
	}
	return out, nil
}

func (r *stubOrderRepo) List(_ context.Context, offset, limit int) ([]order.Order, error) {
	var all []order.Order
	for _, o := range r.orders {
		all = append(all, *o)
	}
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (r *stubOrderRepo) Save(_ context.Context, o *order.Order) error {
	r.orders[o.ID] = o
	return nil
}

func (r *stubOrderRepo) UpdateStatus(_ context.Context, _ *order.Order) error   { return nil }
func (r *stubOrderRepo) LinkToCustomer(_ context.Context, _ *order.Order) error { return nil }
func (r *stubOrderRepo) LinkToCustomerByContactEmail(_ context.Context, _, _ string, _ time.Time) (int64, error) {
	return 0, nil
}
func (r *stubOrderRepo) ListPaidTaxSnapshots(context.Context, time.Time, time.Time) ([]order.TaxSnapshotRow, error) {
	return nil, nil
}

func seedOrder(t *testing.T, repo *stubOrderRepo, id, customerID string) *order.Order {
	t.Helper()
	items := []order.Item{
		{
			VariantID: "var-1",
			SKU:       "SKU-001",
			Name:      "Widget",
			Quantity:  2,
			UnitPrice: shared.MustNewMoney(1500, "EUR"),
			CreatedAt: time.Now().UTC(),
		},
	}
	o, err := order.NewOrder(id, customerID, "", "EUR", items)
	if err != nil {
		t.Fatalf("seedOrder: %v", err)
	}
	repo.orders[id] = &o
	return &o
}
