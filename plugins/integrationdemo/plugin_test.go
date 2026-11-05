package integrationdemo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/integrationhttp"
	"github.com/akarso/shopanda/plugins/integrationdemo"
)

type stubOrderStatusUpdater struct {
	calls int
	last  extapi.IntegrationOrderStatusResult
	err   error
}

func (s *stubOrderStatusUpdater) ApplyOrderStatus(_ context.Context, orderID, status string) (extapi.IntegrationOrderStatusResult, error) {
	s.calls++
	if s.err != nil {
		return extapi.IntegrationOrderStatusResult{}, s.err
	}
	s.last = extapi.IntegrationOrderStatusResult{
		OrderID:        orderID,
		Status:         "confirmed",
		PreviousStatus: "pending",
		Changed:        true,
	}
	if status == "confirmed" || status == "CONFIRMED" {
		return s.last, nil
	}
	return extapi.IntegrationOrderStatusResult{}, extapi.ErrIntegrationOrderInvalidStatus
}

type stubShipmentStatusUpdater struct {
	calls int
	last  extapi.IntegrationShipmentStatusResult
	err   error
}

func (s *stubShipmentStatusUpdater) ApplyShipmentStatus(_ context.Context, orderID, status, tracking, providerRef string) (extapi.IntegrationShipmentStatusResult, error) {
	s.calls++
	if s.err != nil {
		return extapi.IntegrationShipmentStatusResult{}, s.err
	}
	s.last = extapi.IntegrationShipmentStatusResult{
		OrderID:        orderID,
		ShipmentID:     "ship-1",
		Status:         "shipped",
		PreviousStatus: "pending",
		Changed:        true,
		TrackingNumber: tracking,
		ProviderRef:    providerRef,
	}
	switch status {
	case "shipped", "SHIPPED", "delivered", "DELIVERED", "cancelled", "CANCELLED":
		if status == "delivered" || status == "DELIVERED" {
			s.last.Status = "delivered"
			s.last.PreviousStatus = "shipped"
		}
		if status == "cancelled" || status == "CANCELLED" {
			s.last.Status = "cancelled"
		}
		return s.last, nil
	}
	return extapi.IntegrationShipmentStatusResult{}, extapi.ErrIntegrationShipmentInvalidStatus
}

func testApp(cfg *config.Config, orderUpdater extapi.IntegrationOrderStatusUpdater, shipmentUpdater extapi.IntegrationShipmentStatusUpdater) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	if orderUpdater != nil {
		app.SetIntegrationOrderStatusUpdater(orderUpdater)
	}
	if shipmentUpdater != nil {
		app.SetIntegrationShipmentStatusUpdater(shipmentUpdater)
	}
	app.SetIntegrationIdempotencyStore(integrationhttp.NewMemoryIdempotencyStore())
	return app
}

func routeByPattern(routes []plugin.PublicRoute, pattern string) (plugin.PublicRoute, bool) {
	for _, r := range routes {
		if r.Pattern == pattern {
			return r, true
		}
	}
	return plugin.PublicRoute{}, false
}

func initIntegrationDemoPlugin(t *testing.T) (*plugin.App, *stubOrderStatusUpdater, *stubShipmentStatusUpdater) {
	t.Helper()
	orderStub := &stubOrderStatusUpdater{}
	shipmentStub := &stubShipmentStatusUpdater{}
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			IntegrationDemo: config.IntegrationDemoPluginConfig{
				Enabled:           true,
				IntegrationAPIKey: "erp-secret",
			},
		},
	}
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(integrationdemo.New())
	app := testApp(cfg, orderStub, shipmentStub)
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v", summary)
	}
	return app, orderStub, shipmentStub
}

func TestPlugin_Name(t *testing.T) {
	if got := integrationdemo.New().Name(); got != "integrationdemo/reference" {
		t.Fatalf("Name() = %q", got)
	}
}

