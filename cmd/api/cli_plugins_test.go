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

// stubPluginCLIDeps opens a non-connecting *sql.DB and binds via stubSeedBoot.
func stubPluginCLIDeps(t *testing.T) pluginCLIDeps {
	t.Helper()
	return pluginCLIDeps{
		OpenDB: func(*config.Config) (*sql.DB, error) {
			return sql.Open("pgx", "host=/dev/null")
		},
		Bind: func(a *plugin.App, _ *sql.DB, cmd cli.Command) error {
			return bindPluginCLIPortsFromBoot(a, stubSeedBoot(), cmd)
		},
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

// peerRequiresDBPlugin does not Need CapCatalogSeed but registers RequiresDB.
type peerRequiresDBPlugin struct {
	host pluginsdk.Host
	saw  bool
}

func (p *peerRequiresDBPlugin) Name() string { return "test/peerdb" }

func (p *peerRequiresDBPlugin) Init(h pluginsdk.Host) error {
	p.host = h
	sdk := pluginsdk.NewHost(h, p.Name())
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:peer-db",
		Description: "RequiresDB without CapCatalogSeed",
		RequiresDB:  true,
		Run: func(pluginsdk.CLIContext, []string) error {
			_, ok := p.host.CatalogSeed()
			if ok {
				return errCatalogSeedUnbound // reuse: unexpected bind
			}
			p.saw = true
			return nil
		},
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

func initSeedCLIRuntime(t *testing.T, plugs ...pluginsdk.Plugin) (*plugin.Registry, *plugin.App) {
	t.Helper()
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	for _, p := range plugs {
		reg.Register(pluginsdk.Adapt(p))
	}
	app := &plugin.App{Logger: log, Config: &config.Config{}}
	preparePermissionRegistry(app)
	summary := reg.InitAll(app)
	freezePermissionRegistry(app)
	if err := plugin.InitSummaryError(summary); err != nil {
		t.Fatalf("InitSummaryError: %v", err)
	}
	return reg, app
}

func TestRunPluginCLICommand_RequiresDBBindsCatalogSeed(t *testing.T) {
	probe := &seedCLIProbePlugin{}
	reg, app := initSeedCLIRuntime(t, probe)

	cmd, ok := reg.CLIRegistry().Get("test:seed-probe")
	if !ok {
		t.Fatal("command not registered")
	}
	if cmd.Plugin != probe.Name() {
		t.Fatalf("cmd.Plugin = %q, want %q (SDK RegisterCommand attribution)", cmd.Plugin, probe.Name())
	}

	ran, err := runPluginCLICommandWithDeps(&config.Config{}, logger.NewWithWriter(io.Discard, "error"),
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "test:seed-probe", nil, stubPluginCLIDeps(t))
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

func TestRunPluginCLICommand_PeerRequiresDBDoesNotBindSeed(t *testing.T) {
	probe := &seedCLIProbePlugin{}
	peer := &peerRequiresDBPlugin{}
	reg, app := initSeedCLIRuntime(t, probe, peer)

	ran, err := runPluginCLICommandWithDeps(&config.Config{}, logger.NewWithWriter(io.Discard, "error"),
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "test:peer-db", nil, stubPluginCLIDeps(t))
	if err != nil {
		t.Fatalf("peer RequiresDB: %v", err)
	}
	if !ran || !peer.saw {
		t.Fatalf("ran=%v saw=%v", ran, peer.saw)
	}
	if _, ok := app.CatalogSeedPort(); ok {
		t.Fatal("peer RequiresDB must not bind CapCatalogSeed when seed peer Needs it")
	}
}

func TestRunPluginCLICommand_SeedNeedWithoutRequiresDBFailsClosed(t *testing.T) {
	reg, app := initSeedCLIRuntime(t, seedCLINoDBPlugin{})

	ran, err := runPluginCLICommand(&config.Config{}, logger.NewWithWriter(io.Discard, "error"),
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "test:seed-nodb", nil)
	if !ran || err == nil || !strings.Contains(err.Error(), "RequiresDB is false") {
		t.Fatalf("ran=%v err=%v, want RequiresDB fail-closed", ran, err)
	}
}

func TestRunPluginCLICommand_FailedIntegrationInitDoesNotBreakSeedAssert(t *testing.T) {
	probe := &seedCLIProbePlugin{}
	reg, app := initSeedCLIRuntime(t, &integrationRequireFailPlugin{}, probe)
	// initSeedCLIRuntime ignores Failed count — re-check premise.
	if !app.CapabilityDeclaredBy(plugin.CapIntegrationStock, "test/intfail") {
		t.Fatal("failed Init must leave CapIntegrationStock attributed to intfail")
	}

	ran, err := runPluginCLICommandWithDeps(&config.Config{}, logger.NewWithWriter(io.Discard, "error"),
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "test:seed-probe", nil, stubPluginCLIDeps(t))
	if err != nil {
		t.Fatalf("seed CLI must not fail on CapIntegration* left by failed Init: %v", err)
	}
	if !ran || !probe.saw {
		t.Fatalf("ran=%v saw=%v", ran, probe.saw)
	}
}

func TestRunPluginCLICommand_NilAppRequiresDBFailsClosed(t *testing.T) {
	probe := &seedCLIProbePlugin{}
	reg, _ := initSeedCLIRuntime(t, probe)

	ran, err := runPluginCLICommandWithDeps(&config.Config{}, logger.NewWithWriter(io.Discard, "error"),
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: nil}, nil
		}, "test:seed-probe", nil, stubPluginCLIDeps(t))
	if !ran || err == nil || !strings.Contains(err.Error(), "app is nil") {
		t.Fatalf("ran=%v err=%v, want app is nil", ran, err)
	}
}

func TestRunPluginCLICommand_EmptyPluginOwnerFailsClosedWhenSeedNeeded(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	app := &plugin.App{Logger: log, Config: &config.Config{}}
	preparePermissionRegistry(app)
	app.RecordNeedsFor("ghost/seed", plugin.CapCatalogSeed)
	reg.CLIRegistry().Register(cli.Command{
		Name:        "ghost:seed",
		Description: "RequiresDB with empty Plugin",
		RequiresDB:  true,
		Plugin:      "", // explicit empty owner
		Run:         func(cli.Context, []string) error { return nil },
	})
	freezePermissionRegistry(app)

	ran, err := runPluginCLICommandWithDeps(&config.Config{}, log,
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "ghost:seed", nil, stubPluginCLIDeps(t))
	if !ran || err == nil || !strings.Contains(err.Error(), "empty Plugin owner") {
		t.Fatalf("ran=%v err=%v, want empty Plugin fail-closed", ran, err)
	}
}
