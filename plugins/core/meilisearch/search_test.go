package meilisearch_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/core/meilisearch"
)

func testApp(cfg *config.Config) *plugin.App {
	log := logger.NewWithWriter(io.Discard, "error")
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
}

func TestSearchPlugin_Name(t *testing.T) {
	if got := meilisearch.NewSearchPlugin().Name(); got != "core/meilisearch-search" {
		t.Fatalf("Name() = %q, want core/meilisearch-search", got)
	}
}

func TestSearchPlugin_Init_EmptyHost(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Engine: "meilisearch",
			Meilisearch: config.MeilisearchConfig{
				Index: "products",
			},
		},
	}
	err := meilisearch.NewSearchPlugin().Init(testApp(cfg))
	if err == nil {
		t.Fatal("Init() expected error for empty host")
	}
}

func TestSearchPlugin_Init_EmptyIndex(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Engine: "meilisearch",
			Meilisearch: config.MeilisearchConfig{
				Host: "http://localhost:7700",
			},
		},
	}
	err := meilisearch.NewSearchPlugin().Init(testApp(cfg))
	if err == nil {
		t.Fatal("Init() expected error for empty index")
	}
}

func TestSearchPlugin_Init_WrongEngine(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Engine: "postgres",
			Meilisearch: config.MeilisearchConfig{
				Host:  "http://localhost:7700",
				Index: "products",
			},
		},
	}
	err := meilisearch.NewSearchPlugin().Init(testApp(cfg))
	if err == nil {
		t.Fatal("Init() expected error when search.engine is not meilisearch")
	}
}

func TestSearchPlugin_Init_ClientFailure(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Engine: "meilisearch",
			Meilisearch: config.MeilisearchConfig{
				Host:  "http://127.0.0.1:1",
				Index: "products",
			},
		},
	}
	err := meilisearch.NewSearchPlugin().Init(testApp(cfg))
	if err == nil {
		t.Fatal("Init() expected error when Meilisearch is unreachable")
	}
}

func TestSearchPlugin_Init_RegistersProvider(t *testing.T) {
	cfg := &config.Config{
		Search: config.SearchConfig{
			Engine: "meilisearch",
			Meilisearch: config.MeilisearchConfig{
				Host:  "http://127.0.0.1:1",
				Index: "products",
			},
		},
	}
	app := testApp(cfg)
	reg := plugin.NewRegistry(app.Logger)
	reg.Register(meilisearch.NewSearchPlugin())

	summary := reg.InitAll(app)
	if summary.Initialized != 0 || summary.Failed != 1 {
		t.Fatalf("InitAll summary = %+v, want failed plugin when Meili unreachable", summary)
	}
	if _, ok := app.SearchProvider(); ok {
		t.Fatal("SearchProvider() should not be set when plugin init fails")
	}
}
