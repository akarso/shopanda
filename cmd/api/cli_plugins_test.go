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

// seedPluginWithEval registers a CapCatalogSeed seed command and a sibling
// RequiresDB:false command (Metal Dogs metaldogs:eval shape).
type seedPluginWithEval struct {
	seedSaw bool
	evalSaw bool
}

func (p *seedPluginWithEval) Name() string { return "test/seed+eval" }

func (p *seedPluginWithEval) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{pluginsdk.CapCatalogSeed}
}

func (p *seedPluginWithEval) Init(h pluginsdk.Host) error {
	h.Require(pluginsdk.CapCatalogSeed)
	host := h
	sdk := pluginsdk.NewHost(h, p.Name())
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:seed-write",
		Description: "Seed with CapCatalogSeed",
		RequiresDB:  true,
		Needs:       []pluginsdk.Capability{pluginsdk.CapCatalogSeed},
		Run: func(pluginsdk.CLIContext, []string) error {
			if _, ok := host.CatalogSeed(); !ok {
				return errCatalogSeedUnbound
			}
			p.seedSaw = true
			return nil
		},
	})
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:eval-nodb",
		Description: "Non-DB sibling must run despite plugin CapCatalogSeed",
		RequiresDB:  false,
		Run: func(pluginsdk.CLIContext, []string) error {
			p.evalSaw = true
			return nil
		},
	})
	return nil
}

type cmdNeedsSeedNoDBPlugin struct{}

func (cmdNeedsSeedNoDBPlugin) Name() string { return "test/cmdseednodb" }

func (cmdNeedsSeedNoDBPlugin) Init(h pluginsdk.Host) error {
	sdk := pluginsdk.NewHost(h, "test/cmdseednodb")
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:cmd-seed-nodb",
		Description: "Command Needs CapCatalogSeed without RequiresDB — fail closed",
		RequiresDB:  false,
		Needs:       []pluginsdk.Capability{pluginsdk.CapCatalogSeed},
		Run:         func(pluginsdk.CLIContext, []string) error { return nil },
	})
	return nil
}

// peerRequiresDBPlugin does not Need CapCatalogSeed but registers RequiresDB.
// Bind absence is asserted via App.CatalogSeedPort() in the test (Host getters
// deny undeclared caps even if the port were wrongly bound).
type peerRequiresDBPlugin struct {
	saw bool
}

func (p *peerRequiresDBPlugin) Name() string { return "test/peerdb" }

