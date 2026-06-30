package main

import (
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func TestRunPluginCLICommand_UnknownReturnsFalse(t *testing.T) {
	cfg := &config.Config{}
	log := logger.New("error")
	ran, err := runPluginCLICommand(cfg, log, "does-not-exist", nil)
	if err != nil {
		t.Fatalf("runPluginCLICommand: %v", err)
	}
	if ran {
		t.Fatal("expected ran=false for unknown command")
	}
}

func TestPluginCLIHelpLines_IncludesExampleWhenEnabled(t *testing.T) {
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			Example: config.ExamplePluginConfig{Enabled: true},
		},
	}
	log := logger.New("error")
	lines := pluginCLIHelpLines(cfg, log)
	if len(lines) < 2 {
		t.Fatalf("help lines = %v, want plugin section", lines)
	}
	found := false
	for _, line := range lines {
		if line == "  example:ping         Verify example plugin CLI registration" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("help lines = %v, want example:ping entry", lines)
	}
}
