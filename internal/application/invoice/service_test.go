package invoice_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	appInvoice "github.com/akarso/shopanda/internal/application/invoice"
	domainInvoice "github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/apperror"
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
	saved     *domainInvoice.Invoice
	byOrder   map[string]*domainInvoice.Invoice
	byID      map[string]*domainInvoice.Invoice
	seq       int64
	deleted   []string
	saveErr   error
	deleteErr error
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
func (f *fakeInvoiceRepo) Save(ctx context.Context, inv *domainInvoice.Invoice) error {
	return f.SavePreparing(ctx, inv, nil)
}
func (f *fakeInvoiceRepo) SavePreparing(_ context.Context, inv *domainInvoice.Invoice, beforeCommit func(*domainInvoice.Invoice) error) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.seq++
	inv.SetInvoiceNumberFromDB(f.seq)
	if beforeCommit != nil {
		if err := beforeCommit(inv); err != nil {
			// Drop the in-memory row so FindByOrderID stays empty. Postgres
			// rolls back the INSERT but nextval still burns a sequence value;
			// this fake does not model that gap.
			inv.SetInvoiceNumberFromDB(0)
			return err
		}
	}
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
	if f.deleteErr != nil {
		return f.deleteErr
	}
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
	saved   map[string][]byte
	deleted []string
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
func (f *fakeStorage) Delete(path string) error {
	f.deleted = append(f.deleted, path)
	if f.saved != nil {
		delete(f.saved, path)
	}
	return nil
}
func (f *fakeStorage) URL(path string) string { return "http://fake/" + path }
func (f *fakeStorage) Read(_ context.Context, path string) ([]byte, error) {
	if f.saved == nil {
		return nil, os.ErrNotExist
	}
	b, ok := f.saved[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return b, nil
}

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

func TestGenerateFromOrder_ExistingRepairsMissingPDFAndPublishes(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}

	existingInv, _ := domainInvoice.NewInvoice(
		"inv-1", "ord-1", "cust-1", "EUR",
		[]domainInvoice.Item{mustInvoiceItem(t)},
		shared.MustNewMoney(380, "EUR"),
	)
	existingInv.SetInvoiceNumberFromDB(7)
	existingInv.SetSellerSnapshot(domainInvoice.SellerBlock{LegalName: "Acme"})
	invoices := &fakeInvoiceRepo{
		byOrder: map[string]*domainInvoice.Invoice{"ord-1": &existingInv},
		byID:    map[string]*domainInvoice.Invoice{"inv-1": &existingInv},
	}
	renderer := &fakeRenderer{}
	storage := &fakeStorage{}
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	var published int
	bus.On(domainInvoice.EventInvoiceCreated, func(context.Context, event.Event) error {
		published++
		return nil
	})
	svc := appInvoice.NewService(orders, invoices, renderer, storage, bus, log)

	result, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	})
	if err != nil {
		t.Fatalf("GenerateFromOrder repair: %v", err)
	}
	if !renderer.called {
		t.Fatal("expected repair to render PDF from sealed seller")
	}
	if renderer.seller.LegalName != "Acme" {
		t.Errorf("repair seller = %#v", renderer.seller)
	}
	wantPath := fmt.Sprintf("invoices/%s/invoice-%d.pdf", existingInv.ID(), existingInv.InvoiceNumber())
	if result.PDFPath != wantPath {
		t.Errorf("PDFPath = %q, want %q", result.PDFPath, wantPath)
	}
	if _, ok := storage.saved[wantPath]; !ok {
		t.Fatalf("PDF not stored at %q", wantPath)
	}
	if published != 1 {
		t.Fatalf("published invoice.created = %d, want 1", published)
	}

	// Second call: PDF present — still re-publishes (mail dedupes).
	renderer.called = false
	if _, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	}); err != nil {
		t.Fatalf("second GenerateFromOrder: %v", err)
	}
	if renderer.called {
		t.Fatal("must not re-render when PDF already stored")
	}
	if published != 2 {
		t.Fatalf("published invoice.created = %d, want 2", published)
	}
}

func TestGenerateFromOrder_SaveConflictRepairs(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}

	existingInv, _ := domainInvoice.NewInvoice(
		"inv-race", "ord-1", "cust-1", "EUR",
		[]domainInvoice.Item{mustInvoiceItem(t)},
		shared.MustNewMoney(380, "EUR"),
	)
	existingInv.SetInvoiceNumberFromDB(3)
	invoices := &fakeInvoiceRepo{
		saveErr: apperror.Conflict("invoice already exists for this order"),
		byOrder: map[string]*domainInvoice.Invoice{"ord-1": &existingInv},
		byID:    map[string]*domainInvoice.Invoice{"inv-race": &existingInv},
	}
	storage := &fakeStorage{}
	svc := newService(orders, invoices, &fakeRenderer{}, storage)

	result, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	})
	if err != nil {
		t.Fatalf("Save conflict repair: %v", err)
	}
	wantPath := fmt.Sprintf("invoices/%s/invoice-%d.pdf", existingInv.ID(), existingInv.InvoiceNumber())
	if result.PDFPath != wantPath {
		t.Errorf("PDFPath = %q", result.PDFPath)
	}
	if _, ok := storage.saved[wantPath]; !ok {
		t.Fatal("expected repaired PDF stored")
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

func TestGenerateFromOrder_RenderFailureRollsBackRow(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}
	invoices := &fakeInvoiceRepo{}
	renderer := &fakeRenderer{err: fmt.Errorf("font boom")}
	storage := &fakeStorage{}
	svc := newService(orders, invoices, renderer, storage)

	_, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	})
	if err == nil {
		t.Fatal("expected render error")
	}
	if invoices.saved != nil {
		t.Fatal("SavePreparing must roll back the row when Render fails")
	}

	renderer.err = nil
	if _, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	}); err != nil {
		t.Fatalf("retry after render failure: %v", err)
	}
	if len(storage.saved) == 0 {
		t.Fatal("expected PDF stored on retry")
	}
}

