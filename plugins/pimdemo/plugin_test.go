package pimdemo_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/application/composition"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/pimdemo"
)

func testApp(cfg *config.Config) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
}

func initPimDemoPlugin(t *testing.T, endpoint string) *plugin.App {
	t.Helper()
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			PimDemo: config.PimDemoPluginConfig{
				Enabled:            true,
				PimGraphQLEndpoint: endpoint,
				CacheTTL:           "1h",
			},
		},
	}
	reg := plugin.NewRegistry(logger.NewWithWriter(io.Discard, "error"))
	reg.Register(pimdemo.New())
	app := testApp(cfg)
	if summary := reg.InitAll(app); summary.Failed > 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v", summary)
	}
	return app
}

func TestPlugin_Name(t *testing.T) {
	if got := pimdemo.New().Name(); got != "pimdemo/reference" {
		t.Fatalf("Name() = %q", got)
	}
}

func TestPlugin_Init_DisabledReturnsError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			PimDemo: config.PimDemoPluginConfig{Enabled: false},
		},
	}
	if err := pimdemo.New().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected error when disabled")
	}
}

func TestPlugin_Init_RegistersPDPStep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"product":{"marketing_title":"Title","marketing_description":"Desc","specs":[]}}}`))
	}))
	t.Cleanup(srv.Close)

	app := initPimDemoPlugin(t, srv.URL)
	steps := app.CompositionSteps("pdp")
	if len(steps) != 1 {
		t.Fatalf("CompositionSteps(pdp) = %+v", steps)
	}
	step, ok := steps[0].(composition.Step[composition.ProductContext])
	if !ok || step.Name() != pimdemo.StepName {
		t.Fatalf("step = %#v ok=%v", steps[0], ok)
	}
}

func TestPlugin_Init_InvalidCacheTTL(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			PimDemo: config.PimDemoPluginConfig{
				Enabled:            true,
				PimGraphQLEndpoint: "http://localhost/graphql",
				CacheTTL:           "not-a-duration",
			},
		},
	}
	if err := pimdemo.New().Init(testApp(cfg)); err == nil {
		t.Fatal("Init() expected cache_ttl error")
	}
}
