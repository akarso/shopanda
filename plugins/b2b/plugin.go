package b2b

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
)

// Plugin is the commercial B2B extension module.
type Plugin struct{}

// New returns the B2B plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "shopanda/b2b" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("b2b plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("b2b plugin: config not configured")
	}
	if !app.Config.Plugins.B2B.Enabled {
		return fmt.Errorf("b2b plugin: disabled (plugins.b2b.enabled=false)")
	}

	ok, err := Validate(app.Config.Plugins.B2B.LicenseKey)
	if err != nil {
		return fmt.Errorf("b2b plugin: %w", err)
	}
	if !ok {
		return fmt.Errorf("b2b plugin: invalid or missing license (set plugins.b2b.license_key)")
	}

	if app.Logger != nil {
		app.Logger.Info("b2b plugin: licensed (stub — no features registered yet)", nil)
	}
	return nil
}
