package invoice_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	appInvoice "github.com/akarso/shopanda/internal/application/invoice"
	domainInvoice "github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ── fakes ───────────────────────────────────────────────────────────────

type fakeOrderRepo struct {
	orders map[string]*order.Order
}

func (f *fakeOrderRepo) FindByID(_ context.Context, id string) (*order.Order, error) {
	return f.orders[id], nil
}

func (f *fakeOrderRepo) FindByIDs(ctx context.Context, ids []string) (map[string]*order.Order, error) {
	out := make(map[string]*order.Order, len(ids))
	for _, id := range ids {
		o, err := f.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if o != nil {
			out[o.ID] = o
		}
	}
	return out, nil
}
func (f *fakeOrderRepo) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (f *fakeOrderRepo) FindByContactEmail(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (f *fakeOrderRepo) List(context.Context, int, int) ([]order.Order, error) {
	return nil, nil
}
func (f *fakeOrderRepo) Save(context.Context, *order.Order) error           { return nil }
func (f *fakeOrderRepo) UpdateStatus(context.Context, *order.Order) error   { return nil }
func (f *fakeOrderRepo) LinkToCustomer(context.Context, *order.Order) error { return nil }
func (f *fakeOrderRepo) LinkToCustomerByContactEmail(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}
func (f *fakeOrderRepo) ListPaidTaxSnapshots(context.Context, time.Time, time.Time) ([]order.TaxSnapshotRow, error) {
	return nil, nil
}
func (f *fakeOrderRepo) ListCreatedSince(context.Context, time.Time, string, int) ([]order.Order, error) {
	return nil, nil
}


type fakeInvoiceRepo struct {
	saved   *domainInvoice.Invoice
	byOrder map[string]*domainInvoice.Invoice
	byID    map[string]*domainInvoice.Invoice
	seq     int64
	deleted []string
}

func (f *fakeInvoiceRepo) FindByID(_ context.Context, id string) (*domainInvoice.Invoice, error) {
	if f.byID != nil {
		return f.byID[id], nil
	}
	if f.saved != nil && f.saved.ID() == id {
		return f.saved, nil
	}
	return nil, nil
}
func (f *fakeInvoiceRepo) FindByOrderID(_ context.Context, orderID string) (*domainInvoice.Invoice, error) {
	if f.byOrder != nil {
		return f.byOrder[orderID], nil
	}
	return nil, nil
}
func (f *fakeInvoiceRepo) Save(_ context.Context, inv *domainInvoice.Invoice) error {
	f.seq++
	inv.SetInvoiceNumberFromDB(f.seq)
	cp := *inv
	f.saved = &cp
	if f.byID == nil {
		f.byID = make(map[string]*domainInvoice.Invoice)
	}
	f.byID[inv.ID()] = &cp
	if f.byOrder == nil {
		f.byOrder = make(map[string]*domainInvoice.Invoice)
	}
	f.byOrder[inv.OrderID()] = &cp
	return nil
}
func (f *fakeInvoiceRepo) Delete(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)
	if f.byID != nil {
		delete(f.byID, id)
	}
	if f.saved != nil && f.saved.ID() == id {
		if f.byOrder != nil {
			delete(f.byOrder, f.saved.OrderID())
		}
		f.saved = nil
	}
	return nil
}

type fakeRenderer struct {
	called bool
	err    error
	seller domainInvoice.SellerBlock
}

func (f *fakeRenderer) Render(_ domainInvoice.Invoice, seller domainInvoice.SellerBlock) ([]byte, error) {
	f.called = true
	f.seller = seller
	if f.err != nil {
		return nil, f.err
	}
	return []byte("%PDF-fake"), nil
}

type fakeSellerLoader struct {
	block domainInvoice.SellerBlock
	err   error
	calls int
}

func (f *fakeSellerLoader) InvoiceSeller(context.Context, string) (domainInvoice.SellerBlock, error) {
	f.calls++
	if f.err != nil {
		return domainInvoice.SellerBlock{}, f.err
	}
	return f.block, nil
}

type failingStorage struct {
	fakeStorage
	err error
}

func (f *failingStorage) Save(string, io.Reader) error {
	return f.err
}

type fakeStorage struct {
	saved map[string][]byte
}

func (f *fakeStorage) Name() string { return "fake" }
func (f *fakeStorage) Save(path string, file io.Reader) error {
	b, _ := io.ReadAll(file)
	if f.saved == nil {
		f.saved = make(map[string][]byte)
	}
	f.saved[path] = b
	return nil
}
func (f *fakeStorage) Delete(string) error    { return nil }
func (f *fakeStorage) URL(path string) string { return "http://fake/" + path }

// ── helpers ─────────────────────────────────────────────────────────────

