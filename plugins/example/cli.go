package example

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

const CommandPing = "example:ping"

func registerCLICommands(app *plugin.App) {
	app.RegisterCommand(cli.Command{
		Name:        CommandPing,
		Description: "Verify example plugin CLI registration",
		Run: func(ctx cli.Context, _ []string) error {
			fmt.Println("example plugin ok")
			ctx.Logger.Info("example.ping", map[string]interface{}{
				"fee_minor_units": ctx.Config.Plugins.Example.FeeMinorUnits,
			})
			return nil
		},
	})
}
