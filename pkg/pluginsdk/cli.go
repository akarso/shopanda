package pluginsdk

import (
	"context"

	"github.com/akarso/shopanda/internal/platform/cli"
)

// CLIContext provides runtime facilities to a CLI command handler.
type CLIContext struct {
	Ctx    context.Context
	Logger Logger
}

// CLICommand is a named CLI subcommand registered explicitly at startup.
// Name must use the domain:action convention (e.g. "example:ping").
type CLICommand struct {
	Name        string
	Description string
	Run         func(ctx CLIContext, args []string) error
}

// CLI returns CLI command registration helpers for the SDK plugin.
func (s *SDK) CLI() *CLI {
	return &CLI{sdk: s}
}

// CLI registers plugin-defined CLI subcommands.
type CLI struct {
	sdk *SDK
}

// RegisterCommand registers cmd on the host app's CLI registry.
func (c *CLI) RegisterCommand(cmd CLICommand) {
	coreApp(c.sdk.host).RegisterCommand(cli.Command{
		Name:        cmd.Name,
		Description: cmd.Description,
		Run: func(ctx cli.Context, args []string) error {
			return cmd.Run(CLIContext{Ctx: ctx.Ctx, Logger: ctx.Logger}, args)
		},
	})
}