func TestPlugin_Init_DisabledReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			IntegrationDemo: config.IntegrationDemoPluginConfig{Enabled: false},
		},
	}
	if err := integrationdemo.New().Init(testApp(cfg, &stubOrderStatusUpdater{}, &stubShipmentStatusUpdater{})); err == nil {
		t.Fatal("Init() expected error when disabled")
	}
}

func TestPlugin_Init_RegistersSecureOrderStatusRoute(t *testing.T) {
	app, stub, _ := initIntegrationDemoPlugin(t)
	route, ok := routeByPattern(app.PublicRoutes(), "POST /api/v1/integrations/integrationdemo/order-status")
	if !ok {
		t.Fatalf("routes = %+v", app.PublicRoutes())
	}

	body := []byte(`{"order_id":"ord-1","status":"CONFIRMED","external_ref":"ERP-100"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/integrationdemo/order-status", bytes.NewReader(body))
	req.Header.Set(extapi.IntegrationHeaderAPIKey, "erp-secret")
	req.Header.Set(extapi.IntegrationHeaderIdempotencyKey, "idem-1")
	rec := httptest.NewRecorder()
	route.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if stub.calls != 1 {
		t.Fatalf("updater calls = %d", stub.calls)
	}

	var resp struct {
		OrderStatus extapi.IntegrationOrderStatusResult `json:"order_status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OrderStatus.OrderID != "ord-1" || resp.OrderStatus.ExternalRef != "ERP-100" {
		t.Fatalf("response = %+v", resp.OrderStatus)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/integrationdemo/order-status", bytes.NewReader(body))
	req2.Header.Set(extapi.IntegrationHeaderAPIKey, "erp-secret")
	req2.Header.Set(extapi.IntegrationHeaderIdempotencyKey, "idem-1")
	route.Handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusAccepted || stub.calls != 1 {
		t.Fatalf("replay status = %d calls = %d header = %q", rec2.Code, stub.calls, rec2.Header().Get("X-Idempotency-Replayed"))
	}
}

func TestPlugin_Init_RegistersSecureShipmentStatusRoute(t *testing.T) {
	app, _, stub := initIntegrationDemoPlugin(t)
	route, ok := routeByPattern(app.PublicRoutes(), "POST /api/v1/integrations/integrationdemo/shipment-status")
	if !ok {
		t.Fatalf("routes = %+v", app.PublicRoutes())
	}

	body := []byte(`{"order_id":"ord-1","status":"SHIPPED","tracking_number":"TRACK-1","external_ref":"WH-9"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/integrationdemo/shipment-status", bytes.NewReader(body))
	req.Header.Set(extapi.IntegrationHeaderAPIKey, "erp-secret")
	req.Header.Set(extapi.IntegrationHeaderIdempotencyKey, "ship-idem-1")
	rec := httptest.NewRecorder()
	route.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if stub.calls != 1 || stub.last.TrackingNumber != "TRACK-1" {
		t.Fatalf("stub = %+v calls=%d", stub.last, stub.calls)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/integrationdemo/shipment-status", bytes.NewReader(body))
	req2.Header.Set(extapi.IntegrationHeaderAPIKey, "erp-secret")
	req2.Header.Set(extapi.IntegrationHeaderIdempotencyKey, "ship-idem-1")
	route.Handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusAccepted || stub.calls != 1 {
		t.Fatalf("replay status = %d calls = %d", rec2.Code, stub.calls)
	}
}

func TestShipmentStatusHandler_NotFound(t *testing.T) {
	handler := integrationdemo.NewShipmentStatusHandler(&stubShipmentStatusUpdater{err: extapi.ErrIntegrationShipmentNotFound}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{"order_id":"missing","status":"SHIPPED","tracking_number":"T"}`)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestShipmentStatusHandler_MissingTracking(t *testing.T) {
	handler := integrationdemo.NewShipmentStatusHandler(&stubShipmentStatusUpdater{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{"order_id":"ord-1","status":"SHIPPED"}`)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), extapi.IntegrationErrorShipmentInvalidPayload) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestShipmentStatusHandler_Conflict(t *testing.T) {
	handler := integrationdemo.NewShipmentStatusHandler(&stubShipmentStatusUpdater{err: extapi.ErrIntegrationShipmentConflict}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{"order_id":"ord-1","status":"SHIPPED","tracking_number":"T"}`)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestShipmentStatusHandler_Deliver(t *testing.T) {
	stub := &stubShipmentStatusUpdater{}
	handler := integrationdemo.NewShipmentStatusHandler(stub, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{"order_id":"ord-1","status":"DELIVERED"}`)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted || stub.last.Status != "delivered" {
		t.Fatalf("status = %d stub=%+v", rec.Code, stub.last)
	}
}

func TestPlugin_Init_IDocPayloadAccepted(t *testing.T) {
	app, stub, _ := initIntegrationDemoPlugin(t)
	route, ok := routeByPattern(app.PublicRoutes(), "POST /api/v1/integrations/integrationdemo/order-status")
	if !ok {
		t.Fatal("order-status route missing")
	}
	body := []byte(`{"E1ORDSTAT":{"order_id":"ord-2","status":"CONFIRMED","VBELN":"90001"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/integrationdemo/order-status", bytes.NewReader(body))
	req.Header.Set(extapi.IntegrationHeaderAPIKey, "erp-secret")
	rec := httptest.NewRecorder()
	route.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted || stub.calls != 1 {
		t.Fatalf("status = %d calls = %d", rec.Code, stub.calls)
	}
}

func TestOrderStatusHandler_InvalidPayload(t *testing.T) {
	handler := integrationdemo.NewOrderStatusHandler(&stubOrderStatusUpdater{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{"status":"CONFIRMED"}`)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestOrderStatusHandler_OrderNotFound(t *testing.T) {
	handler := integrationdemo.NewOrderStatusHandler(&stubOrderStatusUpdater{err: extapi.ErrIntegrationOrderNotFound}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(`{"order_id":"missing","status":"CONFIRMED"}`)))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestOrderStatusPayload_Normalize(t *testing.T) {
	flatID, flatStatus, flatRef, err := (integrationdemo.OrderStatusPayload{
		OrderID: "ord-1", Status: "confirmed", ExternalRef: "ERP-1",
	}).Normalize()
	if err != nil || flatID != "ord-1" || flatStatus != "confirmed" || flatRef != "ERP-1" {
		t.Fatalf("flat normalize = (%q,%q,%q,%v)", flatID, flatStatus, flatRef, err)
	}

	idocID, idocStatus, idocRef, err := (integrationdemo.OrderStatusPayload{
		E1ORDSTAT: &integrationdemo.IDocOrderStatus{OrderID: "ord-2", Status: "PAID", VBELN: "90001"},
	}).Normalize()
	if err != nil || idocID != "ord-2" || idocStatus != "PAID" || idocRef != "90001" {
		t.Fatalf("idoc normalize = (%q,%q,%q,%v)", idocID, idocStatus, idocRef, err)
	}
}

func TestPlugin_Init_RequiresHMACWhenConfigured(t *testing.T) {
	orderStub := &stubOrderStatusUpdater{}
	shipmentStub := &stubShipmentStatusUpdater{}
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			IntegrationDemo: config.IntegrationDemoPluginConfig{
				Enabled:               true,
				IntegrationAPIKey:     "erp-secret",
				IntegrationHMACSecret: "hmac-secret",
			},
		},
	}
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(integrationdemo.New())
	app := testApp(cfg, orderStub, shipmentStub)
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v", summary)
	}
	route, ok := routeByPattern(app.PublicRoutes(), "POST /api/v1/integrations/integrationdemo/order-status")
	if !ok {
		t.Fatal("order-status route missing")
	}
	body := []byte(`{"order_id":"ord-1","status":"CONFIRMED"}`)
	// Sign with wall-clock now so the request stays inside the 5m freshness window.
	now := time.Now()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/integrationdemo/order-status", bytes.NewReader(body))
	integrationhttp.SignRequest(req, body, "erp-secret", "hmac-secret", now.Unix(), "nonce-hmac")
	rec := httptest.NewRecorder()
	route.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
