package sitekit

import (
	"embed"

	"github.com/akarso/shopanda/pkg/pluginsdk"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var _ pluginsdk.Migrator = (*Plugin)(nil)

// Migration implements pluginsdk.Migrator for app migrate / InitAll
// (pluginsdk.Adapt forwards it to the core plugin.Migrator contract).
func (p *Plugin) Migration() pluginsdk.MigrationSpec {
	return pluginsdk.MigrationSpec{
		FS:  migrationFiles,
		Dir: "migrations",
	}
}