func (p *peerRequiresDBPlugin) Init(h pluginsdk.Host) error {
	sdk := pluginsdk.NewHost(h, p.Name())
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:peer-db",
		Description: "RequiresDB without CapCatalogSeed",
		RequiresDB:  true,
		Run: func(pluginsdk.CLIContext, []string) error {
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

	if !app.CapabilityDeclaredBy(plugin.CapCatalogSeed, probe.Name()) {
		t.Fatal("premise: seed peer must Need CapCatalogSeed")
	}
	peerCmd, ok := reg.CLIRegistry().Get("test:peer-db")
	if !ok || peerCmd.Plugin != peer.Name() {
		t.Fatalf("peer cmd Plugin = %q, want %q", peerCmd.Plugin, peer.Name())
	}

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
	if port, ok := app.CatalogSeedPort(); ok || port != nil {
		t.Fatalf("CatalogSeedPort() = (%v, %v): peer RequiresDB must leave write port unbound despite seed peer Need", port, ok)
	}
}

func TestRunPluginCLICommand_SeedPluginNonDBSiblingRuns(t *testing.T) {
	p := &seedPluginWithEval{}
	reg, app := initSeedCLIRuntime(t, p)
	log := logger.NewWithWriter(io.Discard, "error")
	rt := func() (*pluginCLIRuntime, error) {
		return &pluginCLIRuntime{Registry: reg, App: app}, nil
	}

	ran, err := runPluginCLICommand(cfgEmpty(), log, rt, "test:eval-nodb", nil)
	if err != nil || !ran || !p.evalSaw {
		t.Fatalf("eval: ran=%v err=%v saw=%v (plugin CapCatalogSeed must not block RequiresDB:false)", ran, err, p.evalSaw)
	}

	ran, err = runPluginCLICommandWithDeps(cfgEmpty(), log, rt, "test:seed-write", nil, stubPluginCLIDeps(t))
	if err != nil || !ran || !p.seedSaw {
		t.Fatalf("seed: ran=%v err=%v saw=%v", ran, err, p.seedSaw)
	}
}

func TestRunPluginCLICommand_CommandNeedsSeedWithoutRequiresDBFailsClosed(t *testing.T) {
	reg, app := initSeedCLIRuntime(t, cmdNeedsSeedNoDBPlugin{})

	ran, err := runPluginCLICommand(cfgEmpty(), logger.NewWithWriter(io.Discard, "error"),
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "test:cmd-seed-nodb", nil)
	if !ran || err == nil || !strings.Contains(err.Error(), "RequiresDB is false") {
		t.Fatalf("ran=%v err=%v, want command Needs CapCatalogSeed fail-closed", ran, err)
	}
}

func cfgEmpty() *config.Config { return &config.Config{} }

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

func TestRunPluginCLICommand_EmptyPluginOwnerFailsClosedOnCommandNeedsSeed(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	app := &plugin.App{Logger: log, Config: &config.Config{}}
	preparePermissionRegistry(app)
	// No plugin-level CapCatalogSeed — command Needs alone must still fail closed.
	reg.CLIRegistry().Register(cli.Command{
		Name:        "ghost:cmd-seed",
		Description: "Empty owner + command Needs CapCatalogSeed",
		RequiresDB:  true,
		Needs:       []string{string(plugin.CapCatalogSeed)},
		Plugin:      "",
		Run:         func(cli.Context, []string) error { return nil },
	})
	freezePermissionRegistry(app)

	ran, err := runPluginCLICommandWithDeps(&config.Config{}, log,
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "ghost:cmd-seed", nil, stubPluginCLIDeps(t))
	if !ran || err == nil || !strings.Contains(err.Error(), "empty Plugin owner") {
		t.Fatalf("ran=%v err=%v, want empty Plugin fail-closed on command Needs", ran, err)
	}
}

func TestRunPluginCLICommand_EmptyNeedsCompatBindsSiblingRequiresDB(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	// Fresh App per command — bindProductPorts is once-per-App.
	seed := &seedPluginTwoRequiresDB{}
	reg, app := initSeedCLIRuntime(t, seed)
	ran, err := runPluginCLICommandWithDeps(cfgEmpty(), log,
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "test:seed-empty-needs", nil, stubPluginCLIDeps(t))
	if err != nil || !ran || !seed.seedSaw {
		t.Fatalf("seed empty Needs: ran=%v err=%v saw=%v", ran, err, seed.seedSaw)
	}

	sib := &seedPluginTwoRequiresDB{}
	reg2, app2 := initSeedCLIRuntime(t, sib)
	ran, err = runPluginCLICommandWithDeps(cfgEmpty(), log,
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg2, App: app2}, nil
		}, "test:other-db-empty-needs", nil, stubPluginCLIDeps(t))
	if err != nil || !ran || !sib.otherSaw {
		t.Fatalf("sibling empty Needs: ran=%v err=%v saw=%v (compat still binds CatalogSeed)", ran, err, sib.otherSaw)
	}
}

func TestRunPluginCLICommand_CommandNeedsSeedWithoutPluginDeclare_HostCatalogSeedFalse(t *testing.T) {
	p := &cmdNeedsSeedNoDeclarePlugin{}
	reg, app := initSeedCLIRuntime(t, p)

	ran, err := runPluginCLICommandWithDeps(cfgEmpty(), logger.NewWithWriter(io.Discard, "error"),
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "test:cmd-seed-nodeclare", nil, stubPluginCLIDeps(t))
	if err != nil || !ran {
		t.Fatalf("ran=%v err=%v", ran, err)
	}
	if !p.ran || p.hostOK {
		t.Fatalf("ran=%v hostOK=%v: command Needs CapCatalogSeed alone must not open Host.CatalogSeed", p.ran, p.hostOK)
	}
	if port, ok := app.CatalogSeedPort(); ok || port != nil {
		t.Fatalf("CatalogSeedPort bound without plugin declare: (%v, %v)", port, ok)
	}
}

func TestRunPluginCLICommand_UnknownCommandNeedFailsClosed(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	app := &plugin.App{Logger: log, Config: &config.Config{}}
	preparePermissionRegistry(app)
	reg.CLIRegistry().Register(cli.Command{
		Name:        "ghost:typo-need",
		Description: "Unknown command Need",
		RequiresDB:  true,
		Needs:       []string{"catalog.seed.typo"},
		Plugin:      "ghost/typo",
		Run:         func(cli.Context, []string) error { return nil },
	})
	freezePermissionRegistry(app)

	ran, err := runPluginCLICommandWithDeps(cfgEmpty(), log,
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "ghost:typo-need", nil, stubPluginCLIDeps(t))
	if !ran || err == nil || !strings.Contains(err.Error(), "unsupported command Need") {
		t.Fatalf("ran=%v err=%v, want unsupported command Need", ran, err)
	}
}

