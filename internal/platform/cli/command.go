package cli

import (
	"context"
	"database/sql"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// Context provides runtime facilities to a CLI command handler.
type Context struct {
	Ctx    context.Context
	Config *config.Config
	Logger logger.Logger
	DB     *sql.DB
}

// Command is a named CLI subcommand registered explicitly at startup.
type Command struct {
	Name        string
	Description string
	RequiresDB  bool
	// Needs lists composition-root capabilities this command requires when it
	// runs (e.g. "catalog.seed"). Distinct from plugin-level Needs(): a plugin
	// may Need CapCatalogSeed for its seed command while other commands omit
	// it and run with RequiresDB false. CapCatalogSeed in Needs implies
	// RequiresDB (enforced by runPluginCLICommand).
	Needs []string
	// Plugin is the registering plugin name (set by plugin.App.RegisterCommand
	// from CurrentPluginName during Init). Empty for non-plugin registrations.
	Plugin string
	Run    func(ctx Context, args []string) error
}
