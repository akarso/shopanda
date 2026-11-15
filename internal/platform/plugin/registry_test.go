package plugin_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

// ── mock plugin ─────────────────────────────────────────────────────────

type mockPlugin struct {
	name    string
	initErr error
	doPanic bool
	called  bool
	onInit  func()
}

func (p *mockPlugin) Name() string { return p.name }

func (p *mockPlugin) Init(_ *plugin.App) error {
	p.called = true
	if p.onInit != nil {
		p.onInit()
	}
	if p.doPanic {
		panic("plugin init exploded")
	}
	return p.initErr
}

// ── helpers ─────────────────────────────────────────────────────────────

func testLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

func testApp(log logger.Logger) *plugin.App {
	return &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{},
	}
}

// ── Registry tests ──────────────────────────────────────────────────────

func TestRegistry_Register(t *testing.T) {
	log := testLogger()
	reg := plugin.NewRegistry(log)

	p := &mockPlugin{name: "test-plugin"}
	reg.Register(p)

	if reg.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", reg.Len())
	}
	entries := reg.Entries()
	if entries[0].Name != "test-plugin" {
		t.Errorf("Name = %q, want test-plugin", entries[0].Name)
	}
	if entries[0].State != plugin.StateLoaded {
		t.Errorf("State = %q, want loaded", entries[0].State)
	}
}

func TestRegistry_Register_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil plugin")
		}
	}()
	reg := plugin.NewRegistry(testLogger())
	reg.Register(nil)
}

func TestRegistry_Register_EmptyNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty name")
		}
	}()
	reg := plugin.NewRegistry(testLogger())
	reg.Register(&mockPlugin{name: ""})
}

func TestRegistry_Register_DuplicateNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for duplicate name")
		}
	}()
	reg := plugin.NewRegistry(testLogger())
	reg.Register(&mockPlugin{name: "dup"})
	reg.Register(&mockPlugin{name: "dup"})
}

func TestRegistry_InitAll_Success(t *testing.T) {
	log := testLogger()
	reg := plugin.NewRegistry(log)

	p1 := &mockPlugin{name: "p1"}
	p2 := &mockPlugin{name: "p2"}
	reg.Register(p1)
	reg.Register(p2)

	s := reg.InitAll(testApp(log))

	if s.Initialized != 2 {
		t.Fatalf("Initialized = %d, want 2", s.Initialized)
	}
	if s.Failed != 0 {
		t.Fatalf("Failed = %d, want 0", s.Failed)
	}
	if s.Registered != 2 {
		t.Fatalf("Registered = %d, want 2", s.Registered)
	}
	if !p1.called {
		t.Error("p1.Init not called")
	}
	if !p2.called {
		t.Error("p2.Init not called")
	}
	for _, e := range reg.Entries() {
		if e.State != plugin.StateActive {
			t.Errorf("plugin %q state = %q, want active", e.Name, e.State)
		}
	}
}

func TestRegistry_InitAll_FailureContinues(t *testing.T) {
	log := testLogger()
	reg := plugin.NewRegistry(log)

	failing := &mockPlugin{name: "failing", initErr: errors.New("init failed")}
	ok := &mockPlugin{name: "ok"}
	reg.Register(failing)
	reg.Register(ok)

	s := reg.InitAll(testApp(log))

	if s.Initialized != 1 {
		t.Fatalf("Initialized = %d, want 1", s.Initialized)
	}
	if s.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", s.Failed)
	}

	entries := reg.Entries()
	if entries[0].State != plugin.StateFailed {
		t.Errorf("failing state = %q, want failed", entries[0].State)
	}
	if entries[0].Err == nil {
		t.Error("failing Err should be non-nil")
	}
	if entries[1].State != plugin.StateActive {
		t.Errorf("ok state = %q, want active", entries[1].State)
	}
}

func TestRegistry_InitAllContext_CancelBetweenPlugins(t *testing.T) {
	log := testLogger()
	reg := plugin.NewRegistry(log)
	ctx, cancel := context.WithCancel(context.Background())
	p1 := &mockPlugin{name: "p1", onInit: cancel}
	p2 := &mockPlugin{name: "p2"}
	reg.Register(p1)
	reg.Register(p2)

	s := reg.InitAllContext(ctx, testApp(log))
	if s.Initialized != 1 || s.Failed != 1 || s.Canceled != 1 {
		t.Fatalf("summary = %+v, want Initialized=1 Failed=1 Canceled=1", s)
	}
	entries := reg.Entries()
	if entries[0].State != plugin.StateActive {
		t.Fatalf("p1 state = %q, want active", entries[0].State)
	}
	if entries[1].State != plugin.StateFailed || !errors.Is(entries[1].Err, context.Canceled) {
		t.Fatalf("p2 = %+v, want failed canceled", entries[1])
	}
	if p2.called {
		t.Fatal("p2 Init must not run after cancel")
	}
}

