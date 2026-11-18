package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/example"
)

func cliContextForTest(t *testing.T) cli.Context {
	t.Helper()
	return cli.Context{
		Ctx:    context.Background(),
		Logger: logger.NewWithWriter(io.Discard, "error"),
	}
}

func TestRunPluginCLICommand_UnknownReturnsFalse(t *testing.T) {
	cfg := &config.Config{}
	log := logger.New("error")
	ran, err := runPluginCLICommand(cfg, log, func() (*pluginCLIRuntime, error) {
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
	registryFn := func() (*pluginCLIRuntime, error) {
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

// seedCLIProbePlugin Needs CapCatalogSeed and registers a RequiresDB command
// that resolves Host.CatalogSeed() — mirrors a product seed CLI without
// editing cli_plugins.go.
type seedCLIProbePlugin struct {
	host pluginsdk.Host
	saw  bool
}

func (p *seedCLIProbePlugin) Name() string { return "test/seedcli" }

func (p *seedCLIProbePlugin) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{pluginsdk.CapCatalogSeed}
}

func (p *seedCLIProbePlugin) Init(h pluginsdk.Host) error {
	p.host = h
	h.Require(pluginsdk.CapCatalogSeed)
	sdk := pluginsdk.NewHost(h, p.Name())
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:seed-probe",
		Description: "Probe CatalogSeedPort after auto-bind",
		RequiresDB:  true,
		Run: func(pluginsdk.CLIContext, []string) error {
			port, ok := p.host.CatalogSeed()
			if !ok || port == nil {
				return errCatalogSeedUnbound
			}
			p.saw = true
			return nil
		},
	})
	return nil
}

type catalogSeedUnboundError struct{}

func (catalogSeedUnboundError) Error() string { return "catalog seed port unbound" }

var errCatalogSeedUnbound = catalogSeedUnboundError{}

func TestBindPluginCLIPorts_AutoBindsCatalogSeed(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	probe := &seedCLIProbePlugin{}
	reg.Register(pluginsdk.Adapt(probe))

	app := &plugin.App{Logger: log, Config: &config.Config{}}
	preparePermissionRegistry(app)
	summary := reg.InitAll(app)
	freezePermissionRegistry(app)
	if err := plugin.InitSummaryError(summary); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if summary.Failed != 0 {
		t.Fatalf("InitAll = %+v", summary)
	}

	boot := &plugin.Bootstrap{
		Products:     seedBootProducts{},
		Variants:     seedBootVariants{},
		Prices:       seedBootPrices{},
		PriceHistory: seedBootHistory{},
		Stock:        seedBootStock{},
	}
	if err := bindPluginCLIPortsFromBoot(app, boot); err != nil {
		t.Fatalf("bindPluginCLIPortsFromBoot: %v", err)
	}
	if _, ok := app.CatalogSeedPort(); !ok {
		t.Fatal("CapCatalogSeed must be auto-bound for plugin CLI when Needed")
	}

	cmd, ok := reg.CLIRegistry().Get("test:seed-probe")
	if !ok {
		t.Fatal("command not registered")
	}
	if !cmd.RequiresDB {
		t.Fatal("RequiresDB must propagate from pluginsdk.CLICommand")
	}
	if err := cmd.Run(cliContextForTest(t), nil); err != nil {
		t.Fatalf("command Run: %v (Host.CatalogSeed must work after auto-bind)", err)
	}
	if !probe.saw {
		t.Fatal("command did not see CatalogSeedPort")
	}
}

func TestBindPluginCLIPorts_NoSeedNeedSkipsWritePort(t *testing.T) {
	app := &plugin.App{}
	app.RecordNeeds(plugin.CapCatalog) // not CapCatalogSeed
	boot := &plugin.Bootstrap{
		Products:     seedBootProducts{},
		Variants:     seedBootVariants{},
		Prices:       seedBootPrices{},
		PriceHistory: seedBootHistory{},
		Stock:        seedBootStock{},
	}
	if err := bindPluginCLIPortsFromBoot(app, boot); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if _, ok := app.CatalogSeedPort(); ok {
		t.Fatal("must not bind CapCatalogSeed when not Needed")
	}
}
