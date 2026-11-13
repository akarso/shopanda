package plugin_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/extension"
	domainext "github.com/akarso/shopanda/internal/domain/extension"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestApp_Extensions_RegisterField(t *testing.T) {
	reg := extension.NewRegistry()
	app := &plugin.App{}
	app.SetExtensionRegistry(reg)

	err := app.Extensions().RegisterField(domainext.FieldDef{
		Code:        "acme.gift.wrap_level",
		Label:       "Gift wrap",
		Type:        domainext.FieldTypeEnum,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
		Validation:  domainext.EnumOptions("none", "standard"),
	})
	if err != nil {
		t.Fatalf("RegisterField: %v", err)
	}

	field, ok := app.ExtensionRegistry().Get("acme.gift.wrap_level")
	if !ok {
		t.Fatal("expected field in shared registry")
	}
	if field.Scope != domainext.TargetProduct {
		t.Errorf("Scope = %q", field.Scope)
	}
}

func TestApp_Extensions_RegisterFieldDuplicate(t *testing.T) {
	app := &plugin.App{}
	def := domainext.FieldDef{
		Code:        "acme.gift.wrap_level",
		Label:       "Gift wrap",
		Type:        domainext.FieldTypeString,
		Scope:       domainext.TargetProduct,
		StorageMode: domainext.StorageStored,
	}
	if err := app.Extensions().RegisterField(def); err != nil {
		t.Fatalf("first RegisterField: %v", err)
	}
	err := app.Extensions().RegisterField(def)
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestApp_SetExtensionRegistry_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil extension registry")
		}
	}()
	app := &plugin.App{}
	app.SetExtensionRegistry(nil)
}

func TestApp_Extensions_ConcurrentLazyInit(t *testing.T) {
	app := &plugin.App{}
	const workers = 8
	refs := make([]*extension.Registry, workers)
	done := make(chan struct{}, workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			_ = app.Extensions()
			refs[i] = app.ExtensionRegistry()
			done <- struct{}{}
		}()
	}
	for i := 0; i < workers; i++ {
		<-done
	}
	first := refs[0]
	for i := 1; i < workers; i++ {
		if refs[i] != first {
			t.Fatalf("ExtensionRegistry pointer mismatch at worker %d", i)
		}
	}
}

func TestApp_WithPluginWrite_ValidatesCodesAgainstRegistry(t *testing.T) {
	reg := extension.NewRegistry()
	app := &plugin.App{}
	app.SetExtensionRegistry(reg)
	if err := app.Extensions().RegisterField(domainext.FieldDef{
		Code: "acme.locked", Label: "L", Type: domainext.FieldTypeString, Scope: domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot, Access: domainext.Access{PluginOnlyWrite: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.Extensions().RegisterField(domainext.FieldDef{
		Code: "acme.plain", Label: "P", Type: domainext.FieldTypeString, Scope: domainext.TargetCartItem,
		StorageMode: domainext.StorageStored,
	}); err != nil {
		t.Fatal(err)
	}
	app.WithPluginWrite(context.Background(), "acme", "acme.locked") // ok

	for name, code := range map[string]string{"typo": "acme.lockd", "not plugin-only": "acme.plain"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s: expected panic", name)
				}
			}()
			app.WithPluginWrite(context.Background(), "acme", code)
		}()
	}
}

func TestApp_WithPluginWrite_PanicNamesRegistrationOrder(t *testing.T) {
	app := &plugin.App{}
	app.SetExtensionRegistry(extension.NewRegistry())
	defer func() {
		msg, _ := recover().(string)
		if !strings.Contains(msg, "RegisterField") {
			t.Fatalf("panic = %q, want hint about registering the field first", msg)
		}
	}()
	app.WithPluginWrite(context.Background(), "acme", "acme.locked")
}

