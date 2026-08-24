package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/interfaces/http/admin"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	returnsApp "github.com/akarso/shopanda/internal/application/returns"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/rbac"
	domainReturns "github.com/akarso/shopanda/internal/domain/returns"
	"github.com/akarso/shopanda/internal/domain/shared"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type returnHTTPReturnRepo struct {
	byID    map[string]*domainReturns.Return
	byOrder map[string][]domainReturns.Return
}

func (r *returnHTTPReturnRepo) Save(_ context.Context, ret *domainReturns.Return) error {
	cp := *ret
	r.byID[ret.ID] = &cp
	if r.byOrder == nil {
		r.byOrder = make(map[string][]domainReturns.Return)
	}
	r.byOrder[ret.OrderID] = append(r.byOrder[ret.OrderID], cp)
	return nil
}

func (r *returnHTTPReturnRepo) FindByID(_ context.Context, id string) (*domainReturns.Return, error) {
	return r.byID[id], nil
}

func (r *returnHTTPReturnRepo) FindByOrderID(_ context.Context, orderID string) ([]domainReturns.Return, error) {
	return r.byOrder[orderID], nil
}

func (r *returnHTTPReturnRepo) FindByCustomerID(_ context.Context, customerID string) ([]domainReturns.Return, error) {
	var out []domainReturns.Return
	for _, ret := range r.byID {
		if ret.CustomerID == customerID {
			out = append(out, *ret)
		}
	}
	return out, nil
}

