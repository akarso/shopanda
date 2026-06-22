package core

import (
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	cmanualpay "github.com/akarso/shopanda/plugins/core/manualpay"
	coremeili "github.com/akarso/shopanda/plugins/core/meilisearch"
	corepostgres "github.com/akarso/shopanda/plugins/core/postgres"
	cstoragelocal "github.com/akarso/shopanda/plugins/core/storagelocal"
	cstorages3 "github.com/akarso/shopanda/plugins/core/storages3"
	corestripe "github.com/akarso/shopanda/plugins/core/stripe"
)

// Register adds core infrastructure plugins implied by the active driver switches.
func Register(registry *plugin.Registry, cfg *config.Config) {
	if cfg.CorePostgresSearchEnabled() {
		registry.Register(corepostgres.NewSearchPlugin())
	} else if cfg.CoreMeilisearchSearchEnabled() {
		registry.Register(coremeili.NewSearchPlugin())
	}
	if cfg.CorePostgresCacheEnabled() {
		registry.Register(corepostgres.NewCachePlugin())
	}
	if cfg.CorePostgresQueueEnabled() {
		registry.Register(corepostgres.NewQueuePlugin())
	}
	registry.Register(cmanualpay.NewPaymentPlugin())
	if cfg.Payment.Stripe.Enabled {
		registry.Register(corestripe.NewPaymentPlugin())
	}
	if cfg.CoreLocalStorageEnabled() {
		registry.Register(cstoragelocal.NewStoragePlugin())
	} else if cfg.CoreS3StorageEnabled() {
		registry.Register(cstorages3.NewStoragePlugin())
	}
}
