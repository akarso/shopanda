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
	Run         func(ctx Context, args []string) error
}