func TestGenerateFromOrder_StoreFailureLeavesRowForRepair(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}
	invoices := &fakeInvoiceRepo{}
	storage := &failingStorage{err: fmt.Errorf("disk full")}
	svc := appInvoice.NewService(orders, invoices, &fakeRenderer{}, storage,
		event.NewBus(logger.NewWithWriter(io.Discard, "error")),
		logger.NewWithWriter(io.Discard, "error"),
	)

	_, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	})
	if err == nil {
		t.Fatal("expected store error")
	}
	if len(invoices.deleted) != 0 {
		t.Fatalf("must not delete row on store failure: %v", invoices.deleted)
	}
	if invoices.saved == nil {
		t.Fatal("row must remain for ensureIssued")
	}

	okStorage := &fakeStorage{}
	svc = appInvoice.NewService(orders, invoices, &fakeRenderer{}, okStorage,
		event.NewBus(logger.NewWithWriter(io.Discard, "error")),
		logger.NewWithWriter(io.Discard, "error"),
	)
	if _, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	}); err != nil {
		t.Fatalf("repair after store failure: %v", err)
	}
	if len(okStorage.saved) == 0 {
		t.Fatal("expected PDF stored on repair")
	}
}

func TestGenerateFromOrder_UnprobeableStorageErrors(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}
	existingInv, _ := domainInvoice.NewInvoice(
		"inv-1", "ord-1", "cust-1", "EUR",
		[]domainInvoice.Item{mustInvoiceItem(t)},
		shared.MustNewMoney(380, "EUR"),
	)
	existingInv.SetInvoiceNumberFromDB(1)
	invoices := &fakeInvoiceRepo{
		byOrder: map[string]*domainInvoice.Invoice{"ord-1": &existingInv},
		byID:    map[string]*domainInvoice.Invoice{"inv-1": &existingInv},
	}
	svc := appInvoice.NewService(orders, invoices, &fakeRenderer{}, probeBlindStorage{},
		event.NewBus(logger.NewWithWriter(io.Discard, "error")),
		logger.NewWithWriter(io.Discard, "error"),
	)
	_, err := svc.GenerateFromOrder(context.Background(), appInvoice.GenerateInput{
		OrderID:   "ord-1",
		TaxAmount: shared.MustNewMoney(380, "EUR"),
	})
	if err == nil || !strings.Contains(err.Error(), "cannot probe") {
		t.Fatalf("err = %v, want cannot probe", err)
	}
}

// probeBlindStorage implements media.Storage without Read or Head.
type probeBlindStorage struct{}

func (probeBlindStorage) Name() string                 { return "blind" }
func (probeBlindStorage) Save(string, io.Reader) error { return nil }
func (probeBlindStorage) Delete(string) error          { return nil }
func (probeBlindStorage) URL(string) string            { return "" }

func TestHandleOrderPaid_IssuesAndRepairs(t *testing.T) {
	ord := paidOrder(t)
	orders := &fakeOrderRepo{orders: map[string]*order.Order{"ord-1": ord}}
	invoices := &fakeInvoiceRepo{}
	storage := &fakeStorage{}
	log := logger.NewWithWriter(io.Discard, "error")
	bus := event.NewBus(log)
	var published int
	bus.On(domainInvoice.EventInvoiceCreated, func(context.Context, event.Event) error {
		published++
		return nil
	})
	svc := appInvoice.NewService(orders, invoices, &fakeRenderer{}, storage, bus, log)

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
	if published != 1 {
		t.Fatalf("published = %d", published)
	}

	for path := range storage.saved {
		delete(storage.saved, path)
	}
	renderer := &fakeRenderer{}
	svc = appInvoice.NewService(orders, invoices, renderer, storage, bus, log)
	if err := svc.HandleOrderPaid(context.Background(), evt); err != nil {
		t.Fatalf("second HandleOrderPaid repair: %v", err)
	}
	if !renderer.called {
		t.Fatal("expected PDF repair on second order.paid")
	}
	if published != 2 {
		t.Fatalf("published after repair = %d, want 2", published)
	}
}
