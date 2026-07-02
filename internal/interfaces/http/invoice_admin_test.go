package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainInvoice "github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/shared"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
)

type invoiceHTTPRepo struct {
	byID    map[string]*domainInvoice.Invoice
	byOrder map[string]*domainInvoice.Invoice
}

func (r *invoiceHTTPRepo) FindByID(_ context.Context, id string) (*domainInvoice.Invoice, error) {
	if inv, ok := r.byID[id]; ok {
		cp := *inv
		return &cp, nil
	}
	return nil, nil
}

func (r *invoiceHTTPRepo) FindByOrderID(_ context.Context, orderID string) (*domainInvoice.Invoice, error) {
	if inv, ok := r.byOrder[orderID]; ok {
		cp := *inv
		return &cp, nil
	}
	return nil, nil
}

func (r *invoiceHTTPRepo) Save(context.Context, *domainInvoice.Invoice) error { return nil }

type invoiceHTTPOrderRepo struct {
	orders map[string]*order.Order
}

func (r *invoiceHTTPOrderRepo) FindByID(_ context.Context, id string) (*order.Order, error) {
	return r.orders[id], nil
}
func (r *invoiceHTTPOrderRepo) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (r *invoiceHTTPOrderRepo) FindByContactEmail(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (r *invoiceHTTPOrderRepo) List(context.Context, int, int) ([]order.Order, error) {
	return nil, nil
}
func (r *invoiceHTTPOrderRepo) Save(context.Context, *order.Order) error { return nil }
func (r *invoiceHTTPOrderRepo) UpdateStatus(context.Context, *order.Order) error { return nil }
func (r *invoiceHTTPOrderRepo) LinkToCustomer(context.Context, *order.Order) error { return nil }
func (r *invoiceHTTPOrderRepo) LinkToCustomerByContactEmail(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}
func (r *invoiceHTTPOrderRepo) ListPaidTaxSnapshots(context.Context, time.Time, time.Time) ([]order.TaxSnapshotRow, error) {
	return nil, nil
}

type stubInvoicePDFRenderer struct {
	data []byte
}

func (s *stubInvoicePDFRenderer) Render(domainInvoice.Invoice) ([]byte, error) {
	if len(s.data) > 0 {
		return s.data, nil
	}
	return []byte("%PDF-stub"), nil
}

func sampleHTTPInvoice(t *testing.T, invoiceID, orderID string, number int64) domainInvoice.Invoice {
	t.Helper()
	price, err := shared.NewMoney(1000, "EUR")
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	item, err := domainInvoice.NewItem("variant-1", "SKU-1", "Widget", 1, price)
	if err != nil {
		t.Fatalf("item: %v", err)
	}
	tax, err := shared.NewMoney(200, "EUR")
	if err != nil {
		t.Fatalf("tax: %v", err)
	}
	inv, err := domainInvoice.NewInvoice(invoiceID, orderID, "cust-1", "EUR", []domainInvoice.Item{item}, tax)
	if err != nil {
		t.Fatalf("invoice: %v", err)
	}
	inv.SetInvoiceNumberFromDB(number)
	if err := inv.SetStatusFromDB(string(domainInvoice.InvoiceStatusIssued)); err != nil {
		t.Fatalf("status: %v", err)
	}
	if err := inv.SetItemsFromDB([]domainInvoice.Item{item}); err != nil {
		t.Fatalf("items: %v", err)
	}
	return inv
}

func sampleHTTPOrder(orderID string) *order.Order {
	price, _ := shared.NewMoney(1000, "EUR")
	item, _ := order.NewItem("variant-1", "SKU-1", "Widget", 1, price)
	ord, _ := order.NewOrder(orderID, "cust-1", "cust@example.com", "EUR", []order.Item{item})
	_ = ord.SetStatusFromDB(string(order.OrderStatusPaid))
	return &ord
}

func invoiceAdminMux(t *testing.T, invRepo *invoiceHTTPRepo, ordRepo *invoiceHTTPOrderRepo, renderer *stubInvoicePDFRenderer) *http.ServeMux {
	t.Helper()
	h := shophttp.NewInvoiceAdminHandler(invRepo, ordRepo, renderer, nil)
	withAdminContext := shophttp.AdminContextMiddleware()
	requireInvoicesRead := shophttp.RequirePermission(rbac.InvoicesRead)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/orders/{orderId}/invoices", withAdminContext(requireInvoicesRead(h.ListByOrder())))
	mux.Handle("GET /api/v1/admin/invoices/{invoiceId}/pdf", withAdminContext(requireInvoicesRead(h.DownloadPDF())))
	return mux
}

func TestInvoiceAdminHandler_ListByOrder_Empty(t *testing.T) {
	ordRepo := &invoiceHTTPOrderRepo{orders: map[string]*order.Order{
		"order-1": sampleHTTPOrder("order-1"),
	}}
	mux := invoiceAdminMux(t, &invoiceHTTPRepo{byID: map[string]*domainInvoice.Invoice{}, byOrder: map[string]*domainInvoice.Invoice{}}, ordRepo, &stubInvoicePDFRenderer{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders/order-1/invoices", nil)
	req.SetPathValue("orderId", "order-1")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Invoices []map[string]interface{} `json:"invoices"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Data.Invoices) != 0 {
		t.Fatalf("invoices len = %d, want 0", len(body.Data.Invoices))
	}
}

func TestInvoiceAdminHandler_ListByOrder_WithInvoice(t *testing.T) {
	inv := sampleHTTPInvoice(t, "inv-1", "order-1", 1001)
	ordRepo := &invoiceHTTPOrderRepo{orders: map[string]*order.Order{
		"order-1": sampleHTTPOrder("order-1"),
	}}
	invRepo := &invoiceHTTPRepo{
		byID:    map[string]*domainInvoice.Invoice{"inv-1": &inv},
		byOrder: map[string]*domainInvoice.Invoice{"order-1": &inv},
	}
	mux := invoiceAdminMux(t, invRepo, ordRepo, &stubInvoicePDFRenderer{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders/order-1/invoices", nil)
	req.SetPathValue("orderId", "order-1")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"invoice_number":1001`) {
		t.Fatalf("expected invoice_number in body: %s", rec.Body.String())
	}
}

func TestInvoiceAdminHandler_ListByOrder_OrderNotFound(t *testing.T) {
	mux := invoiceAdminMux(t, &invoiceHTTPRepo{byID: map[string]*domainInvoice.Invoice{}, byOrder: map[string]*domainInvoice.Invoice{}}, &invoiceHTTPOrderRepo{orders: map[string]*order.Order{}}, &stubInvoicePDFRenderer{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders/missing/invoices", nil)
	req.SetPathValue("orderId", "missing")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestInvoiceAdminHandler_ListByOrder_Forbidden(t *testing.T) {
	mux := invoiceAdminMux(t, &invoiceHTTPRepo{byID: map[string]*domainInvoice.Invoice{}, byOrder: map[string]*domainInvoice.Invoice{}}, &invoiceHTTPOrderRepo{orders: map[string]*order.Order{}}, &stubInvoicePDFRenderer{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders/order-1/invoices", nil)
	req.SetPathValue("orderId", "order-1")
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestInvoiceAdminHandler_DownloadPDF(t *testing.T) {
	inv := sampleHTTPInvoice(t, "inv-1", "order-1", 42)
	invRepo := &invoiceHTTPRepo{
		byID:    map[string]*domainInvoice.Invoice{"inv-1": &inv},
		byOrder: map[string]*domainInvoice.Invoice{"order-1": &inv},
	}
	renderer := &stubInvoicePDFRenderer{data: []byte("%PDF-test-bytes")}
	mux := invoiceAdminMux(t, invRepo, &invoiceHTTPOrderRepo{orders: map[string]*order.Order{}}, renderer)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/invoices/inv-1/pdf", nil)
	req.SetPathValue("invoiceId", "inv-1")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if ct != "application/pdf" {
		t.Fatalf("content-type = %q, want application/pdf", ct)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "invoice-42.pdf") {
		t.Fatalf("content-disposition = %q", rec.Header().Get("Content-Disposition"))
	}
	if got := rec.Body.String(); got != "%PDF-test-bytes" {
		t.Fatalf("body = %q, want %%PDF-test-bytes", got)
	}
}

func TestInvoiceAdminHandler_DownloadPDF_NotFound(t *testing.T) {
	mux := invoiceAdminMux(t, &invoiceHTTPRepo{byID: map[string]*domainInvoice.Invoice{}, byOrder: map[string]*domainInvoice.Invoice{}}, &invoiceHTTPOrderRepo{orders: map[string]*order.Order{}}, &stubInvoicePDFRenderer{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/invoices/missing/pdf", nil)
	req.SetPathValue("invoiceId", "missing")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// Ensure invoice summary uses RFC3339 timestamps.
func TestInvoiceAdminHandler_ListByOrder_CreatedAtFormat(t *testing.T) {
	base := sampleHTTPInvoice(t, "inv-1", "order-1", 1)
	created := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	inv := domainInvoice.NewInvoiceFromDB(
		base.ID(), base.InvoiceNumber(), base.OrderID(), base.CustomerID(), base.Currency(),
		base.SubtotalAmount(), base.TaxAmount(), base.TotalAmount(), created,
	)
	_ = inv.SetStatusFromDB(string(domainInvoice.InvoiceStatusIssued))
	_ = inv.SetItemsFromDB(base.Items())

	ordRepo := &invoiceHTTPOrderRepo{orders: map[string]*order.Order{"order-1": sampleHTTPOrder("order-1")}}
	invRepo := &invoiceHTTPRepo{
		byID:    map[string]*domainInvoice.Invoice{"inv-1": &inv},
		byOrder: map[string]*domainInvoice.Invoice{"order-1": &inv},
	}
	mux := invoiceAdminMux(t, invRepo, ordRepo, &stubInvoicePDFRenderer{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders/order-1/invoices", nil)
	req.SetPathValue("orderId", "order-1")
	req = testhelper.AdminRequest(req, "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "2024-06-01T12:00:00Z") {
		t.Fatalf("expected RFC3339 created_at, got: %s", rec.Body.String())
	}
}
