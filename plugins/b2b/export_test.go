package b2b

import (
	"io/fs"
	"strings"

	"github.com/akarso/shopanda/pkg/pluginsdk"
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

// NewAdminHandlersForTest exposes newAdminHandlers for wiring unit tests.
func NewAdminHandlersForTest(
	customers pluginsdk.CustomerPort,
	variants pluginsdk.CatalogPort,
	groupRepo groups.Repository,
	groupPriceRepo b2bpricing.GroupPriceRepository,
) (*groups.AdminHandler, *b2bpricing.AdminHandler) {
	return newAdminHandlers(customers, variants, groupRepo, groupPriceRepo)
}
