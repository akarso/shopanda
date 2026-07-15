package integrationdemo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func testApp(cfg *config.Config, updater extapi.IntegrationOrderStatusUpdater) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	if updater != nil {
		app.SetIntegrationOrderStatusUpdater(updater)
	}
	app.SetIntegrationIdempotencyStore(integrationhttp.NewMemoryIdempotencyStore())
	return app
}

func initIntegrationDemoPlugin(t *testing.T) (*plugin.App, *stubOrderStatusUpdater) {
	t.Helper()
	stub := &stubOrderStatusUpdater{}
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
	app := testApp(cfg, stub)
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v", summary)
	}
	return app, stub
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
	if err := integrationdemo.New().Init(testApp(cfg, &stubOrderStatusUpdater{})); err == nil {
		t.Fatal("Init() expected error when disabled")
	}
}

func TestPlugin_Init_RegistersSecureOrderStatusRoute(t *testing.T) {
	app, stub := initIntegrationDemoPlugin(t)
	routes := app.PublicRoutes()
	if len(routes) != 1 || routes[0].Pattern != "POST /api/v1/integrations/integrationdemo/order-status" {
		t.Fatalf("routes = %+v", routes)
	}

	body := []byte(`{"order_id":"ord-1","status":"CONFIRMED","external_ref":"ERP-100"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/integrationdemo/order-status", bytes.NewReader(body))
	req.Header.Set(extapi.IntegrationHeaderAPIKey, "erp-secret")
	req.Header.Set(extapi.IntegrationHeaderIdempotencyKey, "idem-1")
	rec := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(rec, req)
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
	routes[0].Handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusAccepted || stub.calls != 1 {
		t.Fatalf("replay status = %d calls = %d header = %q", rec2.Code, stub.calls, rec2.Header().Get("X-Idempotency-Replayed"))
	}
}

func TestPlugin_Init_IDocPayloadAccepted(t *testing.T) {
	app, stub := initIntegrationDemoPlugin(t)
	routes := app.PublicRoutes()
	body := []byte(`{"E1ORDSTAT":{"order_id":"ord-2","status":"CONFIRMED","VBELN":"90001"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/integrationdemo/order-status", bytes.NewReader(body))
	req.Header.Set(extapi.IntegrationHeaderAPIKey, "erp-secret")
	rec := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(rec, req)
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
	stub := &stubOrderStatusUpdater{}
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
	app := testApp(cfg, stub)
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v", summary)
	}
	routes := app.PublicRoutes()
	body := []byte(`{"order_id":"ord-1","status":"CONFIRMED"}`)
	now := time.Unix(1_700_000_000, 0)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/integrationdemo/order-status", bytes.NewReader(body))
	integrationhttp.SignRequest(req, body, "erp-secret", "hmac-secret", now.Unix(), "nonce-hmac")
	rec := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
