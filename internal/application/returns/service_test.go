package returns_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	returnsApp "github.com/akarso/shopanda/internal/application/returns"
	"github.com/akarso/shopanda/internal/domain/catalog"
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

func (r *memReturnRepo) FindByCustomerID(_ context.Context, customerID string) ([]domainReturns.Return, error) {
	var out []domainReturns.Return
	for _, ret := range r.byID {
		if ret.CustomerID == customerID {
			out = append(out, *ret)
		}
	}
	return out, nil
}

func (r *memReturnRepo) List(_ context.Context, offset, limit int) ([]domainReturns.Return, error) {
	all := make([]domainReturns.Return, 0, len(r.byID))
	for _, ret := range r.byID {
		all = append(all, *ret)
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

func (r *memReturnRepo) WithTx(*sql.Tx) domainReturns.Repository { return r }

func (r *memReturnRepo) LockForRestock(ctx context.Context, id string) (*domainReturns.Return, error) {
	return r.FindByID(ctx, id)
}

func (r *memReturnRepo) UpdateIfUnrestocked(ctx context.Context, ret *domainReturns.Return) error {
	if ret == nil {
		return errors.New("nil return")
	}
	current := r.byID[ret.ID]
	if current != nil && current.RestockedAt != nil {
		return domainReturns.ErrAlreadyRestocked
	}
	return r.Update(ctx, ret)
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
func (r *memOrderRepo) ListPaidTaxSnapshots(context.Context, time.Time, time.Time) ([]order.TaxSnapshotRow, error) {
	return nil, nil
}

type memStockRepo struct {
	qty      map[string]int
	setCalls int
}

func (r *memStockRepo) GetStock(_ context.Context, variantID string) (inventory.StockEntry, error) {
	return inventory.StockEntry{VariantID: variantID, Quantity: r.qty[variantID]}, nil
}
func (r *memStockRepo) GetStockForUpdate(ctx context.Context, variantID string) (inventory.StockEntry, error) {
	return r.GetStock(ctx, variantID)
}
func (r *memStockRepo) WithTx(*sql.Tx) inventory.StockRepository { return r }
func (r *memStockRepo) SetStock(_ context.Context, entry *inventory.StockEntry) error {
	r.setCalls++
	if r.qty == nil {
		r.qty = map[string]int{}
	}
	r.qty[entry.VariantID] = entry.Quantity
	return nil
}
func (r *memStockRepo) SetStocks(ctx context.Context, entries []inventory.StockEntry) error {
	for i := range entries {
		if err := r.SetStock(ctx, &entries[i]); err != nil {
			return err
		}
	}
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
func (r *memPaymentRepo) List(context.Context, payment.ListFilter) ([]payment.Payment, error) {
	return nil, nil
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
	item.Restockable = true
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

// memVariantRepo is a minimal catalog.VariantRepository fake — every
// variant ID resolves to a variant on product "p1", matching paidOrder's
// own "v1"/"SKU-1" item. Tests that care about the specific product/SKU a
// restock event carries construct their own returnsApp.Service directly
// instead of going through newService.
type memVariantRepo struct{}

func (memVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return &catalog.Variant{ID: id, ProductID: "p1", SKU: "SKU-1"}, nil
}
func (memVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) { return nil, nil }
func (memVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}
func (memVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (memVariantRepo) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (memVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (memVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

func newService(t *testing.T, orders *memOrderRepo, returns *memReturnRepo, stock *memStockRepo, payments *memPaymentRepo, refunder payment.Refunder) *returnsApp.Service {
	t.Helper()
	return returnsApp.NewService(returns, orders, stock, memVariantRepo{}, payments, refunder, event.NewBus(logger.NewWithWriter(io.Discard, "info")), logger.NewWithWriter(io.Discard, "info")).
		WithTxStarter(returnsFakeTxDB())
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

// TestService_Receive_PublishesStockUpdatedEvent pins PR-1049's returns
// fix: a successful restock must publish inventory.EventStockUpdated per
// item, alongside the existing EventReturnReceived — restocking is a real
// stock change the search index's on-save subscriber previously never
// heard about.
func TestService_Receive_PublishesStockUpdatedEvent(t *testing.T) {
	orders := &memOrderRepo{order: paidOrder(t)}
	returns := newMemReturnRepo()
	stock := &memStockRepo{qty: map[string]int{"v1": 5}}
	bus := event.NewBus(logger.NewWithWriter(io.Discard, "info"))
	svc := returnsApp.NewService(returns, orders, stock, memVariantRepo{}, &memPaymentRepo{}, nil, bus, logger.NewWithWriter(io.Discard, "info")).
		WithTxStarter(returnsFakeTxDB())

	var captured []event.Event
	bus.On(inventory.EventStockUpdated, func(_ context.Context, evt event.Event) error {
		captured = append(captured, evt)
		return nil
	})

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
	if _, err := svc.Receive(context.Background(), ret.ID); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	if len(captured) != 1 {
		t.Fatalf("published %d inventory.EventStockUpdated events, want 1", len(captured))
	}
	data, ok := captured[0].Data.(inventory.StockUpdatedData)
	if !ok {
		t.Fatalf("event data type = %T, want StockUpdatedData", captured[0].Data)
	}
	if data.VariantID != "v1" || data.ProductID != "p1" || data.SKU != "SKU-1" || data.OnHand == nil || *data.OnHand != 6 || data.Delta == nil || *data.Delta != 1 {
		t.Errorf("data = %+v, want variant_id=v1 product_id=p1 sku=SKU-1 on_hand=6 delta=1 (5 + 1 restocked)", data)
	}
}

// failAfterVariantRepo succeeds FindByID remaining times (so Receive can
// classify the product and restock) and then returns err. The stock-event
// publish lookup is that later call; its failure must be logged, not returned.
type failAfterVariantRepo struct {
	remaining int
	err       error
}

func (r *failAfterVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	if r.remaining <= 0 {
		return nil, r.err
	}
	r.remaining--
	return &catalog.Variant{ID: id, ProductID: "p1", SKU: "SKU-1"}, nil
}
func (*failAfterVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (*failAfterVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}
func (*failAfterVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (*failAfterVariantRepo) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (*failAfterVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (*failAfterVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

// TestService_Receive_SwallowsVariantLookupFailure pins that a failed
// variant lookup during publishStockUpdated is logged, not returned —
// Receive has already durably recorded the restock and the return's own
// state transition by that point, so a stale search index entry is not
// worth reporting a successful restock as a failure.
func TestService_Receive_SwallowsVariantLookupFailure(t *testing.T) {
	orders := &memOrderRepo{order: paidOrder(t)}
	returns := newMemReturnRepo()
	stock := &memStockRepo{qty: map[string]int{"v1": 5}}
	var logBuf bytes.Buffer
	svc := returnsApp.NewService(returns, orders, stock, &failAfterVariantRepo{remaining: 0, err: errors.New("db down")}, &memPaymentRepo{}, nil,
		event.NewBus(logger.NewWithWriter(io.Discard, "info")), logger.NewWithWriter(&logBuf, "info")).
		WithTxStarter(returnsFakeTxDB())

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
	if _, err := svc.Receive(context.Background(), ret.ID); err != nil {
		t.Fatalf("Receive: %v, want nil despite the variant lookup failing", err)
	}
	if stock.qty["v1"] != 6 {
		t.Fatalf("stock = %d, want 6 (restock itself must still succeed)", stock.qty["v1"])
	}
	if !strings.Contains(logBuf.String(), "returns: variant lookup failed for stock event") {
		t.Errorf("log output = %q, want it to contain the variant-lookup-failed warning", logBuf.String())
	}
}

// TestService_Receive_SwallowsStockEventPublishFailure pins the other
// swallowed-error path in publishStockUpdated: a failing bus.Publish call
// (a sync handler erroring) is logged, not returned — same reasoning as
// the variant-lookup case above.
func TestService_Receive_SwallowsStockEventPublishFailure(t *testing.T) {
	orders := &memOrderRepo{order: paidOrder(t)}
	returns := newMemReturnRepo()
	stock := &memStockRepo{qty: map[string]int{"v1": 5}}
	var logBuf bytes.Buffer
	bus := event.NewBus(logger.NewWithWriter(io.Discard, "info"))
	bus.On(inventory.EventStockUpdated, func(context.Context, event.Event) error {
		return errors.New("sync handler exploded")
	})
	svc := returnsApp.NewService(returns, orders, stock, memVariantRepo{}, &memPaymentRepo{}, nil, bus, logger.NewWithWriter(&logBuf, "info")).
		WithTxStarter(returnsFakeTxDB())

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
	if _, err := svc.Receive(context.Background(), ret.ID); err != nil {
		t.Fatalf("Receive: %v, want nil despite the stock event publish failing", err)
	}
	if stock.qty["v1"] != 6 {
		t.Fatalf("stock = %d, want 6 (restock itself must still succeed)", stock.qty["v1"])
	}
	if !strings.Contains(logBuf.String(), "returns: publish stock event failed") {
		t.Errorf("log output = %q, want it to contain the publish-failed warning", logBuf.String())
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

func TestService_Receive_ExplodedComponentRestocksSimpleSkipsDigital(t *testing.T) {
	simple, err := order.NewItem("v-simple", "SKU-S", "Simple", 2, shared.MustNewMoney(300, "EUR"))
	if err != nil {
		t.Fatalf("NewItem simple: %v", err)
	}
	simple.Restockable = true
	virtual, err := order.NewItem("v-virtual", "SKU-V", "Virtual", 1, shared.MustNewMoney(400, "EUR"))
	if err != nil {
		t.Fatalf("NewItem virtual: %v", err)
	}
	virtual.Restockable = false
	download, err := order.NewItem("v-download", "SKU-D", "Download", 1, shared.MustNewMoney(100, "EUR"))
	if err != nil {
		t.Fatalf("NewItem download: %v", err)
	}
	download.Restockable = false
	ord, err := order.NewOrder("o1", "c1", "", "EUR", []order.Item{simple, virtual, download})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	if err := ord.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := ord.MarkPaid(); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}

	variants := mapVariantRepo{byID: map[string]*catalog.Variant{
		"v-simple":   {ID: "v-simple", ProductID: "p-simple", SKU: "SKU-S"},
		"v-virtual":  {ID: "v-virtual", ProductID: "p-virtual", SKU: "SKU-V"},
		"v-download": {ID: "v-download", ProductID: "p-download", SKU: "SKU-D"},
	}}
	stock := &memStockRepo{qty: map[string]int{"v-simple": 5, "v-download": 4}}
	// Restockable was snapshotted on the order line. Receive does not read products.
	svc := returnsApp.NewService(
		newMemReturnRepo(), &memOrderRepo{order: &ord}, stock, variants, &memPaymentRepo{}, nil,
		event.NewBus(logger.NewWithWriter(io.Discard, "info")), logger.NewWithWriter(io.Discard, "info"),
	).WithTxStarter(returnsFakeTxDB())

	ret, err := svc.RequestReturn(context.Background(), "o1", "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "v-simple", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn simple: %v", err)
	}
	if _, err := svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve simple: %v", err)
	}
	if _, err := svc.Receive(context.Background(), ret.ID); err != nil {
		t.Fatalf("Receive simple: %v", err)
	}
	if stock.qty["v-simple"] != 6 {
		t.Fatalf("simple stock = %d, want 6", stock.qty["v-simple"])
	}
	if stock.qty["v-download"] != 4 {
		t.Fatalf("download stock = %d, want 4 before its return", stock.qty["v-download"])
	}
	if _, ok := stock.qty["v-virtual"]; ok {
		t.Fatalf("virtual stock was created: %d", stock.qty["v-virtual"])
	}

	ret, err = svc.RequestReturn(context.Background(), "o1", "c1", "digital", []returnsApp.RequestLine{
		{VariantID: "v-virtual", Quantity: 1},
		{VariantID: "v-download", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn digital: %v", err)
	}
	if _, err := svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve digital: %v", err)
	}
	if _, err := svc.Receive(context.Background(), ret.ID); err != nil {
		t.Fatalf("Receive digital: %v", err)
	}
	if stock.qty["v-simple"] != 6 {
		t.Fatalf("simple stock changed on digital receive: %d", stock.qty["v-simple"])
	}
	if _, ok := stock.qty["v-virtual"]; ok {
		t.Fatalf("virtual leaf was restocked: %d", stock.qty["v-virtual"])
	}
	if stock.qty["v-download"] != 4 {
		t.Fatalf("downloadable leaf stock = %d, want 4", stock.qty["v-download"])
	}
	if stock.setCalls != 1 {
		t.Fatalf("SetStock calls = %d, want 1 (digital lines are not restocked)", stock.setCalls)
	}
}

type mapVariantRepo struct {
	byID map[string]*catalog.Variant
}

func (r mapVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return r.byID[id], nil
}
func (mapVariantRepo) FindBySKU(context.Context, string) (*catalog.Variant, error) { return nil, nil }
func (mapVariantRepo) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return nil, nil
}
func (mapVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (mapVariantRepo) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (mapVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (mapVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

func init() {
	sql.Register("shopanda-returns-fake-tx", returnsFakeTxDriver{})
	sql.Register("shopanda-returns-capture-tx", returnsCaptureTxDriver{})
}

var (
	returnsCaptureFinished func()
	returnsCaptureCommits  atomic.Int32
)

type returnsCaptureTxDriver struct{}

func (returnsCaptureTxDriver) Open(string) (driver.Conn, error) { return returnsCaptureTxConn{}, nil }

type returnsCaptureTxConn struct{}

func (returnsCaptureTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (returnsCaptureTxConn) Close() error                        { return nil }
func (returnsCaptureTxConn) Begin() (driver.Tx, error)           { return returnsCaptureTx{}, nil }

type returnsCaptureTx struct{}

func (returnsCaptureTx) Commit() error {
	returnsCaptureCommits.Add(1)
	if returnsCaptureFinished != nil {
		returnsCaptureFinished()
	}
	return nil
}
func (returnsCaptureTx) Rollback() error {
	if returnsCaptureFinished != nil {
		returnsCaptureFinished()
	}
	return nil
}

type returnsFakeTxDriver struct{}

func (returnsFakeTxDriver) Open(string) (driver.Conn, error) { return returnsFakeTxConn{}, nil }

type returnsFakeTxConn struct{}

func (returnsFakeTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (returnsFakeTxConn) Close() error                        { return nil }
func (returnsFakeTxConn) Begin() (driver.Tx, error)           { return returnsFakeTx{}, nil }

type returnsFakeTx struct{}

func (returnsFakeTx) Commit() error   { return nil }
func (returnsFakeTx) Rollback() error { return nil }

var (
	returnsFakeTxOnce   sync.Once
	returnsFakeTxDBConn *sql.DB
)

func returnsFakeTxDB() *sql.DB {
	returnsFakeTxOnce.Do(func() {
		db, err := sql.Open("shopanda-returns-fake-tx", "")
		if err != nil {
			panic(err)
		}
		returnsFakeTxDBConn = db
	})
	return returnsFakeTxDBConn
}

func TestService_Receive_MissingOrderLineWritesNothing(t *testing.T) {
	orders := &memOrderRepo{order: paidOrder(t)}
	returns := newMemReturnRepo()
	stock := &memStockRepo{qty: map[string]int{"v1": 5}}
	svc := newService(t, orders, returns, stock, &memPaymentRepo{}, nil)

	kept, err := domainReturns.NewItem("v1", "SKU-1", "Widget", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem kept: %v", err)
	}
	missing, err := domainReturns.NewItem("v-missing", "SKU-X", "Missing", 1, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem missing: %v", err)
	}
	ret, err := domainReturns.NewReturn("r-missing", "o1", "c1", "damaged", "EUR", []domainReturns.Item{kept, missing})
	if err != nil {
		t.Fatalf("NewReturn: %v", err)
	}
	if err := returns.Save(context.Background(), &ret); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if _, err := svc.Receive(context.Background(), ret.ID); err == nil {
		t.Fatal("expected missing order line to fail receive")
	}
	if stock.setCalls != 0 {
		t.Fatalf("SetStock calls = %d, want 0", stock.setCalls)
	}
	if stock.qty["v1"] != 5 {
		t.Fatalf("stock = %d, want 5", stock.qty["v1"])
	}
	stored, err := returns.FindByID(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if stored.Status() != domainReturns.StatusApproved {
		t.Fatalf("status = %q, want approved", stored.Status())
	}
}

// staleReadReturnRepo hides RestockedAt from the unlocked read and shows it
// under LockForRestock, the window two overlapping Receives used to both pass.
type staleReadReturnRepo struct {
	*memReturnRepo
}

func (r *staleReadReturnRepo) WithTx(*sql.Tx) domainReturns.Repository { return r }

func (r *staleReadReturnRepo) FindByID(ctx context.Context, id string) (*domainReturns.Return, error) {
	ret, err := r.memReturnRepo.FindByID(ctx, id)
	if err != nil || ret == nil || ret.RestockedAt == nil {
		return ret, err
	}
	cp := *ret
	cp.RestockedAt = nil
	return &cp, nil
}

func (r *staleReadReturnRepo) LockForRestock(ctx context.Context, id string) (*domainReturns.Return, error) {
	ret, err := r.memReturnRepo.FindByID(ctx, id)
	if err != nil || ret == nil {
		return ret, err
	}
	cp := *ret
	return &cp, nil
}

func TestService_Receive_LockSeesRestockedSkipsStock(t *testing.T) {
	orders := &memOrderRepo{order: paidOrder(t)}
	returns := newMemReturnRepo()
	repo := &staleReadReturnRepo{memReturnRepo: returns}
	stock := &memStockRepo{qty: map[string]int{"v1": 5}}
	svc := returnsApp.NewService(repo, orders, stock, memVariantRepo{}, &memPaymentRepo{}, nil, event.NewBus(logger.NewWithWriter(io.Discard, "info")), logger.NewWithWriter(io.Discard, "info")).
		WithTxStarter(returnsFakeTxDB())

	ret, err := svc.RequestReturn(context.Background(), "o1", "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "v1", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if _, err := svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	stored, err := returns.FindByID(context.Background(), ret.ID)
	if err != nil || stored == nil {
		t.Fatalf("FindByID: %v", err)
	}
	if err := stored.SetStatusFromDB(string(domainReturns.StatusReceived)); err != nil {
		t.Fatalf("SetStatusFromDB: %v", err)
	}
	restocked := time.Now().UTC()
	stored.RestockedAt = &restocked

	got, err := svc.Receive(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got.RestockedAt == nil {
		t.Fatal("expected the locked return to keep restocked_at")
	}
	if stock.setCalls != 0 || stock.qty["v1"] != 5 {
		t.Fatalf("stock calls=%d qty=%d, want no restock", stock.setCalls, stock.qty["v1"])
	}
}

// lostRestockReturnRepo lets Receive pass the lock, then reports that the
// conditional restock update matched zero rows.
type lostRestockReturnRepo struct {
	*memReturnRepo
}

func (r *lostRestockReturnRepo) WithTx(*sql.Tx) domainReturns.Repository { return r }

func (r *lostRestockReturnRepo) LockForRestock(ctx context.Context, id string) (*domainReturns.Return, error) {
	ret, err := r.memReturnRepo.FindByID(ctx, id)
	if err != nil || ret == nil {
		return ret, err
	}
	cp := *ret
	return &cp, nil
}

func (r *lostRestockReturnRepo) UpdateIfUnrestocked(ctx context.Context, ret *domainReturns.Return) error {
	stored, err := r.memReturnRepo.FindByID(ctx, ret.ID)
	if err != nil {
		return err
	}
	if stored != nil {
		cp := *stored
		now := time.Now().UTC()
		cp.RestockedAt = &now
		if err := cp.SetStatusFromDB(string(domainReturns.StatusReceived)); err != nil {
			return err
		}
		if err := r.memReturnRepo.Update(ctx, &cp); err != nil {
			return err
		}
	}
	return domainReturns.ErrAlreadyRestocked
}

func TestService_Receive_AlreadyRestockedUpdateRollsBack(t *testing.T) {
	orders := &memOrderRepo{order: paidOrder(t)}
	returns := newMemReturnRepo()
	repo := &lostRestockReturnRepo{memReturnRepo: returns}
	stock := &bufferingStock{qty: map[string]int{"v1": 5}}
	returnsCaptureCommits.Store(0)
	db, err := sql.Open("shopanda-returns-capture-tx", "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := returnsApp.NewService(repo, orders, stock, memVariantRepo{}, &memPaymentRepo{}, nil, event.NewBus(logger.NewWithWriter(io.Discard, "info")), logger.NewWithWriter(io.Discard, "info")).
		WithTxStarter(db)

	ret, err := svc.RequestReturn(context.Background(), "o1", "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "v1", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if _, err := svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	got, err := svc.Receive(context.Background(), ret.ID)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got.RestockedAt == nil {
		t.Fatal("expected the winning return")
	}
	if stock.qty["v1"] != 5 {
		t.Fatalf("committed stock = %d, want 5", stock.qty["v1"])
	}
	if stock.child == nil || stock.child.setCalls != 1 {
		t.Fatalf("in-tx SetStock calls = %v, want 1", stock.child)
	}
	if returnsCaptureCommits.Load() != 0 {
		t.Fatalf("commits = %d, want 0", returnsCaptureCommits.Load())
	}
}

// serialReturnRepo holds the second LockForRestock until the first transaction
// finishes, matching SELECT … FOR UPDATE.
type serialReturnRepo struct {
	*memReturnRepo
	mu       sync.Mutex
	arm      bool
	finds    int
	bothRead chan struct{}
	locked   bool
	done     chan struct{}
}

func (r *serialReturnRepo) WithTx(*sql.Tx) domainReturns.Repository { return r }

func (r *serialReturnRepo) FindByID(ctx context.Context, id string) (*domainReturns.Return, error) {
	ret, err := r.memReturnRepo.FindByID(ctx, id)
	if err != nil || ret == nil {
		return ret, err
	}
	cp := *ret
	if !r.arm {
		return &cp, nil
	}
	r.mu.Lock()
	r.finds++
	n := r.finds
	if n == 2 {
		close(r.bothRead)
	}
	r.mu.Unlock()
	if n <= 2 {
		<-r.bothRead
	}
	return &cp, nil
}

func (r *serialReturnRepo) LockForRestock(ctx context.Context, id string) (*domainReturns.Return, error) {
	r.mu.Lock()
	for r.locked {
		ch := r.done
		r.mu.Unlock()
		<-ch
		r.mu.Lock()
	}
	r.locked = true
	r.done = make(chan struct{})
	r.mu.Unlock()

	ret, err := r.memReturnRepo.FindByID(ctx, id)
	if err != nil || ret == nil {
		return ret, err
	}
	cp := *ret
	return &cp, nil
}

func (r *serialReturnRepo) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.locked {
		return
	}
	r.locked = false
	close(r.done)
}

func TestService_Receive_ConcurrentLockRestocksOnce(t *testing.T) {
	orders := &memOrderRepo{order: paidOrder(t)}
	returns := newMemReturnRepo()
	repo := &serialReturnRepo{memReturnRepo: returns, bothRead: make(chan struct{})}
	stock := &memStockRepo{qty: map[string]int{"v1": 5}}
	db, err := sql.Open("shopanda-returns-capture-tx", "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() {
		returnsCaptureFinished = nil
		_ = db.Close()
	})
	returnsCaptureFinished = repo.finish
	svc := returnsApp.NewService(repo, orders, stock, memVariantRepo{}, &memPaymentRepo{}, nil, event.NewBus(logger.NewWithWriter(io.Discard, "info")), logger.NewWithWriter(io.Discard, "info")).
		WithTxStarter(db)

	ret, err := svc.RequestReturn(context.Background(), "o1", "c1", "damaged", []returnsApp.RequestLine{
		{VariantID: "v1", Quantity: 1},
	})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}
	if _, err := svc.Approve(context.Background(), ret.ID); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	repo.arm = true

	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, recErr := svc.Receive(context.Background(), ret.ID)
			errCh <- recErr
		}()
	}
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent Receive did not finish")
	}
	close(errCh)
	for recErr := range errCh {
		if recErr != nil {
			t.Fatalf("Receive: %v", recErr)
		}
	}
	if stock.qty["v1"] != 6 || stock.setCalls != 1 {
		t.Fatalf("stock qty=%d calls=%d, want qty 6 and one SetStock", stock.qty["v1"], stock.setCalls)
	}
}

type bufferingStock struct {
	qty   map[string]int
	child *bufferingStockTx
}

func (b *bufferingStock) GetStock(_ context.Context, variantID string) (inventory.StockEntry, error) {
	return inventory.StockEntry{VariantID: variantID, Quantity: b.qty[variantID]}, nil
}
func (b *bufferingStock) GetStockForUpdate(ctx context.Context, variantID string) (inventory.StockEntry, error) {
	return b.GetStock(ctx, variantID)
}
func (b *bufferingStock) WithTx(*sql.Tx) inventory.StockRepository {
	q := make(map[string]int, len(b.qty))
	for k, v := range b.qty {
		q[k] = v
	}
	b.child = &bufferingStockTx{qty: q}
	return b.child
}
func (b *bufferingStock) SetStock(_ context.Context, entry *inventory.StockEntry) error {
	b.qty[entry.VariantID] = entry.Quantity
	return nil
}
func (b *bufferingStock) SetStocks(context.Context, []inventory.StockEntry) error { return nil }
func (b *bufferingStock) ListStock(context.Context, int, int) ([]inventory.StockEntry, error) {
	return nil, nil
}
func (b *bufferingStock) ListInventory(context.Context, int, int, string) ([]inventory.InventoryListItem, error) {
	return nil, nil
}
func (b *bufferingStock) GetInventoryItem(context.Context, string) (inventory.InventoryListItem, error) {
	return inventory.InventoryListItem{}, nil
}

type bufferingStockTx struct {
	qty      map[string]int
	setCalls int
}

func (t *bufferingStockTx) GetStock(_ context.Context, variantID string) (inventory.StockEntry, error) {
	return inventory.StockEntry{VariantID: variantID, Quantity: t.qty[variantID]}, nil
}
func (t *bufferingStockTx) GetStockForUpdate(ctx context.Context, variantID string) (inventory.StockEntry, error) {
	return t.GetStock(ctx, variantID)
}
func (t *bufferingStockTx) WithTx(*sql.Tx) inventory.StockRepository { return t }
func (t *bufferingStockTx) SetStock(_ context.Context, entry *inventory.StockEntry) error {
	t.setCalls++
	t.qty[entry.VariantID] = entry.Quantity
	return nil
}
func (t *bufferingStockTx) SetStocks(context.Context, []inventory.StockEntry) error { return nil }
func (t *bufferingStockTx) ListStock(context.Context, int, int) ([]inventory.StockEntry, error) {
	return nil, nil
}
func (t *bufferingStockTx) ListInventory(context.Context, int, int, string) ([]inventory.InventoryListItem, error) {
	return nil, nil
}
func (t *bufferingStockTx) GetInventoryItem(context.Context, string) (inventory.InventoryListItem, error) {
	return inventory.InventoryListItem{}, nil
}