func (r *returnHTTPReturnRepo) List(_ context.Context, offset, limit int) ([]domainReturns.Return, error) {
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

func (r *returnHTTPReturnRepo) Update(_ context.Context, ret *domainReturns.Return) error {
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

type returnHTTPOrderRepo struct {
	order *order.Order
}

func (r *returnHTTPOrderRepo) FindByID(_ context.Context, _ string) (*order.Order, error) {
	return r.order, nil
}
func (r *returnHTTPOrderRepo) FindByCustomerID(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (r *returnHTTPOrderRepo) FindByContactEmail(context.Context, string) ([]order.Order, error) {
	return nil, nil
}
func (r *returnHTTPOrderRepo) List(context.Context, int, int) ([]order.Order, error) { return nil, nil }
func (r *returnHTTPOrderRepo) Save(context.Context, *order.Order) error              { return nil }
func (r *returnHTTPOrderRepo) UpdateStatus(context.Context, *order.Order) error      { return nil }
func (r *returnHTTPOrderRepo) LinkToCustomer(context.Context, *order.Order) error    { return nil }
func (r *returnHTTPOrderRepo) LinkToCustomerByContactEmail(context.Context, string, string, time.Time) (int64, error) {
	return 0, nil
}
func (r *returnHTTPOrderRepo) ListPaidTaxSnapshots(context.Context, time.Time, time.Time) ([]order.TaxSnapshotRow, error) {
	return nil, nil
}

type returnHTTPStockRepo struct {
	qty map[string]int
}

func (r *returnHTTPStockRepo) GetStock(_ context.Context, variantID string) (inventory.StockEntry, error) {
	return inventory.StockEntry{VariantID: variantID, Quantity: r.qty[variantID]}, nil
}
func (r *returnHTTPStockRepo) SetStock(_ context.Context, entry *inventory.StockEntry) error {
	r.qty[entry.VariantID] = entry.Quantity
	return nil
}
func (r *returnHTTPStockRepo) SetStocks(context.Context, []inventory.StockEntry) error { return nil }
func (r *returnHTTPStockRepo) ListStock(context.Context, int, int) ([]inventory.StockEntry, error) {
	return nil, nil
}
func (r *returnHTTPStockRepo) ListInventory(context.Context, int, int, string) ([]inventory.InventoryListItem, error) {
	return nil, nil
}
func (r *returnHTTPStockRepo) GetInventoryItem(context.Context, string) (inventory.InventoryListItem, error) {
	return inventory.InventoryListItem{}, nil
}

type returnHTTPPaymentRepo struct{}

func (r *returnHTTPPaymentRepo) FindByID(context.Context, string) (*payment.Payment, error) {
	return nil, nil
}
func (r *returnHTTPPaymentRepo) FindByOrderID(context.Context, string) (*payment.Payment, error) {
	return nil, nil
}
func (r *returnHTTPPaymentRepo) Create(context.Context, *payment.Payment) error { return nil }
func (r *returnHTTPPaymentRepo) UpdateStatus(context.Context, *payment.Payment, time.Time) error {
	return nil
}
func (r *returnHTTPPaymentRepo) List(context.Context, payment.ListFilter) ([]payment.Payment, error) {
	return nil, nil
}

func returnHTTPPaidOrder(t *testing.T) *order.Order {
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

func returnAdminTestService(t *testing.T) *returnsApp.Service {
	t.Helper()
	return returnsApp.NewService(
		&returnHTTPReturnRepo{byID: make(map[string]*domainReturns.Return)},
		&returnHTTPOrderRepo{order: returnHTTPPaidOrder(t)},
		&returnHTTPStockRepo{qty: map[string]int{"v1": 5}},
		&returnHTTPPaymentRepo{},
		nil,
		event.NewBus(logger.NewWithWriter(io.Discard, "info")),
		logger.NewWithWriter(io.Discard, "info"),
	)
}

func TestReturnAdmin_ListAndApprove(t *testing.T) {
	svc := returnAdminTestService(t)
	ctx := context.Background()
	ret, err := svc.RequestReturn(ctx, "o1", "c1", "damaged", []returnsApp.RequestLine{{VariantID: "v1", Quantity: 1}})
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}

	auditor := adminapp.NewAuditor(logger.New("error"))
	h := admin.NewReturnAdminHandler(svc, auditor)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/returns", admin.RequirePermission(rbac.OrdersRead)(h.List()))
	mux.Handle("GET /api/v1/admin/returns/{returnId}", admin.RequirePermission(rbac.OrdersRead)(h.Get()))
	mux.Handle("POST /api/v1/admin/returns/{returnId}/approve", admin.RequirePermission(rbac.OrdersWrite)(h.Approve()))

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/returns?offset=0&limit=10", nil)
	listReq = testhelper.AdminRequest(listReq, "admin-1")
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body: %s", listRec.Code, listRec.Body.String())
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/returns/"+ret.ID+"/approve", nil)
	approveReq = testhelper.AdminRequest(approveReq, "admin-1")
	approveReq.SetPathValue("returnId", ret.ID)
	approveRec := httptest.NewRecorder()
	mux.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body: %s", approveRec.Code, approveRec.Body.String())
	}

	var envelope struct {
		Data struct {
			Return struct {
				Status string `json:"status"`
			} `json:"return"`
		} `json:"data"`
	}
	if err := json.Unmarshal(approveRec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envelope.Data.Return.Status != "approved" {
		t.Fatalf("status = %q, want approved", envelope.Data.Return.Status)
	}
}

func TestReturnAccount_RequestAndList(t *testing.T) {
	svc := returnAdminTestService(t)
	h := storefront.NewReturnAccountHandler(svc)
	mux := http.NewServeMux()
	requireAuth := storefront.RequireAuth()
	mux.Handle("POST /api/v1/orders/{orderId}/returns", requireAuth(h.Request()))
	mux.Handle("GET /api/v1/account/returns", requireAuth(h.List()))

	body := bytes.NewBufferString(`{"reason":"wrong size","lines":[{"variant_id":"v1","quantity":1}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/o1/returns", body)
	req = testhelper.CustomerRequest(req, "c1")
	req.SetPathValue("orderId", "o1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("request status = %d, body: %s", rec.Code, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/account/returns", nil)
	listReq = testhelper.CustomerRequest(listReq, "c1")
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body: %s", listRec.Code, listRec.Body.String())
	}
}

func TestReturnAdmin_ForbiddenWithoutPermission(t *testing.T) {
	svc := returnAdminTestService(t)
	h := admin.NewReturnAdminHandler(svc, adminapp.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/returns", admin.RequirePermission(rbac.OrdersRead)(h.List()))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/returns", nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
