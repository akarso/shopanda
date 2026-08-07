package order

import (
	"context"
	"errors"
	"testing"
	"time"

	domainOrder "github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

type statusRepoStub struct {
	order *domainOrder.Order
}

func (s *statusRepoStub) FindByID(_ context.Context, id string) (*domainOrder.Order, error) {
	if s.order == nil || s.order.ID != id {
		return nil, nil
	}
	cp := *s.order
	return &cp, nil
}

func (s *statusRepoStub) FindByCustomerID(context.Context, string) ([]domainOrder.Order, error) {
	return nil, nil
}
func (s *statusRepoStub) FindByContactEmail(context.Context, string) ([]domainOrder.Order, error) {
	return nil, nil
}
func (s *statusRepoStub) List(context.Context, int, int) ([]domainOrder.Order, error) {
	return nil, nil
}
func (s *statusRepoStub) Save(context.Context, *domainOrder.Order) error { return nil }
func (s *statusRepoStub) UpdateStatus(_ context.Context, o *domainOrder.Order) error {
	*s.order = *o
	return nil
}
func (s *statusRepoStub) LinkToCustomer(context.Context, *domainOrder.Order) error { return nil }
func (s *statusRepoStub) LinkToCustomerByContactEmail(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}
func (s *statusRepoStub) ListPaidTaxSnapshots(context.Context, time.Time, time.Time) ([]domainOrder.TaxSnapshotRow, error) {
	return nil, nil
}

func testOrder(t *testing.T) domainOrder.Order {
	t.Helper()
	price := shared.MustNewMoney(1000, "EUR")
	item, err := domainOrder.NewItem("var-1", "SKU", "Widget", 1, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	o, err := domainOrder.NewOrder("ord-1", "cust-1", "buyer@example.com", "EUR", []domainOrder.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return o
}

func TestStatusService_ApplyStatus_ConfirmsPendingOrder(t *testing.T) {
	o := testOrder(t)
	repo := &statusRepoStub{order: &o}
	svc := NewStatusService(repo)

	updated, previous, changed, err := svc.ApplyStatus(context.Background(), "ord-1", domainOrder.OrderStatusConfirmed)
	if err != nil || !changed || updated.Status() != domainOrder.OrderStatusConfirmed || previous != domainOrder.OrderStatusPending {
		t.Fatalf("ApplyStatus() = (%v, %q, %v, %v)", updated, previous, changed, err)
	}
}

func TestStatusService_ApplyStatus_IdempotentWhenAlreadyConfirmed(t *testing.T) {
	o := testOrder(t)
	if err := o.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	repo := &statusRepoStub{order: &o}
	svc := NewStatusService(repo)

	updated, previous, changed, err := svc.ApplyStatus(context.Background(), "ord-1", domainOrder.OrderStatusConfirmed)
	if err != nil || changed || updated.Status() != domainOrder.OrderStatusConfirmed || previous != domainOrder.OrderStatusConfirmed {
		t.Fatalf("ApplyStatus() = (%v, %q, %v, %v)", updated, previous, changed, err)
	}
}

func TestStatusService_ApplyStatus_NotFound(t *testing.T) {
	svc := NewStatusService(&statusRepoStub{})
	_, _, _, err := svc.ApplyStatus(context.Background(), "missing", domainOrder.OrderStatusConfirmed)
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeNotFound {
		t.Fatalf("ApplyStatus() err = %v", err)
	}
}

func TestStatusService_ApplyStatus_InvalidTransition(t *testing.T) {
	o := testOrder(t)
	repo := &statusRepoStub{order: &o}
	svc := NewStatusService(repo)

	_, _, _, err := svc.ApplyStatus(context.Background(), "ord-1", domainOrder.OrderStatusPaid)
	var appErr *apperror.Error
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidation {
		t.Fatalf("ApplyStatus() err = %v", err)
	}
}
