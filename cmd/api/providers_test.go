package main

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestResolveSearchEngine_MeilisearchWithoutProvider(t *testing.T) {
	cfg := &config.Config{Search: config.SearchConfig{Engine: "meilisearch"}}
	app := &plugin.App{Config: cfg}

	_, err := resolveSearchEngine(app, nil, cfg)
	if err == nil {
		t.Fatal("resolveSearchEngine() expected error when meilisearch provider missing")
	}
}
