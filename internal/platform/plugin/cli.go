package plugin

import (
	"github.com/akarso/shopanda/internal/platform/cli"
)

// RegisterCommand registers a CLI subcommand for this plugin.
// Commands must use the domain:action naming convention (e.g. example:ping).
// Sets cmd.Plugin from CurrentPluginName when empty so the composition root
// can attribute RequiresDB / CapCatalogSeed binding to the owner.
func (a *App) RegisterCommand(cmd cli.Command) {
	if a == nil {
		panic("plugin: app must not be nil")
	}
	if a.cliRegistry == nil {
		panic("plugin: cli registry not configured")
	}
	if cmd.Plugin == "" {
		cmd.Plugin = a.CurrentPluginName()
	}
	a.cliRegistry.Register(cmd)
}
