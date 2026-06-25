package returns_test

import (
	"context"
	"io"
	"testing"
	"time"

	returnsApp "github.com/akarso/shopanda/internal/application/returns"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	domainReturns "github.com/akarso/shopanda/internal/domain/returns"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type memReturnRepo struct {
	byID    map[string]*domainReturns.Return
	byOrder map[string][]domainReturns.Return
}

func newMemReturnRepo() *memReturnRepo {
	return &memReturnRepo{
		byID:    make(map[string]*domainReturns.Return),
		byOrder: make(map[string][]domainReturns.Return),
	}
}

func (r *memReturnRepo) Save(_ context.Context, ret *domainReturns.Return) error {
	cp := *ret
	r.byID[ret.ID] = &cp
	r.byOrder[ret.OrderID] = append(r.byOrder[ret.OrderID], cp)
	return nil
}

func (r *memReturnRepo) FindByID(_ context.Context, id string) (*domainReturns.Return, error) {
	return r.byID[id], nil
}

func (r *memReturnRepo) FindByOrderID(_ context.Context, orderID string) ([]domainReturns.Return, error) {
	return r.byOrder[orderID], nil
}

func (r *memReturnRepo) Update(_ context.Context, ret *domainReturns.Return) error {
	cp := *ret
	r.byID[ret.ID] = &cp
	list := r.byOrder[ret.OrderID]
	for i := range list {
		if list[i].ID == ret.ID {
			list[i] = cp
			r.byOrder[ret.OrderID] = list
			return nil
		}
	}
	return nil
}

type memOrderRepo struct {
	order *order.Order
}

func (r *memOrderRepo) FindByID(_ context.Context, _ string) (*order.Order, error) {
	return r.order, nil
}
func (r *memOrderRepo) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (r *memOrderRepo) FindByContactEmail(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (r *memOrderRepo) List(context.Context, int, int) ([]order.Order, error) { return nil, nil }
func (r *memOrderRepo) Save(context.Context, *order.Order) error              { return nil }
func (r *memOrderRepo) UpdateStatus(context.Context, *order.Order) error      { return nil }
func (r *memOrderRepo) LinkToCustomer(context.Context, *order.Order) error    { return nil }
func (r *memOrderRepo) LinkToCustomerByContactEmail(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}

type memStockRepo struct {
	qty map[string]int
}

func (r *memStockRepo) GetStock(_ context.Context, variantID string) (inventory.StockEntry, error) {
	return inventory.StockEntry{VariantID: variantID, Quantity: r.qty[variantID]}, nil
}
func (r *memStockRepo) SetStock(_ context.Context, entry *inventory.StockEntry) error {
	r.qty[entry.VariantID] = entry.Quantity
	return nil
}
func (r *memStockRepo) ListStock(context.Context, int, int) ([]inventory.StockEntry, error) {
	return nil, nil
}
func (r *memStockRepo) ListInventory(context.Context, int, int, string) ([]inventory.InventoryListItem, error) {
	return nil, nil
}
func (r *memStockRepo) GetInventoryItem(context.Context, string) (inventory.InventoryListItem, error) {
	return inventory.InventoryListItem{}, nil
}

type memPaymentRepo struct {
	payment *payment.Payment
}

func (r *memPaymentRepo) FindByID(context.Context, string) (*payment.Payment, error) { return nil, nil }
func (r *memPaymentRepo) FindByOrderID(context.Context, string) (*payment.Payment, error) {
	return r.payment, nil
}
func (r *memPaymentRepo) Create(context.Context, *payment.Payment) error { return nil }
func (r *memPaymentRepo) UpdateStatus(context.Context, *payment.Payment, time.Time) error {
	return nil
}

type stubRefunder struct {
	called bool
}

func (s *stubRefunder) Refund(context.Context, string, int64, string) (payment.RefundResult, error) {
	s.called = true
	return payment.RefundResult{ProviderRef: "re_123"}, nil
}

func paidOrder(t *testing.T) *order.Order {
	t.Helper()
	item, err := order.NewItem("v1", "SKU-1", "Widget", 2, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	ord, err := order.NewOrder("o1", "c1", "", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	if err := ord.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := ord.MarkPaid(); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}
	return &ord
}

func newService(t *testing.T, orders *memOrderRepo, returns *memReturnRepo, stock *memStockRepo, payments *memPaymentRepo, refunder payment.Refunder) *returnsApp.Service {
	t.Helper()
	return returnsApp.NewService(returns, orders, stock, payments, refunder, event.NewBus(logger.NewWithWriter(io.Discard, "info")), logger.NewWithWriter(io.Discard, "info"))
}

func TestService_RequestReturnAndWorkflow(t *testing.T) {
	orders := &memOrderRepo{order: paidOrder(t)}
	returns := newMemReturnRepo()
	stock := &memStockRepo{qty: map[string]int{"v1": 5}}
	pay, err := payment.NewPayment("pay1", "o1", payment.MethodStripe, shared.MustNewMoney(2000, "EUR"))
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	_ = pay.Complete("pi_123")
	payments := &memPaymentRepo{payment: &pay}
	refunder := &stubRefunder{}
	svc := newService(t, orders, returns, stock, payments, refunder)

	ret, err := svc.RequestReturn(context.Background(), "o1", "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "v1", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if ret.Status() != domainReturns.StatusRequested {
		t.Fatalf("status = %q", ret.Status())
	}

	ret, err = svc.Approve(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	ret, err = svc.Receive(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if stock.qty["v1"] != 6 {
		t.Fatalf("stock = %d, want 6", stock.qty["v1"])
	}
	ret, err = svc.Refund(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if ret.Status() != domainReturns.StatusRefunded {
		t.Fatalf("status = %q", ret.Status())
	}
	if !refunder.called {
		t.Fatal("expected provider refund")
	}
}

func TestService_RequestReturn_RejectsUnpaidOrder(t *testing.T) {
	item, _ := order.NewItem("v1", "SKU-1", "Widget", 1, shared.MustNewMoney(1000, "EUR"))
	ord, _ := order.NewOrder("o1", "c1", "", "EUR", []order.Item{item})
	orders := &memOrderRepo{order: &ord}
	svc := newService(t, orders, newMemReturnRepo(), &memStockRepo{qty: map[string]int{}}, &memPaymentRepo{}, nil)

	_, err := svc.RequestReturn(context.Background(), "o1", "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "v1", Quantity: 1},
	})
	if err == nil {
		t.Fatal("expected error for unpaid order")
	}
}

func TestService_RequestReturn_ExceedsOrderedQty(t *testing.T) {
	orders := &memOrderRepo{order: paidOrder(t)}
	svc := newService(t, orders, newMemReturnRepo(), &memStockRepo{qty: map[string]int{}}, &memPaymentRepo{}, nil)

	_, err := svc.RequestReturn(context.Background(), "o1", "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "v1", Quantity: 3},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestService_Receive_RetryDoesNotDoubleRestock(t *testing.T) {
	orders := &memOrderRepo{order: paidOrder(t)}
	returns := newMemReturnRepo()
	stock := &memStockRepo{qty: map[string]int{"v1": 5}}
	svc := newService(t, orders, returns, stock, &memPaymentRepo{}, nil)

	ret, err := svc.RequestReturn(context.Background(), "o1", "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "v1", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	ret, err = svc.Approve(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	ret, err = svc.Receive(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	ret, err = svc.Receive(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Receive retry: %v", err)
	}
	if stock.qty["v1"] != 6 {
		t.Fatalf("stock = %d, want 6 after idempotent retry", stock.qty["v1"])
	}
	if ret.RestockedAt == nil {
		t.Fatal("expected restocked_at after receive")
	}
}

func TestService_Refund_RetryDoesNotDoubleRefund(t *testing.T) {
	orders := &memOrderRepo{order: paidOrder(t)}
	returns := newMemReturnRepo()
	stock := &memStockRepo{qty: map[string]int{"v1": 5}}
	pay, err := payment.NewPayment("pay1", "o1", payment.MethodStripe, shared.MustNewMoney(2000, "EUR"))
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	_ = pay.Complete("pi_123")
	refunder := &stubRefunder{}
	svc := newService(t, orders, returns, stock, &memPaymentRepo{payment: &pay}, refunder)

	ret, err := svc.RequestReturn(context.Background(), "o1", "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "v1", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	ret, err = svc.Approve(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	ret, err = svc.Receive(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	ret, err = svc.Refund(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if !refunder.called {
		t.Fatal("expected provider refund")
	}
	refunder.called = false
	ret, err = svc.Refund(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Refund retry: %v", err)
	}
	if refunder.called {
		t.Fatal("expected idempotent refund retry to skip provider call")
	}
	if ret.Status() != domainReturns.StatusRefunded {
		t.Fatalf("status = %q", ret.Status())
	}
}