func TestApp_ExtensionsFor_RecordsOwnerAndGuardsWithPluginWrite(t *testing.T) {
	app := &plugin.App{}
	app.SetExtensionRegistry(extension.NewRegistry())
	def := func(code string) domainext.FieldDef {
		return domainext.FieldDef{Code: code, Label: "L", Type: domainext.FieldTypeString, Scope: domainext.TargetCartItem,
			StorageMode: domainext.StorageSnapshot, Access: domainext.Access{PluginOnlyWrite: true}}
	}
	if err := app.ExtensionsFor("acme").RegisterField(def("acme.owned")); err != nil {
		t.Fatal(err)
	}
	if err := app.Extensions().RegisterField(def("legacy.free")); err != nil {
		t.Fatal(err)
	}
	if got := app.ExtensionRegistry().Owner("acme.owned"); got != "acme" {
		t.Fatalf("owner = %q", got)
	}
	app.WithPluginWrite(context.Background(), "acme", "acme.owned")    // owner: ok
	app.WithPluginWrite(context.Background(), "anyone", "legacy.free") // unowned: ok

	defer func() {
		msg, _ := recover().(string)
		if !strings.Contains(msg, "owned by") {
			t.Fatalf("panic = %q, want owner mismatch message", msg)
		}
	}()
	app.WithPluginWrite(context.Background(), "intruder", "acme.owned")
}

func TestApp_ExtensionsFor_BlankOwnerPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	(&plugin.App{}).ExtensionsFor("  ")
}

type extFuncPlugin struct {
	name string
	init func(app *plugin.App) error
}

func (p *extFuncPlugin) Name() string               { return p.name }
func (p *extFuncPlugin) Init(app *plugin.App) error { return p.init(app) }

func ownerTestField(code string) domainext.FieldDef {
	return domainext.FieldDef{Code: code, Label: "L", Type: domainext.FieldTypeString, Scope: domainext.TargetCartItem,
		StorageMode: domainext.StorageSnapshot, Access: domainext.Access{PluginOnlyWrite: true}}
}

func TestRegistry_InitAll_BindsExtensionOwnerToLoadedPlugin(t *testing.T) {
	log := testLogger()
	reg := plugin.NewRegistry(log)
	reg.Register(&extFuncPlugin{name: "acme", init: func(app *plugin.App) error {
		return app.Extensions().RegisterField(ownerTestField("acme.design")) // legacy call, loader binds owner
	}})
	reg.Register(&extFuncPlugin{name: "evil", init: func(app *plugin.App) error {
		return app.ExtensionsFor("acme").RegisterField(ownerTestField("evil.squat")) // squatting another name
	}})
	app := testApp(log)
	app.SetExtensionRegistry(extension.NewRegistry())

	summary := reg.InitAll(app)

	if got := app.ExtensionRegistry().Owner("acme.design"); got != "acme" {
		t.Fatalf("owner of acme.design = %q, want acme (bound by loader)", got)
	}
	if summary.Failed != 1 {
		t.Fatalf("Failed = %d, want 1 (squatting plugin must fail Init)", summary.Failed)
	}
	if _, ok := app.ExtensionRegistry().Get("evil.squat"); ok {
		t.Fatal("squatted field must not be registered")
	}
	// Outside Init the legacy call stays unowned.
	if err := app.Extensions().RegisterField(ownerTestField("late.free")); err != nil {
		t.Fatal(err)
	}
	if got := app.ExtensionRegistry().Owner("late.free"); got != "" {
		t.Fatalf("owner outside Init = %q, want empty", got)
	}
}

func TestApp_WithPluginWrite_TrimsRegistrantBeforeOwnerCheck(t *testing.T) {
	app := &plugin.App{}
	app.SetExtensionRegistry(extension.NewRegistry())
	if err := app.ExtensionsFor(" acme ").RegisterField(ownerTestField("acme.design")); err != nil {
		t.Fatal(err)
	}
	if got := app.ExtensionRegistry().Owner("acme.design"); got != "acme" {
		t.Fatalf("owner = %q, want trimmed", got)
	}
	app.WithPluginWrite(context.Background(), " acme ", "acme.design") // must not panic
}

func TestApp_UnownedPluginOnlyFields_ListsOnlyUnowned(t *testing.T) {
	app := &plugin.App{}
	app.SetExtensionRegistry(extension.NewRegistry())
	_ = app.ExtensionsFor("acme").RegisterField(ownerTestField("acme.owned"))
	_ = app.Extensions().RegisterField(ownerTestField("legacy.free"))
	plain := ownerTestField("legacy.plain")
	plain.Access = domainext.Access{}
	_ = app.Extensions().RegisterField(plain)
	got := app.UnownedPluginOnlyFields()
	if len(got) != 1 || got[0] != "legacy.free" {
		t.Fatalf("UnownedPluginOnlyFields = %v, want [legacy.free]", got)
	}
}