func TestApp_Context_DuringInit(t *testing.T) {
	log := testLogger()
	reg := plugin.NewRegistry(log)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var saw context.Context
	reg.Register(&contextProbePlugin{probe: func(app *plugin.App) { saw = app.Context() }})
	_ = reg.InitAllContext(ctx, testApp(log))
	if saw != ctx {
		t.Fatalf("App.Context during Init = %v, want boot ctx", saw)
	}
}

type contextProbePlugin struct {
	probe func(*plugin.App)
}

func (p *contextProbePlugin) Name() string { return "ctx-probe" }
func (p *contextProbePlugin) Init(app *plugin.App) error {
	p.probe(app)
	return nil
}

func TestRegistry_InitAllContext_PreCanceledMarksLoadedFailed(t *testing.T) {
	log := testLogger()
	reg := plugin.NewRegistry(log)
	reg.Register(&mockPlugin{name: "p1"})
	reg.Register(&mockPlugin{name: "p2"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := reg.InitAllContext(ctx, testApp(log))
	if s.Initialized != 0 || s.Failed != 2 || s.Canceled != 2 {
		t.Fatalf("summary = %+v, want Initialized=0 Failed=2 Canceled=2", s)
	}
	for _, e := range reg.Entries() {
		if e.State != plugin.StateFailed || !errors.Is(e.Err, context.Canceled) {
			t.Fatalf("entry %+v, want failed canceled", e)
		}
	}
}

func TestRegistry_InitAll_NilAppPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil app")
		}
	}()
	reg := plugin.NewRegistry(testLogger())
	reg.Register(&mockPlugin{name: "p"})
	reg.InitAll(nil)
}

func TestRegistry_InitAll_SkipsNonLoaded(t *testing.T) {
	log := testLogger()
	reg := plugin.NewRegistry(log)

	p := &mockPlugin{name: "p"}
	reg.Register(p)

	// First init → active
	reg.InitAll(testApp(log))
	p.called = false

	// Second init → skip (already active)
	s := reg.InitAll(testApp(log))
	if s.Initialized != 0 {
		t.Fatalf("second Initialized = %d, want 0", s.Initialized)
	}
	if p.called {
		t.Error("Init should not be called on already-active plugin")
	}
}

func TestRegistry_InitAll_Empty(t *testing.T) {
	log := testLogger()
	reg := plugin.NewRegistry(log)

	s := reg.InitAll(testApp(log))
	if s.Initialized != 0 {
		t.Fatalf("Initialized = %d, want 0", s.Initialized)
	}
}

func TestRegistry_Entries_ReturnsCopy(t *testing.T) {
	log := testLogger()
	reg := plugin.NewRegistry(log)
	reg.Register(&mockPlugin{name: "p"})

	entries := reg.Entries()
	entries[0].State = plugin.StateFailed // mutate copy

	// Original should be unchanged
	origEntries := reg.Entries()
	if origEntries[0].State != plugin.StateLoaded {
		t.Errorf("original state changed to %q", origEntries[0].State)
	}
}

func TestNewRegistry_NilLoggerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil logger")
		}
	}()
	plugin.NewRegistry(nil)
}

func TestRegistry_InitAll_PanicRecovery(t *testing.T) {
	log := testLogger()
	reg := plugin.NewRegistry(log)

	panicking := &mockPlugin{name: "panicker", doPanic: true}
	normal := &mockPlugin{name: "normal"}
	reg.Register(panicking)
	reg.Register(normal)

	s := reg.InitAll(testApp(log))

	if s.Initialized != 1 {
		t.Fatalf("Initialized = %d, want 1", s.Initialized)
	}
	if s.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", s.Failed)
	}

	entries := reg.Entries()
	if entries[0].State != plugin.StateFailed {
		t.Errorf("panicker state = %q, want failed", entries[0].State)
	}
	if entries[0].Err == nil {
		t.Error("panicker Err should be non-nil")
	}
	if entries[1].State != plugin.StateActive {
		t.Errorf("normal state = %q, want active", entries[1].State)
	}
	if !normal.called {
		t.Error("normal.Init not called")
	}
}

type cliThenFailPlugin struct {
	name string
}

func (p *cliThenFailPlugin) Name() string { return p.name }

func (p *cliThenFailPlugin) Init(app *plugin.App) error {
	app.RegisterCommand(cli.Command{
		Name:        "fail:cmd",
		Description: "Should not persist",
		Run:         func(cli.Context, []string) error { return nil },
	})
	return errors.New("init failed after CLI registration")
}

func TestRegistry_InitAll_FailedPluginDoesNotRetainCLICommands(t *testing.T) {
	log := testLogger()
	reg := plugin.NewRegistry(log)
	reg.Register(&cliThenFailPlugin{name: "cli-fail"})
	summary := reg.InitAll(testApp(log))
	if summary.Failed != 1 || summary.Initialized != 0 {
		t.Fatalf("summary = %+v, want 0 initialized and 1 failed", summary)
	}
	if cmds := reg.CLIRegistry().List(); len(cmds) != 0 {
		t.Fatalf("CLIRegistry().List() = %#v, want empty after failed init", cmds)
	}
}
