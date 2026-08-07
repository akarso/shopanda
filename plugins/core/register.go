package core

import (
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/plugin"
	cgraphql "github.com/akarso/shopanda/plugins/core/graphql"
	ckafkaqueue "github.com/akarso/shopanda/plugins/core/kafkaqueue"
	cmanualpay "github.com/akarso/shopanda/plugins/core/manualpay"
	coremeili "github.com/akarso/shopanda/plugins/core/meilisearch"
	corepostgres "github.com/akarso/shopanda/plugins/core/postgres"
	crabbitmq "github.com/akarso/shopanda/plugins/core/rabbitmqqueue"
	crediscache "github.com/akarso/shopanda/plugins/core/rediscache"
	credisqueue "github.com/akarso/shopanda/plugins/core/redisqueue"
	csqsqueue "github.com/akarso/shopanda/plugins/core/sqsqueue"
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
	} else if cfg.CoreRedisCacheEnabled() {
		registry.Register(crediscache.NewCachePlugin())
	}
	if cfg.CorePostgresQueueEnabled() {
		registry.Register(corepostgres.NewQueuePlugin())
	} else if cfg.CoreRedisQueueEnabled() {
		registry.Register(credisqueue.NewQueuePlugin())
	} else if cfg.CoreRabbitMQQueueEnabled() {
		registry.Register(crabbitmq.NewQueuePlugin())
	} else if cfg.CoreKafkaQueueEnabled() {
		registry.Register(ckafkaqueue.NewQueuePlugin())
	} else if cfg.CoreSQSQueueEnabled() {
		registry.Register(csqsqueue.NewQueuePlugin())
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
	if cfg.Plugins.GraphQL.Enabled {
		registry.Register(cgraphql.NewPlugin())
	}
}
