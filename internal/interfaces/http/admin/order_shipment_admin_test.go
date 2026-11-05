package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/interfaces/http/admin"

	shippingapp "github.com/akarso/shopanda/internal/application/shipping"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/shared"
	domainShipping "github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type memShipmentRepo struct {
	mu sync.Mutex
	sh *domainShipping.Shipment
}

func (m *memShipmentRepo) FindByID(context.Context, string) (*domainShipping.Shipment, error) {
	return nil, nil
}
func (m *memShipmentRepo) FindByOrderID(_ context.Context, orderID string) (*domainShipping.Shipment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sh == nil || m.sh.OrderID != orderID {
		return nil, nil
	}
	cp := *m.sh
	return &cp, nil
}
func (m *memShipmentRepo) Create(context.Context, *domainShipping.Shipment) error { return nil }
func (m *memShipmentRepo) UpdateStatus(_ context.Context, s *domainShipping.Shipment, prev time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sh == nil || !m.sh.UpdatedAt.Equal(prev) {
		return context.Canceled
	}
	cp := *s
	m.sh = &cp
	return nil
}

func shipmentAdminSetup(t *testing.T) (*http.ServeMux, *memShipmentRepo) {
	t.Helper()
	pending, err := domainShipping.NewShipment("ship-1", "ord-1", domainShipping.MethodFlatRate, shared.MustNewMoney(500, "EUR"))
	if err != nil {
		t.Fatal(err)
	}
	repo := &memShipmentRepo{sh: &pending}
	svc := shippingapp.NewStatusService(repo)
	orders := newStubOrderRepo()
	handler := admin.NewOrderAdminHandler(orders, logger.New("error"), nil).WithShipmentStatusService(svc)
	requireAdmin := admin.RequireRole(identity.RoleAdmin)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/orders/{orderId}/shipment", requireAdmin(handler.GetShipment()))
	mux.Handle("PUT /api/v1/admin/orders/{orderId}/shipment", requireAdmin(handler.UpdateShipment()))
	return mux, repo
}

func TestOrderAdmin_GetAndShipShipment(t *testing.T) {
	mux, _ := shipmentAdminSetup(t)

	getReq := testhelper.AdminRequest(httptest.NewRequest("GET", "/api/v1/admin/orders/ord-1/shipment", nil), "admin-1")
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", getRec.Code, getRec.Body.String())
	}

	putReq := testhelper.AdminRequest(httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1/shipment",
		strings.NewReader(`{"status":"shipped","tracking_number":"TRACK-9"}`)), "admin-1")
	putRec := httptest.NewRecorder()
	mux.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusAccepted {
		t.Fatalf("PUT status = %d body=%s", putRec.Code, putRec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(putRec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	data, _ := body["data"].(map[string]interface{})
	sh, _ := data["shipment"].(map[string]interface{})
	if sh["status"] != "shipped" || sh["tracking_number"] != "TRACK-9" {
		t.Fatalf("shipment = %#v", sh)
	}
}

func TestOrderAdmin_ShipmentNotFound(t *testing.T) {
	mux, _ := shipmentAdminSetup(t)
	req := testhelper.AdminRequest(httptest.NewRequest("GET", "/api/v1/admin/orders/missing/shipment", nil), "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestOrderAdmin_DeliverAndCancelShipment(t *testing.T) {
	mux, _ := shipmentAdminSetup(t)
	shipReq := testhelper.AdminRequest(httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1/shipment",
		strings.NewReader(`{"status":"shipped","tracking_number":"T1"}`)), "admin-1")
	shipRec := httptest.NewRecorder()
	mux.ServeHTTP(shipRec, shipReq)
	if shipRec.Code != http.StatusAccepted {
		t.Fatalf("ship = %d %s", shipRec.Code, shipRec.Body.String())
	}
	delReq := testhelper.AdminRequest(httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1/shipment",
		strings.NewReader(`{"status":"delivered"}`)), "admin-1")
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusAccepted {
		t.Fatalf("deliver = %d %s", delRec.Code, delRec.Body.String())
	}

	mux2, _ := shipmentAdminSetup(t)
	cancelReq := testhelper.AdminRequest(httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1/shipment",
		strings.NewReader(`{"status":"cancelled"}`)), "admin-1")
	cancelRec := httptest.NewRecorder()
	mux2.ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusAccepted {
		t.Fatalf("cancel = %d %s", cancelRec.Code, cancelRec.Body.String())
	}
}

func TestOrderAdmin_ShipmentInvalidTransition(t *testing.T) {
	mux, _ := shipmentAdminSetup(t)
	req := testhelper.AdminRequest(httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1/shipment",
		strings.NewReader(`{"status":"delivered"}`)), "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	assertAdminShipmentError(t, rec, http.StatusUnprocessableEntity, "validation")
}

func TestOrderAdmin_ShipmentTrackingAmendRejected(t *testing.T) {
	mux, _ := shipmentAdminSetup(t)
	shipReq := testhelper.AdminRequest(httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1/shipment",
		strings.NewReader(`{"status":"shipped","tracking_number":"T1","provider_ref":"ref-a"}`)), "admin-1")
	mux.ServeHTTP(httptest.NewRecorder(), shipReq)
	amend := testhelper.AdminRequest(httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1/shipment",
		strings.NewReader(`{"status":"shipped","tracking_number":"T2"}`)), "admin-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, amend)
	assertAdminShipmentError(t, rec, http.StatusUnprocessableEntity, "validation")
	if !strings.Contains(rec.Body.String(), "tracking amend not supported") {
		t.Fatalf("body = %s", rec.Body.String())
	}

	// Same tracking with omitted provider_ref is an idempotent no-op.
	retry := testhelper.AdminRequest(httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1/shipment",
		strings.NewReader(`{"status":"shipped","tracking_number":"T1"}`)), "admin-1")
	retryRec := httptest.NewRecorder()
	mux.ServeHTTP(retryRec, retry)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("omitted provider_ref retry = %d %s", retryRec.Code, retryRec.Body.String())
	}
}

func assertAdminShipmentError(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), wantStatus)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	errBody, _ := body["error"].(map[string]interface{})
	if errBody == nil || errBody["code"] != wantCode {
		t.Fatalf("error = %#v, want code %q", errBody, wantCode)
	}
}
