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
	// so Host ports (including CatalogSeed when this command/plugin needs it)
	// can bind.
	RequiresDB bool
	// Needs lists capabilities required for this command only (e.g. CapCatalogSeed).
	// Plugin-level Needs() alone no longer blocks sibling RequiresDB:false
	// commands. CapCatalogSeed here requires RequiresDB true.
	Needs []Capability
	Run   func(ctx CLIContext, args []string) error
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
	needs := make([]string, 0, len(cmd.Needs))
	for _, cap := range cmd.Needs {
		if cap == "" {
			continue
		}
		needs = append(needs, string(cap))
	}
	coreApp(c.sdk.host).RegisterCommand(cli.Command{
		Name:        cmd.Name,
		Description: cmd.Description,
		RequiresDB:  cmd.RequiresDB,
		Needs:       needs,
		Run: func(ctx cli.Context, args []string) error {
			return cmd.Run(CLIContext{Ctx: ctx.Ctx, Logger: ctx.Logger}, args)
		},
	})
}
