package b2b

import (
	"io/fs"
	"strings"

	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/b2b/groups"
	b2bpricing "github.com/akarso/shopanda/plugins/b2b/pricing"
)

// MigrationBasenamesForTest returns embedded migration basenames.
func MigrationBasenamesForTest() ([]string, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}

// PluginNameForTest returns the stable plugin name / migrate namespace.
func PluginNameForTest() string { return pluginName }

// LegacyBareMigrationBasenamesForTest returns a copy of the frozen adopt allowlist.
func LegacyBareMigrationBasenamesForTest() []string {
	out := make([]string, len(legacyBareMigrationBasenames))
	copy(out, legacyBareMigrationBasenames)
	return out
}

// MigrationFilesForTest returns the embedded migrations FS and directory name.
func MigrationFilesForTest() (fs.FS, string) {
	return migrationFiles, "migrations"
}

// NewAdminHandlersForTest exposes newAdminHandlers for wiring unit tests.
func NewAdminHandlersForTest(
	boot *plugin.Bootstrap,
	groupRepo customergroup.Repository,
	groupPriceRepo customergroup.GroupPriceRepository,
) (*groups.AdminHandler, *b2bpricing.AdminHandler) {
	return newAdminHandlers(boot, groupRepo, groupPriceRepo)
}
