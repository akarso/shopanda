package pluginsdk

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
)

// Adapt wraps a public Plugin so it can be registered on plugin.Registry
// (which still expects Init(*plugin.App) during dual-mode). When p also
// implements Migrator, the returned plugin.Plugin additionally implements
// plugin.Migrator so `app migrate` / InitAll apply its SQL before Init.
func Adapt(p Plugin) plugin.Plugin {
	if p == nil {
		panic("pluginsdk: Adapt: plugin must not be nil")
	}
	base := &hostPluginBridge{p: p}
	if m, ok := p.(Migrator); ok {
		return &migratingPluginBridge{hostPluginBridge: base, migrator: m}
	}
	return base
}

type hostPluginBridge struct {
	p Plugin
}

func (b *hostPluginBridge) Name() string { return b.p.Name() }

func (b *hostPluginBridge) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("pluginsdk: Init: app must not be nil")
	}
	if nd, ok := b.p.(NeedsDeclarer); ok {
		caps := make([]plugin.Capability, len(nd.Needs()))
		for i, c := range nd.Needs() {
			caps[i] = plugin.Capability(c)
		}
		app.RecordNeeds(caps...)
	}
	// AsHostFor (not AsHost): scopes ConfigValue and gates the
	// Catalog/Orders/Customers/BootstrapDB getters to this plugin's own
	// declared config namespace and capabilities (see AsHostFor doc).
	return b.p.Init(AsHostFor(app, b.p.Name()))
}

// migratingPluginBridge additionally implements plugin.Migrator. Kept as a
// distinct type (rather than an unconditional method on hostPluginBridge) so
// plugins without migrations are never mistaken for a Migrator plugin.
type migratingPluginBridge struct {
	*hostPluginBridge
	migrator Migrator
}

func (b *migratingPluginBridge) Migration() plugin.MigrationSpec {
	return b.migrator.Migration()
}
