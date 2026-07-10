package ports_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/application/ports"
	"github.com/akarso/shopanda/internal/infrastructure/manualpay"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type stubSearch struct{}

func testConfig() *config.Config {
	return &config.Config{
		Search: config.SearchConfig{Engine: "postgres"},
		Cache:  config.CacheConfig{Driver: "postgres"},
		Queue:  config.QueueConfig{Driver: "postgres"},
		Media:  config.MediaConfig{Storage: "local"},
	}
}

func TestBuildSnapshot_PlannedPorts(t *testing.T) {
	snap := ports.BuildSnapshot(&plugin.App{}, testConfig())
	byName := indexPorts(snap.Ports)

	tax := byName["tax"]
	if tax.Status != ports.StatusPlanned {
		t.Fatalf("tax status = %q, want planned", tax.Status)
	}
	if tax.RegisterAPI != "RegisterTaxCalculator" {
		t.Fatalf("tax register API = %q", tax.RegisterAPI)
	}
}

func TestBuildSnapshot_CoreDefaults(t *testing.T) {
	snap := ports.BuildSnapshot(&plugin.App{}, testConfig())
	byName := indexPorts(snap.Ports)

	search := byName["search"]
	if search.Status != ports.StatusCoreDefault {
		t.Fatalf("search status = %q, want core_default", search.Status)
	}
	if search.Driver != "postgres" {
		t.Fatalf("search driver = %q", search.Driver)
	}
	if search.Implementation != "postgres.SearchEngine" {
		t.Fatalf("search implementation = %q", search.Implementation)
	}

	media := byName["media"]
	if media.Implementation != "localfs.Storage" {
		t.Fatalf("media implementation = %q", media.Implementation)
	}

	paymentPort := byName["payment"]
	if paymentPort.Status != ports.StatusCoreDefault {
		t.Fatalf("payment status = %q", paymentPort.Status)
	}
	if len(paymentPort.Providers) != 1 || paymentPort.Providers[0].Key != "manual" {
		t.Fatalf("payment providers = %+v", paymentPort.Providers)
	}
}

func TestBuildSnapshot_PluginSearchProvider(t *testing.T) {
	app := &plugin.App{}
	app.RegisterSearchProvider(stubSearch{})

	cfg := testConfig()
	cfg.Search.Engine = "meilisearch"
	snap := ports.BuildSnapshot(app, cfg)

	search := indexPorts(snap.Ports)["search"]
	if search.Status != ports.StatusActive {
		t.Fatalf("search status = %q, want active", search.Status)
	}
	if search.Source != "plugin" {
		t.Fatalf("search source = %q", search.Source)
	}
	if search.Implementation != "ports_test.stubSearch" {
		t.Fatalf("search implementation = %q", search.Implementation)
	}
}

func TestBuildSnapshot_PaymentRegistry(t *testing.T) {
	app := &plugin.App{}
	app.RegisterPaymentProvider(manualpay.NewProvider())

	snap := ports.BuildSnapshot(app, testConfig())
	paymentPort := indexPorts(snap.Ports)["payment"]
	if len(paymentPort.Providers) != 1 {
		t.Fatalf("providers = %+v", paymentPort.Providers)
	}
	if paymentPort.Providers[0].Source != "core" {
		t.Fatalf("provider source = %q", paymentPort.Providers[0].Source)
	}
}

func TestBuildSnapshot_UnconfiguredRedisCache(t *testing.T) {
	cfg := testConfig()
	cfg.Cache.Driver = "redis"
	snap := ports.BuildSnapshot(&plugin.App{}, cfg)

	cache := indexPorts(snap.Ports)["cache"]
	if cache.Status != ports.StatusUnconfigured {
		t.Fatalf("cache status = %q, want unconfigured", cache.Status)
	}
	if cache.Driver != "redis" {
		t.Fatalf("cache driver = %q", cache.Driver)
	}
}

func TestCatalog_IncludesShippedPorts(t *testing.T) {
	names := make(map[string]struct{})
	for _, entry := range ports.Catalog() {
		names[entry.Name] = struct{}{}
	}
	for _, want := range []string{"search", "cache", "queue", "payment", "media", "tax"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("catalog missing %q", want)
		}
	}
}

func indexPorts(portsList []ports.ActivePort) map[string]ports.ActivePort {
	out := make(map[string]ports.ActivePort, len(portsList))
	for _, p := range portsList {
		out[p.Name] = p
	}
	return out
}
