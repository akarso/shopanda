package warehousedemo_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/plugins/warehousedemo"
)

type stubStockSyncer struct {
	last  []extapi.StockLevelUpdate
	calls int
	err   error
}

func (s *stubStockSyncer) UpsertBySKU(_ context.Context, updates []extapi.StockLevelUpdate) (extapi.StockSyncResult, error) {
	s.calls++
	s.last = append([]extapi.StockLevelUpdate(nil), updates...)
	if s.err != nil {
		return extapi.StockSyncResult{}, s.err
	}
	return extapi.StockSyncResult{Updated: len(updates)}, nil
}

func testApp(cfg *config.Config, syncer extapi.IntegrationStockSyncer) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	if syncer != nil {
		app.SetIntegrationStockSyncer(syncer)
	}
	return app
}

func initWarehouseDemoPlugin(t *testing.T, baseURL string) (*plugin.App, *stubStockSyncer) {
	t.Helper()
	stub := &stubStockSyncer{}
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			WarehouseDemo: config.WarehouseDemoPluginConfig{
				Enabled:          true,
				WarehouseBaseURL: baseURL,
			},
		},
	}
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(warehousedemo.New())
	app := testApp(cfg, stub)
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v", summary)
	}
	return app, stub
}

func TestPlugin_Name(t *testing.T) {
	if got := warehousedemo.New().Name(); got != "warehousedemo/reference" {
		t.Fatalf("Name() = %q", got)
	}
}

func TestPlugin_Init_DisabledReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			WarehouseDemo: config.WarehouseDemoPluginConfig{Enabled: false},
		},
	}
	if err := warehousedemo.New().Init(testApp(cfg, &stubStockSyncer{})); err == nil {
		t.Fatal("Init() expected error when disabled")
	}
}

func TestPlugin_Init_RegistersStockSyncJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stock" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"stock": []map[string]interface{}{
				{"sku": "SKU-1", "quantity": 10},
				{"sku": "SKU-2", "quantity": 5},
			},
		})
	}))
	t.Cleanup(srv.Close)

	app, stub := initWarehouseDemoPlugin(t, srv.URL)
	jobs := app.SyncJobs()
	if len(jobs) != 1 || jobs[0].JobType != "integration.sync.warehousedemo.warehouse.stock" {
		t.Fatalf("SyncJobs() = %+v", jobs)
	}

	handler := jobs[0].Job.Handler
	if err := handler(context.Background(), extapi.SyncJobContext{JobID: "job-1"}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if stub.calls != 1 || len(stub.last) != 2 || stub.last[0].SKU != "SKU-1" || stub.last[0].Quantity != 10 {
		t.Fatalf("syncer calls = %d last = %+v", stub.calls, stub.last)
	}
}

func TestStockSyncHandler_WarehouseHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			WarehouseDemo: config.WarehouseDemoPluginConfig{
				Enabled:          true,
				WarehouseBaseURL: srv.URL,
			},
		},
	}
	stub := &stubStockSyncer{}
	if err := warehousedemo.New().Init(testApp(cfg, stub)); err != nil {
		t.Fatalf("Init: %v", err)
	}
	handler := warehousedemo.NewStockSyncHandler(nil, stub, nil)
	if err := handler(context.Background(), extapi.SyncJobContext{}); err == nil {
		t.Fatal("expected error for nil client")
	}
}
