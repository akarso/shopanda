package main

import (
	"context"
	"database/sql"
	"io"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/example"

	_ "github.com/jackc/pgx/v5/stdlib"
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

type seedCLINoDBPlugin struct{}

func (seedCLINoDBPlugin) Name() string { return "test/seednodb" }

func (seedCLINoDBPlugin) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{pluginsdk.CapCatalogSeed}
}

func (seedCLINoDBPlugin) Init(h pluginsdk.Host) error {
	h.Require(pluginsdk.CapCatalogSeed)
	sdk := pluginsdk.NewHost(h, "test/seednodb")
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:seed-nodb",
		Description: "Seed Need without RequiresDB — must fail closed",
		RequiresDB:  false,
		Run:         func(pluginsdk.CLIContext, []string) error { return nil },
	})
	return nil
}

// integrationRequireFailPlugin Requires CapIntegrationStock then fails Init
// (mirrors warehousedemo without syncer). Leaves CapIntegration* in required.
type integrationRequireFailPlugin struct{}

func (integrationRequireFailPlugin) Name() string { return "test/intfail" }

func (integrationRequireFailPlugin) Init(h pluginsdk.Host) error {
	h.Require(pluginsdk.CapIntegrationStock)
	return errIntegrationUninit
}

type integrationUninitError struct{}

func (integrationUninitError) Error() string { return "integration stock syncer not configured" }

var errIntegrationUninit = integrationUninitError{}

func stubSeedBoot() *plugin.Bootstrap {
	return &plugin.Bootstrap{
		Products:     seedBootProducts{},
		Variants:     seedBootVariants{},
		Prices:       seedBootPrices{},
		PriceHistory: seedBootHistory{},
		Stock:        seedBootStock{},
	}
}

func TestRunPluginCLICommand_RequiresDBBindsCatalogSeed(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	probe := &seedCLIProbePlugin{}
	reg.Register(pluginsdk.Adapt(probe))

	app := &plugin.App{Logger: log, Config: &config.Config{}}
	preparePermissionRegistry(app)
	summary := reg.InitAll(app)
	freezePermissionRegistry(app)
	if err := plugin.InitSummaryError(summary); err != nil || summary.Failed != 0 {
		t.Fatalf("Init: summary=%+v err=%v", summary, err)
	}

	prevOpen, prevBind := pluginCLIOpenDB, pluginCLIBindPorts
	t.Cleanup(func() {
		pluginCLIOpenDB, pluginCLIBindPorts = prevOpen, prevBind
	})
	pluginCLIOpenDB = func(*config.Config) (*sql.DB, error) {
		// sql.Open does not connect until use; Close is safe.
		return sql.Open("pgx", "host=/dev/null")
	}
	pluginCLIBindPorts = func(a *plugin.App, _ *sql.DB, cmd cli.Command) error {
		return bindPluginCLIPortsFromBoot(a, stubSeedBoot(), cmd)
	}

	ran, err := runPluginCLICommand(&config.Config{}, log, func() (*pluginCLIRuntime, error) {
		return &pluginCLIRuntime{Registry: reg, App: app}, nil
	}, "test:seed-probe", nil)
	if err != nil {
		t.Fatalf("runPluginCLICommand: %v", err)
	}
	if !ran {
		t.Fatal("expected ran=true")
	}
	if !probe.saw {
		t.Fatal("Host.CatalogSeed must succeed after RequiresDB auto-bind via runPluginCLICommand")
	}
	if _, ok := app.CatalogSeedPort(); !ok {
		t.Fatal("CatalogSeedPort must stay bound on App")
	}
}

func TestRunPluginCLICommand_SeedNeedWithoutRequiresDBFailsClosed(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	reg.Register(pluginsdk.Adapt(seedCLINoDBPlugin{}))

	app := &plugin.App{Logger: log, Config: &config.Config{}}
	preparePermissionRegistry(app)
	summary := reg.InitAll(app)
	freezePermissionRegistry(app)
	if summary.Failed != 0 {
		t.Fatalf("InitAll = %+v", summary)
	}

	ran, err := runPluginCLICommand(&config.Config{}, log, func() (*pluginCLIRuntime, error) {
		return &pluginCLIRuntime{Registry: reg, App: app}, nil
	}, "test:seed-nodb", nil)
	if !ran || err == nil || !strings.Contains(err.Error(), "RequiresDB is false") {
		t.Fatalf("ran=%v err=%v, want RequiresDB fail-closed", ran, err)
	}
}

func TestRunPluginCLICommand_FailedIntegrationInitDoesNotBreakSeedAssert(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	probe := &seedCLIProbePlugin{}
	reg.Register(pluginsdk.Adapt(integrationRequireFailPlugin{}))
	reg.Register(pluginsdk.Adapt(probe))

	app := &plugin.App{Logger: log, Config: &config.Config{}}
	preparePermissionRegistry(app)
	summary := reg.InitAll(app)
	freezePermissionRegistry(app)
	if summary.Failed < 1 {
		t.Fatalf("expected integration plugin Failed, got %+v", summary)
	}
	if !app.CapabilityNeeded(plugin.CapIntegrationStock) {
		t.Fatal("failed Init must leave CapIntegrationStock in required (regression premise)")
	}

	prevOpen, prevBind := pluginCLIOpenDB, pluginCLIBindPorts
	t.Cleanup(func() {
		pluginCLIOpenDB, pluginCLIBindPorts = prevOpen, prevBind
	})
	pluginCLIOpenDB = func(*config.Config) (*sql.DB, error) {
		return sql.Open("pgx", "host=/dev/null")
	}
	pluginCLIBindPorts = func(a *plugin.App, _ *sql.DB, cmd cli.Command) error {
		return bindPluginCLIPortsFromBoot(a, stubSeedBoot(), cmd)
	}

	ran, err := runPluginCLICommand(&config.Config{}, log, func() (*pluginCLIRuntime, error) {
		return &pluginCLIRuntime{Registry: reg, App: app}, nil
	}, "test:seed-probe", nil)
	if err != nil {
		t.Fatalf("seed CLI must not fail on CapIntegration* left by failed Init: %v", err)
	}
	if !ran || !probe.saw {
		t.Fatalf("ran=%v saw=%v", ran, probe.saw)
	}
}

func TestBindPluginCLIPorts_OnlyOwnerGetsCatalogSeed(t *testing.T) {
	app := &plugin.App{}
	app.RecordNeedsFor("test/seedcli", plugin.CapCatalogSeed)
	app.RecordNeedsFor("other/plugin", plugin.CapCatalog)

	boot := stubSeedBoot()
	// Peer RequiresDB command — owner did not declare CapCatalogSeed.
	if err := bindPluginCLIPortsFromBoot(app, boot, cli.Command{
		Name: "other:cmd", Plugin: "other/plugin", RequiresDB: true,
	}); err != nil {
		t.Fatalf("bind for other: %v", err)
	}
	if _, ok := app.CatalogSeedPort(); ok {
		t.Fatal("must not bind CapCatalogSeed for a command whose owner did not Need it")
	}
}

func TestBindPluginCLIPorts_NilAppFailsClosed(t *testing.T) {
	err := bindPluginCLIPortsFromBoot(nil, stubSeedBoot(), cli.Command{Name: "x:y", RequiresDB: true})
	if err == nil || !strings.Contains(err.Error(), "app is nil") {
		t.Fatalf("err = %v, want app is nil", err)
	}
}