func paidOrder(t *testing.T) *order.Order {
	t.Helper()
	price := shared.MustNewMoney(1000, "EUR")
	item, err := order.NewItem("v-1", "SKU-001", "Blue Shirt", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	ord, err := order.NewOrder("ord-1", "cust-1", "", "EUR", []order.Item{item})
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

func newService(
	orders *fakeOrderRepo,
	invoices *fakeInvoiceRepo,
	renderer *fakeRenderer,
	storage *fakeStorage,
) *appInvoice.Service {
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	return appInvoice.NewService(orders, invoices, renderer, storage, bus, log)
}

// ── tests ───────────────────────────────────────────────────────────────

func TestGenerateFromOrder_Success(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}
	invoices := &fakeInvoiceRepo{}
	renderer := &fakeRenderer{}
	storage := &fakeStorage{}
	svc := newService(orders, invoices, renderer, storage)

	tax := shared.MustNewMoney(380, "EUR")
	result, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: tax,
	})
	if err != nil {
		t.Fatalf("GenerateFromOrder: %v", err)
	}
	if result.Invoice == nil {
		t.Fatal("expected non-nil invoice")
	}
	if result.Invoice.InvoiceNumber() != 1 {
		t.Errorf("InvoiceNumber = %d, want 1", result.Invoice.InvoiceNumber())
	}
	if result.Invoice.OrderID() != "ord-1" {
		t.Errorf("OrderID = %q, want ord-1", result.Invoice.OrderID())
	}
	// subtotal = 1000*2 = 2000
	if result.Invoice.SubtotalAmount().Amount() != 2000 {
		t.Errorf("SubtotalAmount = %d, want 2000", result.Invoice.SubtotalAmount().Amount())
	}
	if result.Invoice.TotalAmount().Amount() != 2380 {
		t.Errorf("TotalAmount = %d, want 2380", result.Invoice.TotalAmount().Amount())
	}
	if !renderer.called {
		t.Error("expected renderer to be called")
	}
	expectedPath := fmt.Sprintf("invoices/%s/invoice-%d.pdf", result.Invoice.ID(), result.Invoice.InvoiceNumber())
	if result.PDFPath != expectedPath {
		t.Errorf("PDFPath = %q, want %q", result.PDFPath, expectedPath)
	}
	if _, ok := storage.saved[expectedPath]; !ok {
		t.Errorf("storage missing key %q, got keys: %v", expectedPath, storageKeys(storage))
	}
}

func TestGenerateFromOrder_GuestOrder(t *testing.T) {
	price := shared.MustNewMoney(1000, "EUR")
	item, err := order.NewItem("v-1", "SKU-001", "Blue Shirt", 2, price)
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	ord, err := order.NewOrder("ord-guest", "", "guest@example.com", "EUR", []order.Item{item})
	if err != nil {
		t.Fatalf("NewOrder guest: %v", err)
	}
	if err := ord.Confirm(); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if err := ord.MarkPaid(); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}

	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-guest": &ord}}
	invoices := &fakeInvoiceRepo{}
	svc := newService(orders, invoices, &fakeRenderer{}, &fakeStorage{})

	result, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-guest",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	})
	if err != nil {
		t.Fatalf("GenerateFromOrder guest: %v", err)
	}
	if result.Invoice.CustomerID() != "" {
		t.Errorf("CustomerID = %q, want empty for guest invoice", result.Invoice.CustomerID())
	}
	if invoices.saved == nil {
		t.Fatal("expected guest invoice to be saved")
	}
}

func TestGenerateFromOrder_EmptyOrderID(t *testing.T) {
	svc := newService(
		&fakeOrderRepo{},
		&fakeInvoiceRepo{},
		&fakeRenderer{},
		&fakeStorage{},
	)
	_, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{})
	if err == nil {
		t.Fatal("expected error for empty order id")
	}
}

func TestGenerateFromOrder_OrderNotFound(t *testing.T) {
	svc := newService(
		&fakeOrderRepo{orders: map[string]*order.Order{}},
		&fakeInvoiceRepo{},
		&fakeRenderer{},
		&fakeStorage{},
	)
	tax := shared.MustNewMoney(0, "EUR")
	_, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "missing",
		TaxAmount: tax,
	})
	if err == nil {
		t.Fatal("expected error for missing order")
	}
}

func TestGenerateFromOrder_OrderNotPaid(t *testing.T) {
	price := shared.MustNewMoney(1000, "EUR")
	item, _ := order.NewItem("v-1", "SKU", "Shirt", 1, price)
	ord, _ := order.NewOrder("ord-1", "cust-1", "", "EUR", []order.Item{item})
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": &ord}}
	svc := newService(orders, &fakeInvoiceRepo{}, &fakeRenderer{}, &fakeStorage{})

	tax := shared.MustNewMoney(0, "EUR")
	_, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: tax,
	})
	if err == nil {
		t.Fatal("expected error for unpaid order")
	}
}

func TestGenerateFromOrder_InvoiceAlreadyExists(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}

	// Pre-create an invoice for this order.
	existingInv, _ := domainInvoice.NewInvoice(
		"inv-1", "ord-1", "cust-1", "EUR",
		[]domainInvoice.Item{mustInvoiceItem(t)},
		shared.MustNewMoney(380, "EUR"),
	)
	invoices := &fakeInvoiceRepo{
		byOrder: map[string]*domainInvoice.Invoice{"ord-1": &existingInv},
	}
	svc := newService(orders, invoices, &fakeRenderer{}, &fakeStorage{})

	tax := shared.MustNewMoney(380, "EUR")
	_, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: tax,
	})
	if err == nil {
		t.Fatal("expected error for duplicate invoice")
	}
}