func TestRegistry_InitAll_FailedInitRollsBackItsExtensionFields(t *testing.T) {
	log := testLogger()
	app := testApp(log)
	app.SetExtensionRegistry(extension.NewRegistry())
	failing := true
	newReg := func() *plugin.Registry {
		reg := plugin.NewRegistry(log)
		reg.Register(&extFuncPlugin{name: "acme", init: func(app *plugin.App) error {
			if err := app.Extensions().RegisterField(ownerTestField("acme.design")); err != nil {
				return err
			}
			if failing {
				return errors.New("init failed after registering")
			}
			return nil
		}})
		return reg
	}
	if s := newReg().InitAll(app); s.Failed != 1 {
		t.Fatalf("Failed = %d, want 1", s.Failed)
	}
	if _, ok := app.ExtensionRegistry().Get("acme.design"); ok {
		t.Fatal("failed plugin's fields must be rolled back")
	}
	failing = false
	if s := newReg().InitAll(app); s.Failed != 0 || s.Initialized != 1 {
		t.Fatalf("retry summary = %+v, want clean init (no ErrAlreadyRegistered)", s)
	}
	if got := app.ExtensionRegistry().Owner("acme.design"); got != "acme" {
		t.Fatalf("owner after retry = %q", got)
	}
}

func TestRegistry_InitAll_LateRegistrationsStayUnowned(t *testing.T) {
	log := testLogger()
	app := testApp(log)
	app.SetExtensionRegistry(extension.NewRegistry())
	reg := plugin.NewRegistry(log)
	var stored *plugin.Extensions
	proceed := make(chan struct{})
	done := make(chan struct{})
	reg.Register(&extFuncPlugin{name: "a", init: func(app *plugin.App) error {
		stored = app.Extensions() // used later, e.g. in Start
		go func() {
			<-proceed // background work after Init finished
			_ = app.Extensions().RegisterField(ownerTestField("a.bg"))
			close(done)
		}()
		return nil
	}})
	reg.Register(&extFuncPlugin{name: "b", init: func(app *plugin.App) error { return nil }})
	reg.InitAll(app)

	close(proceed)
	<-done
	if err := stored.RegisterField(ownerTestField("a.start")); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"a.bg", "a.start"} {
		if got := app.ExtensionRegistry().Owner(code); got != "" {
			t.Fatalf("owner of late field %s = %q, want unowned (never misattributed to b)", code, got)
		}
	}
	got := app.UnownedPluginOnlyFields()
	if len(got) != 2 {
		t.Fatalf("UnownedPluginOnlyFields = %v, want both late fields", got)
	}
}

func TestRegistry_EntryNameMatchesPluginName(t *testing.T) {
	reg := plugin.NewRegistry(testLogger())
	reg.Register(&extFuncPlugin{name: "acme", init: func(*plugin.App) error { return nil }})
	for _, e := range reg.Entries() {
		if e.Name != e.Plugin.Name() {
			t.Fatalf("entry name %q != Plugin.Name() %q", e.Name, e.Plugin.Name())
		}
	}
}