func TestRunPluginCLICommand_EmptyStringNeedFailsClosed(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	app := &plugin.App{Logger: log, Config: &config.Config{}}
	preparePermissionRegistry(app)
	app.RecordNeedsFor("ghost/empty-need", plugin.CapCatalogSeed)
	reg.CLIRegistry().Register(cli.Command{
		Name:        "ghost:empty-need",
		Description: "Empty-string Needs must not disable compat silently",
		RequiresDB:  true,
		Needs:       []string{""},
		Plugin:      "ghost/empty-need",
		Run:         func(cli.Context, []string) error { return nil },
	})
	freezePermissionRegistry(app)

	ran, err := runPluginCLICommandWithDeps(cfgEmpty(), log,
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "ghost:empty-need", nil, stubPluginCLIDeps(t))
	if !ran || err == nil || !strings.Contains(err.Error(), "empty command Need") {
		t.Fatalf("ran=%v err=%v, want empty command Need fail-closed", ran, err)
	}
}

func TestRunPluginCLICommand_MixedNeedsSeedAndEmptySiblingRequiresDB(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	// Realistic shape: seed opts into Needs: CapCatalogSeed; sibling RequiresDB
	// keeps empty Needs → compat still binds write port.
	seed := &seedPluginMixedNeeds{}
	reg, app := initSeedCLIRuntime(t, seed)
	ran, err := runPluginCLICommandWithDeps(cfgEmpty(), log,
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg, App: app}, nil
		}, "test:seed-explicit-needs", nil, stubPluginCLIDeps(t))
	if err != nil || !ran || !seed.seedSaw {
		t.Fatalf("explicit Needs seed: ran=%v err=%v saw=%v", ran, err, seed.seedSaw)
	}

	sib := &seedPluginMixedNeeds{}
	reg2, app2 := initSeedCLIRuntime(t, sib)
	ran, err = runPluginCLICommandWithDeps(cfgEmpty(), log,
		func() (*pluginCLIRuntime, error) {
			return &pluginCLIRuntime{Registry: reg2, App: app2}, nil
		}, "test:sib-db-empty-needs", nil, stubPluginCLIDeps(t))
	if err != nil || !ran || !sib.sibSaw {
		t.Fatalf("sibling empty Needs: ran=%v err=%v saw=%v (compat over-bind)", ran, err, sib.sibSaw)
	}
}

// seedPluginMixedNeeds: seed with explicit Needs CapCatalogSeed + sibling
// RequiresDB with empty Needs (compat over-bind).
type seedPluginMixedNeeds struct {
	seedSaw bool
	sibSaw  bool
}

func (p *seedPluginMixedNeeds) Name() string { return "test/seed-mixed-needs" }

func (p *seedPluginMixedNeeds) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{pluginsdk.CapCatalogSeed}
}

func (p *seedPluginMixedNeeds) Init(h pluginsdk.Host) error {
	h.Require(pluginsdk.CapCatalogSeed)
	host := h
	sdk := pluginsdk.NewHost(h, p.Name())
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:seed-explicit-needs",
		Description: "Seed with Needs CapCatalogSeed",
		RequiresDB:  true,
		Needs:       []pluginsdk.Capability{pluginsdk.CapCatalogSeed},
		Run: func(pluginsdk.CLIContext, []string) error {
			if _, ok := host.CatalogSeed(); !ok {
				return errCatalogSeedUnbound
			}
			p.seedSaw = true
			return nil
		},
	})
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:sib-db-empty-needs",
		Description: "Sibling RequiresDB empty Needs still binds (compat)",
		RequiresDB:  true,
		Run: func(pluginsdk.CLIContext, []string) error {
			if _, ok := host.CatalogSeed(); !ok {
				return errCatalogSeedUnbound
			}
			p.sibSaw = true
			return nil
		},
	})
	return nil
}

// seedPluginTwoRequiresDB documents empty-Needs compat: every RequiresDB of a
// CapCatalogSeed plugin still gets CatalogSeedPort when Needs is empty.
type seedPluginTwoRequiresDB struct {
	seedSaw  bool
	otherSaw bool
}

func (p *seedPluginTwoRequiresDB) Name() string { return "test/seed-two-db" }

func (p *seedPluginTwoRequiresDB) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{pluginsdk.CapCatalogSeed}
}

func (p *seedPluginTwoRequiresDB) Init(h pluginsdk.Host) error {
	h.Require(pluginsdk.CapCatalogSeed)
	host := h
	sdk := pluginsdk.NewHost(h, p.Name())
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:seed-empty-needs",
		Description: "Seed with empty Needs (compat)",
		RequiresDB:  true,
		Run: func(pluginsdk.CLIContext, []string) error {
			if _, ok := host.CatalogSeed(); !ok {
				return errCatalogSeedUnbound
			}
			p.seedSaw = true
			return nil
		},
	})
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:other-db-empty-needs",
		Description: "Sibling RequiresDB empty Needs still binds (compat)",
		RequiresDB:  true,
		Run: func(pluginsdk.CLIContext, []string) error {
			if _, ok := host.CatalogSeed(); !ok {
				return errCatalogSeedUnbound
			}
			p.otherSaw = true
			return nil
		},
	})
	return nil
}