func mustInvoiceItem(t *testing.T) domainInvoice.Item {
	t.Helper()
	item, err := domainInvoice.NewItem("v-1", "SKU-001", "Blue Shirt", 2, shared.MustNewMoney(1000, "EUR"))
	if err != nil {
		t.Fatalf("NewItem: %v", err)
	}
	return item
}

func storageKeys(s *fakeStorage) []string {
	keys := make([]string, 0, len(s.saved))
	for k := range s.saved {
		keys = append(keys, k)
	}
	return keys
}

func TestGenerateFromOrder_SealsSellerSnapshot(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}
	invoices := &fakeInvoiceRepo{}
	renderer := &fakeRenderer{}
	loader := &fakeSellerLoader{
		block: domainInvoice.SellerBlock{
			LegalName: "Spółka\nŁódź",
			TaxIDs:    []domainInvoice.SellerTaxLine{{Label: "NIP", Value: "5252344022"}},
		},
	}
	svc := newService(orders, invoices, renderer, &fakeStorage{}).
		WithSellerLoader(loader).
		WithDefaultStoreLookup(func(context.Context) (string, error) { return "store-1", nil })

	result, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	})
	if err != nil {
		t.Fatalf("GenerateFromOrder: %v", err)
	}
	if loader.calls != 1 {
		t.Fatalf("loader calls = %d, want 1", loader.calls)
	}
	got := result.Invoice.Seller()
	// SetSellerSnapshot sanitizes CR/LF before seal.
	if got.LegalName != "Spółka Łódź" {
		t.Errorf("saved LegalName = %q, want sanitized Polish name", got.LegalName)
	}
	if renderer.seller.LegalName != "Spółka Łódź" {
		t.Errorf("render seller LegalName = %q", renderer.seller.LegalName)
	}
	if invoices.saved == nil || invoices.saved.Seller().LegalName != "Spółka Łódź" {
		t.Fatal("repo saved invoice missing seller snapshot")
	}
	if len(got.TaxIDs) != 1 || got.TaxIDs[0].Value != "5252344022" {
		t.Errorf("TaxIDs = %#v", got.TaxIDs)
	}
}

func TestGenerateFromOrder_SellerLoaderErrorSkipsSave(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}
	invoices := &fakeInvoiceRepo{}
	renderer := &fakeRenderer{}
	loader := &fakeSellerLoader{err: fmt.Errorf("db down")}
	svc := newService(orders, invoices, renderer, &fakeStorage{}).
		WithSellerLoader(loader).
		WithDefaultStoreLookup(func(context.Context) (string, error) { return "store-1", nil })

	_, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	})
	if err == nil {
		t.Fatal("expected seller loader error")
	}
	if invoices.saved != nil {
		t.Fatal("Save must not run when InvoiceSeller fails")
	}
	if renderer.called {
		t.Fatal("renderer must not run when InvoiceSeller fails")
	}
}

func TestGenerateFromOrder_MissingDefaultStoreErrors(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}
	invoices := &fakeInvoiceRepo{}
	svc := newService(orders, invoices, &fakeRenderer{}, &fakeStorage{}).
		WithSellerLoader(&fakeSellerLoader{}).
		WithDefaultStoreLookup(func(context.Context) (string, error) { return "", nil })

	_, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	})
	if err == nil {
		t.Fatal("expected error when default store id is empty")
	}
	if invoices.saved != nil {
		t.Fatal("Save must not run without a store")
	}
}

func TestGenerateFromOrder_RenderFailureDeletesInvoice(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}
	invoices := &fakeInvoiceRepo{}
	renderer := &fakeRenderer{err: fmt.Errorf("font boom")}
	svc := newService(orders, invoices, renderer, &fakeStorage{})

	_, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	})
	if err == nil {
		t.Fatal("expected render error")
	}
	if len(invoices.deleted) != 1 {
		t.Fatalf("deleted = %v, want one invoice id", invoices.deleted)
	}
	if invoices.saved != nil {
		t.Fatal("saved invoice must be cleared after rollback")
	}
	// Retry must not hit conflict.
	renderer.err = nil
	if _, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	}); err != nil {
		t.Fatalf("retry after render failure: %v", err)
	}
}

func TestHandleOrderPaid_IssuesInvoice(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}
	invoices := &fakeInvoiceRepo{}
	svc := newService(orders, invoices, &fakeRenderer{}, &fakeStorage{})

	evt := event.New(order.EventOrderPaid, "test", order.OrderStatusChangedData{
		OrderID:   "ord-1",
		OldStatus: "confirmed",
		NewStatus: "paid",
	})
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("HandleOrderPaid: %v", err)
	}
	if invoices.saved == nil {
		t.Fatal("expected invoice saved from order.paid")
	}
	// Second delivery is idempotent.
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("second HandleOrderPaid: %v", err)
	}
}
