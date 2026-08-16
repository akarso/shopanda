package b2b

import (
	"github.com/akarso/shopanda/internal/domain/customergroup"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/b2b/groups"
	b2bpricing "github.com/akarso/shopanda/plugins/b2b/pricing"
)

// NewAdminHandlersForTest exposes newAdminHandlers for wiring unit tests.
func NewAdminHandlersForTest(
	boot *plugin.Bootstrap,
	groupRepo customergroup.Repository,
	groupPriceRepo customergroup.GroupPriceRepository,
) (*groups.AdminHandler, *b2bpricing.AdminHandler) {
	return newAdminHandlers(boot, groupRepo, groupPriceRepo)
}
