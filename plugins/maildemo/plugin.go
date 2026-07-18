package maildemo

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/plugin"
)

// Plugin demonstrates integrator mail port replacement via RegisterMailSender.
type Plugin struct{}

// New returns the mail sender reference plugin.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string { return "maildemo/reference" }

func (p *Plugin) Init(app *plugin.App) error {
	if app == nil {
		return fmt.Errorf("maildemo plugin: app not configured")
	}
	if app.Config == nil {
		return fmt.Errorf("maildemo plugin: config not configured")
	}
	cfg := app.Config.Plugins.MailDemo
	if !cfg.Enabled {
		return fmt.Errorf("maildemo plugin: disabled (plugins.maildemo.enabled=false)")
	}
	app.RegisterMailSender(NewLogMailer(app.Logger, cfg.SubjectPrefix))
	return nil
}
