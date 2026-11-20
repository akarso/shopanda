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
	// Needs may list CapCatalogSeed for this command only. Unknown values fail
	// closed at run (not a general per-command capability API). Distinct from
	// plugin-level Needs(): CapCatalogSeed here implies RequiresDB and the
	// owning plugin must also have declared CapCatalogSeed for Host.CatalogSeed
	// to open. Empty Needs + plugin CapCatalogSeed still binds (compat).
	Needs []string
	// Plugin is the registering plugin name (set by plugin.App.RegisterCommand
	// from CurrentPluginName during Init). Empty for non-plugin registrations.
	Plugin string
	Run    func(ctx Context, args []string) error
}
