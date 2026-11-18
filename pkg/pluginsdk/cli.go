package pluginsdk

import (
	"context"

	"github.com/akarso/shopanda/internal/platform/cli"
)

// CLIContext provides runtime facilities to a CLI command handler.
// Catalog/DB access goes through Host (CatalogSeed, BootstrapDB + CapDB) —
// not a raw *sql.DB on this context.
type CLIContext struct {
	Ctx    context.Context
	Logger Logger
}

// CLICommand is a named CLI subcommand registered explicitly at startup.
// Name must use the domain:action convention (e.g. "example:ping").
type CLICommand struct {
	Name        string
	Description string
	// RequiresDB asks the composition root to open the platform DB before Run
	// so Host ports (including CatalogSeed when this plugin Needs it) can bind.
	RequiresDB bool
	Run        func(ctx CLIContext, args []string) error
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
		RequiresDB:  cmd.RequiresDB,
		Run: func(ctx cli.Context, args []string) error {
			return cmd.Run(CLIContext{Ctx: ctx.Ctx, Logger: ctx.Logger}, args)
		},
	})
}
