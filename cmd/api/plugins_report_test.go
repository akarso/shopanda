package main

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestRunPluginsReport_TextOutput(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			CartDemo: config.CartDemoPluginConfig{Enabled: true},
		},
	}
	log := logger.New("error")
	if err := runPluginsReport(cfg, log, nil); err != nil {
		t.Fatalf("runPluginsReport: %v", err)
	}
}

func TestRunPluginsReport_JSONFlag(t *testing.T) {
	cfg := &config.Config{}
	log := logger.New("error")
	if err := runPluginsReport(cfg, log, []string{"--json"}); err != nil {
		t.Fatalf("runPluginsReport --json: %v", err)
	}
}

func TestRunPluginsReport_UnknownFlag(t *testing.T) {
	cfg := &config.Config{}
	log := logger.New("error")
	err := runPluginsReport(cfg, log, []string{"--nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("err = %v", err)
	}
}
