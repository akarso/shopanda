package main

import (
	"io"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestBootstrapExportRegistry_DependsOnError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			B2B: config.B2BPluginConfig{Enabled: true, LicenseKey: "DEV-test"},
			DependsOn: map[string][]string{
				"shopanda/b2b": {"shopanda/disabled-plugin"},
			},
		},
	}
	log := logger.NewWithWriter(io.Discard, "error")
	_, err := bootstrapExportRegistry(cfg, log)
	if err == nil || !strings.Contains(err.Error(), "depends_on") {
		t.Fatalf("got %v, want depends_on error", err)
	}
}

func TestBootstrapImportRegistry_DependsOnError(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			B2B: config.B2BPluginConfig{Enabled: true, LicenseKey: "DEV-test"},
			DependsOn: map[string][]string{
				"shopanda/b2b": {"shopanda/disabled-plugin"},
			},
		},
	}
	log := logger.NewWithWriter(io.Discard, "error")
	_, err := bootstrapImportRegistry(cfg, log)
	if err == nil || !strings.Contains(err.Error(), "depends_on") {
		t.Fatalf("got %v, want depends_on error", err)
	}
}
