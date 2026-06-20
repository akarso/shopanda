package core_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/core"
)

func TestRegister_MeilisearchEngineRegistersMeiliPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Search: config.SearchConfig{Engine: "meilisearch"},
		Queue:  config.QueueConfig{Driver: "postgres"},
		Cache:  config.CacheConfig{Driver: "postgres"},
	}
	core.Register(reg, cfg)

	var hasMeili, hasPostgresSearch bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/meilisearch-search":
			hasMeili = true
		case "core/postgres-search":
			hasPostgresSearch = true
		}
	}
	if !hasMeili {
		t.Fatal("expected core/meilisearch-search to be registered")
	}
	if hasPostgresSearch {
		t.Fatal("postgres search plugin should not register when search.engine=meilisearch")
	}
}

func TestRegister_PostgresEngineRegistersPostgresSearchOnly(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Search: config.SearchConfig{Engine: "postgres"},
		Queue:  config.QueueConfig{Driver: "postgres"},
		Cache:  config.CacheConfig{Driver: "postgres"},
	}
	core.Register(reg, cfg)

	for _, e := range reg.Entries() {
		if e.Name == "core/meilisearch-search" {
			t.Fatal("meilisearch plugin should not register when search.engine=postgres")
		}
	}
}

func TestRegister_ExplicitPostgresSearchOverridesMeilisearchEngine(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	enabled := true
	cfg := &config.Config{
		Search: config.SearchConfig{Engine: "meilisearch"},
		Plugins: config.PluginsConfig{
			Core: config.CorePluginsConfig{
				PostgresSearch: &enabled,
			},
		},
	}
	core.Register(reg, cfg)

	var hasMeili, hasPostgresSearch bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/meilisearch-search":
			hasMeili = true
		case "core/postgres-search":
			hasPostgresSearch = true
		}
	}
	if !hasPostgresSearch {
		t.Fatal("expected postgres search plugin when explicitly enabled")
	}
	if hasMeili {
		t.Fatal("meilisearch plugin must not register when postgres search wins")
	}
}

func TestRegister_AlwaysRegistersManualPaymentPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Search: config.SearchConfig{Engine: "postgres"},
	}
	core.Register(reg, cfg)

	var hasManual bool
	for _, e := range reg.Entries() {
		if e.Name == "core/manualpay" {
			hasManual = true
		}
	}
	if !hasManual {
		t.Fatal("expected core/manualpay to always register")
	}
}

func TestRegister_StripePaymentBeforeManualWhenEnabled(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Payment: config.PaymentConfig{
			Stripe: config.StripeConfig{Enabled: true},
		},
	}
	core.Register(reg, cfg)

	entries := reg.Entries()
	var stripeIdx, manualIdx = -1, -1
	for i, e := range entries {
		switch e.Name {
		case "core/stripe":
			stripeIdx = i
		case "core/manualpay":
			manualIdx = i
		}
	}
	if stripeIdx < 0 || manualIdx < 0 {
		t.Fatalf("entries = %v, want stripe and manual payment plugins", entries)
	}
	if stripeIdx > manualIdx {
		t.Fatal("stripe payment plugin must register before manualpay so Stripe can take the provider slot")
	}
}
