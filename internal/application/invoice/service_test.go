package invoice_test

import (
	"context"
	"io"
	"testing"

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
func (f *fakeOrderRepo) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (f *fakeOrderRepo) List(context.Context, int, int) ([]order.Order, error) {
	return nil, nil
}
func (f *fakeOrderRepo) Save(context.Context, *order.Order) error         { return nil }
func (f *fakeOrderRepo) UpdateStatus(context.Context, *order.Order) error { return nil }

type fakeInvoiceRepo struct {
	saved   *domainInvoice.Invoice
	byOrder map[string]*domainInvoice.Invoice
	seq     int64
}

func (f *fakeInvoiceRepo) FindByID(context.Context, string) (*domainInvoice.Invoice, error) {
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
	f.saved = inv
	return nil
}

type fakeRenderer struct {
	called bool
}

func (f *fakeRenderer) Render(domainInvoice.Invoice) ([]byte, error) {
	f.called = true
	return []byte("%PDF-fake"), nil
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
	ord, err := order.NewOrder("ord-1", "cust-1", "EUR", []order.Item{item})
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
	if len(storage.saved) != 1 {
		t.Errorf("stored files = %d, want 1", len(storage.saved))
	}
	if result.PDFPath == "" {
		t.Error("expected non-empty PDFPath")
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
	ord, _ := order.NewOrder("ord-1", "cust-1", "EUR", []order.Item{item})
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
