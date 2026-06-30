package main

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/example"
)

func TestRunPluginCLICommand_UnknownReturnsFalse(t *testing.T) {
	cfg := &config.Config{}
	log := logger.New("error")
	ran, err := runPluginCLICommand(cfg, log, func() *plugin.Registry {
		return bootstrapPluginCLIRegistry(cfg, log)
	}, "does-not-exist", nil)
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
	registryFn := func() *plugin.Registry {
		return bootstrapPluginCLIRegistry(cfg, log)
	}
	lines := pluginCLIHelpLines(registryFn)
	if len(lines) < 2 {
		t.Fatalf("help lines = %v, want plugin section", lines)
	}
	found := false
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == example.CommandPing && strings.Contains(line, "Verify example plugin CLI registration") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("help lines = %v, want example:ping entry", lines)
	}
}