func TestApp_PluginWriteContext_ReturnsErrorsInsteadOfPanicking(t *testing.T) {
	app := &plugin.App{}
	app.SetExtensionRegistry(extension.NewRegistry())
	if err := app.ExtensionsFor("acme").RegisterField(ownerTestField("acme.owned")); err != nil {
		t.Fatal(err)
	}
	plain := ownerTestField("acme.plain")
	plain.Access = domainext.Access{}
	_ = app.ExtensionsFor("acme").RegisterField(plain)

	if _, err := app.PluginWriteContext(context.Background(), " acme ", "acme.owned"); err != nil {
		t.Fatalf("valid: %v", err)
	}
	for name, tc := range map[string]struct {
		who   string
		codes []string
	}{
		"owner mismatch":  {"intruder", []string{"acme.owned"}},
		"unregistered":    {"acme", []string{"acme.typo"}},
		"not plugin-only": {"acme", []string{"acme.plain"}},
		"blank name":      {"  ", []string{"acme.owned"}},
		"no codes":        {"acme", nil},
	} {
		if _, err := app.PluginWriteContext(context.Background(), tc.who, tc.codes...); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestRegistry_InitAll_WarnsAboutUnownedPluginOnlyFields(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, "warn")
	app := testApp(log)
	app.SetExtensionRegistry(extension.NewRegistry())
	if err := app.Extensions().RegisterField(ownerTestField("core.free")); err != nil { // before Init: unowned
		t.Fatal(err)
	}
	reg := plugin.NewRegistry(log)
	reg.Register(&extFuncPlugin{name: "acme", init: func(app *plugin.App) error {
		return app.Extensions().RegisterField(ownerTestField("acme.design"))
	}})

	reg.InitAll(app) // any boot path, not only serve

	out := buf.String()
	if !strings.Contains(out, "extension.plugin_only.unowned") || !strings.Contains(out, "core.free") {
		t.Fatalf("expected unowned warning naming core.free, got %q", out)
	}
	if strings.Contains(out, "acme.design") {
		t.Fatalf("owned field must not be warned about: %q", out)
	}
	buf.Reset()
	app.WarnUnownedPluginOnlyFields() // second call (serve after LoadPersisted): nothing new
	if buf.Len() != 0 {
		t.Fatalf("already-warned field must not be logged again: %q", buf.String())
	}
}

func TestApp_WarnUnownedPluginOnlyFields_NilLoggerDoesNotPanic(t *testing.T) {
	app := &plugin.App{}
	app.SetExtensionRegistry(extension.NewRegistry())
	if err := app.Extensions().RegisterField(ownerTestField("x.free")); err != nil {
		t.Fatal(err)
	}
	app.WarnUnownedPluginOnlyFields()
}

func TestRegistry_Replace_CannotDisablePluginOnlyOnOwnedField(t *testing.T) {
	reg := extension.NewRegistry()
	if err := reg.RegisterOwned(ownerTestField("acme.design"), "acme"); err != nil {
		t.Fatal(err)
	}
	field, _ := reg.Get("acme.design")
	field.Access.PluginOnlyWrite = false
	if err := reg.Replace(field); err == nil {
		t.Fatal("Replace must refuse to clear PluginOnlyWrite on an owned plugin-only field")
	}
	got, _ := reg.Get("acme.design")
	if !got.Access.PluginOnlyWrite {
		t.Fatal("PluginOnlyWrite must stay set")
	}
}

// Known limit (documented): the loader attributes by snapshot-diff, so a
// registration by another plugin's background goroutine during Init is blamed on
// the plugin being initialised. Fixing it needs a per-plugin App facade.
func TestRegistry_InitAll_BackgroundRegistrationDuringOtherInit_KnownLimit(t *testing.T) {
	log := testLogger()
	app := testApp(log)
	app.SetExtensionRegistry(extension.NewRegistry())
	reg := plugin.NewRegistry(log)
	reg.Register(&extFuncPlugin{name: "b", init: func(app *plugin.App) error {
		done := make(chan struct{})
		go func() { // stands in for plugin A's leftover goroutine
			_ = app.ExtensionsFor("a").RegisterField(ownerTestField("a.bg"))
			close(done)
		}()
		<-done
		return nil
	}})
	summary := reg.InitAll(app)
	if summary.Failed != 1 {
		t.Fatalf("documented limit: B's Init fails on A's field (Failed=%d)", summary.Failed)
	}
	if _, ok := app.ExtensionRegistry().Get("a.bg"); ok {
		t.Fatal("rolled back field must not stay registered")
	}
}

func TestApp_PluginWriteContext_ErrorsNameThemselves(t *testing.T) {
	app := &plugin.App{}
	app.SetExtensionRegistry(extension.NewRegistry())
	_, err := app.PluginWriteContext(context.Background(), "", "x")
	if err == nil || !strings.Contains(err.Error(), "PluginWriteContext:") || strings.Contains(err.Error(), "WithPluginWrite:") {
		t.Fatalf("error = %v", err)
	}
}