// cmdNeedsSeedNoDeclarePlugin lists CapCatalogSeed on the command only —
// bind must not open the port; Host.CatalogSeed stays false.
type cmdNeedsSeedNoDeclarePlugin struct {
	ran    bool
	hostOK bool
}

func (cmdNeedsSeedNoDeclarePlugin) Name() string { return "test/cmdseed-nodeclare" }

func (p *cmdNeedsSeedNoDeclarePlugin) Init(h pluginsdk.Host) error {
	host := h
	sdk := pluginsdk.NewHost(h, "test/cmdseed-nodeclare")
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:cmd-seed-nodeclare",
		Description: "Command Needs CapCatalogSeed without plugin Need",
		RequiresDB:  true,
		Needs:       []pluginsdk.Capability{pluginsdk.CapCatalogSeed},
		Run: func(pluginsdk.CLIContext, []string) error {
			p.ran = true
			_, p.hostOK = host.CatalogSeed()
			return nil
		},
	})
	return nil
}

// jobsMediaCLIPlugin Requires CapJobs+CapMedia (Metal Dogs shape). Plugin CLI
// never binds those ports — assert must skip them so commands still run.
type jobsMediaCLIPlugin struct {
	host     pluginsdk.Host
	dbSaw    bool
	nodbSaw  bool
	jobsOnDB bool
	jobsNoDB bool
	mediaDB  bool
	mediaNo  bool
}

func (p *jobsMediaCLIPlugin) Name() string { return "test/jobs-media-cli" }

func (p *jobsMediaCLIPlugin) Needs() []pluginsdk.Capability {
	return []pluginsdk.Capability{pluginsdk.CapJobs, pluginsdk.CapMedia}
}

func (p *jobsMediaCLIPlugin) Init(h pluginsdk.Host) error {
	p.host = h
	h.Require(pluginsdk.CapJobs)
	h.Require(pluginsdk.CapMedia)
	sdk := pluginsdk.NewHost(h, p.Name())
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:jobs-media-db",
		Description: "RequiresDB with CapJobs+CapMedia declared",
		RequiresDB:  true,
		Run: func(pluginsdk.CLIContext, []string) error {
			_, p.jobsOnDB = p.host.Jobs()
			_, p.mediaDB = p.host.Media()
			p.dbSaw = true
			return nil
		},
	})
	sdk.CLI().RegisterCommand(pluginsdk.CLICommand{
		Name:        "test:jobs-media-nodb",
		Description: "Non-DB sibling with CapJobs+CapMedia declared",
		RequiresDB:  false,
		Run: func(pluginsdk.CLIContext, []string) error {
			_, p.jobsNoDB = p.host.Jobs()
			_, p.mediaNo = p.host.Media()
			p.nodbSaw = true
			return nil
		},
	})
	return nil
}

func TestRunPluginCLICommand_CapJobsMediaSkipped(t *testing.T) {
	p := &jobsMediaCLIPlugin{}
	reg, app := initSeedCLIRuntime(t, p)
	log := logger.NewWithWriter(io.Discard, "error")
	rt := func() (*pluginCLIRuntime, error) {
		return &pluginCLIRuntime{Registry: reg, App: app}, nil
	}

	ran, err := runPluginCLICommand(cfgEmpty(), log, rt, "test:jobs-media-nodb", nil)
	if err != nil || !ran || !p.nodbSaw {
		t.Fatalf("nodb: ran=%v err=%v saw=%v", ran, err, p.nodbSaw)
	}
	if p.jobsNoDB || p.mediaNo {
		t.Fatalf("Host.Jobs/Media must be unbound on plugin CLI nodb (jobs=%v media=%v)", p.jobsNoDB, p.mediaNo)
	}

	ran, err = runPluginCLICommandWithDeps(cfgEmpty(), log, rt, "test:jobs-media-db", nil, stubPluginCLIDeps(t))
	if err != nil || !ran || !p.dbSaw {
		t.Fatalf("db: ran=%v err=%v saw=%v (CapJobs/CapMedia must be skipped in pluginCLINeedsSkip)", ran, err, p.dbSaw)
	}
	if p.jobsOnDB || p.mediaDB {
		t.Fatalf("Host.Jobs/Media must be unbound on plugin CLI RequiresDB (jobs=%v media=%v)", p.jobsOnDB, p.mediaDB)
	}
}
