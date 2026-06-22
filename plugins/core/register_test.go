package core_test

import (
	"io"
	"testing"

	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/core"
)

func TestRegister_MeilisearchEngineRegistersMeiliPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Search: config.SearchConfig{Engine: "meilisearch"},
		Queue:  config.QueueConfig{Driver: "postgres"},
		Cache:  config.CacheConfig{Driver: "postgres"},
	}
	core.Register(reg, cfg)

	var hasMeili, hasPostgresSearch bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/meilisearch-search":
			hasMeili = true
		case "core/postgres-search":
			hasPostgresSearch = true
		}
	}
	if !hasMeili {
		t.Fatal("expected core/meilisearch-search to be registered")
	}
	if hasPostgresSearch {
		t.Fatal("postgres search plugin should not register when search.engine=meilisearch")
	}
}

func TestRegister_PostgresEngineRegistersPostgresSearchOnly(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Search: config.SearchConfig{Engine: "postgres"},
		Queue:  config.QueueConfig{Driver: "postgres"},
		Cache:  config.CacheConfig{Driver: "postgres"},
	}
	core.Register(reg, cfg)

	for _, e := range reg.Entries() {
		if e.Name == "core/meilisearch-search" {
			t.Fatal("meilisearch plugin should not register when search.engine=postgres")
		}
	}
}

func TestRegister_ExplicitPostgresSearchOverridesMeilisearchEngine(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	enabled := true
	cfg := &config.Config{
		Search: config.SearchConfig{Engine: "meilisearch"},
		Plugins: config.PluginsConfig{
			Core: config.CorePluginsConfig{
				PostgresSearch: &enabled,
			},
		},
	}
	core.Register(reg, cfg)

	var hasMeili, hasPostgresSearch bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/meilisearch-search":
			hasMeili = true
		case "core/postgres-search":
			hasPostgresSearch = true
		}
	}
	if !hasPostgresSearch {
		t.Fatal("expected postgres search plugin when explicitly enabled")
	}
	if hasMeili {
		t.Fatal("meilisearch plugin must not register when postgres search wins")
	}
}

func TestRegister_AlwaysRegistersManualPaymentPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Search: config.SearchConfig{Engine: "postgres"},
	}
	core.Register(reg, cfg)

	var hasManual bool
	for _, e := range reg.Entries() {
		if e.Name == "core/manualpay" {
			hasManual = true
		}
	}
	if !hasManual {
		t.Fatal("expected core/manualpay to always register")
	}
}

func TestRegister_ManualAndStripeBothRegisterWhenEnabled(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Payment: config.PaymentConfig{
			Stripe: config.StripeConfig{Enabled: true},
		},
	}
	core.Register(reg, cfg)

	var hasManual, hasStripe bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/manualpay":
			hasManual = true
		case "core/stripe":
			hasStripe = true
		}
	}
	if !hasManual || !hasStripe {
		t.Fatalf("entries = %v, want manual and stripe plugins", reg.Entries())
	}
}

func TestRegister_RabbitMQQueueDriverRegistersRabbitMQPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Queue: config.QueueConfig{Driver: "rabbitmq"},
	}
	core.Register(reg, cfg)

	var hasRabbitMQ, hasPostgresQueue, hasRedis bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/rabbitmq-queue":
			hasRabbitMQ = true
		case "core/postgres-queue":
			hasPostgresQueue = true
		case "core/redis-queue":
			hasRedis = true
		}
	}
	if !hasRabbitMQ {
		t.Fatal("expected core/rabbitmq-queue to register when queue.driver=rabbitmq")
	}
	if hasPostgresQueue || hasRedis {
		t.Fatal("only rabbitmq queue plugin should register when queue.driver=rabbitmq")
	}
}

func TestRegister_RedisQueueDriverRegistersRedisPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Queue: config.QueueConfig{Driver: "redis"},
	}
	core.Register(reg, cfg)

	var hasRedis, hasPostgresQueue bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/redis-queue":
			hasRedis = true
		case "core/postgres-queue":
			hasPostgresQueue = true
		}
	}
	if !hasRedis {
		t.Fatal("expected core/redis-queue to register when queue.driver=redis")
	}
	if hasPostgresQueue {
		t.Fatal("postgres queue plugin should not register when queue.driver=redis")
	}
}

func TestRegister_PostgresQueueByDefault(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Queue: config.QueueConfig{},
	}
	core.Register(reg, cfg)

	var hasPostgresQueue, hasRedis bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/postgres-queue":
			hasPostgresQueue = true
		case "core/redis-queue":
			hasRedis = true
		}
	}
	if !hasPostgresQueue {
		t.Fatal("expected core/postgres-queue to register by default")
	}
	if hasRedis {
		t.Fatal("redis queue plugin should not register when queue driver is unset")
	}
}

func TestRegister_ExplicitPostgresQueueOverridesRedisDriver(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	enabled := true
	cfg := &config.Config{
		Queue: config.QueueConfig{Driver: "redis"},
		Plugins: config.PluginsConfig{
			Core: config.CorePluginsConfig{
				PostgresQueue: &enabled,
			},
		},
	}
	core.Register(reg, cfg)

	var hasPostgresQueue, hasRedis bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/postgres-queue":
			hasPostgresQueue = true
		case "core/redis-queue":
			hasRedis = true
		}
	}
	if !hasPostgresQueue {
		t.Fatal("expected postgres queue plugin when explicitly enabled")
	}
	if hasRedis {
		t.Fatal("redis queue plugin must not register when postgres queue wins")
	}
}

func TestRegister_RedisCacheDriverRegistersRedisPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Cache: config.CacheConfig{Driver: "redis"},
	}
	core.Register(reg, cfg)

	var hasRedis, hasPostgresCache bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/redis-cache":
			hasRedis = true
		case "core/postgres-cache":
			hasPostgresCache = true
		}
	}
	if !hasRedis {
		t.Fatal("expected core/redis-cache to register when cache.driver=redis")
	}
	if hasPostgresCache {
		t.Fatal("postgres cache plugin should not register when cache.driver=redis")
	}
}

func TestRegister_PostgresCacheByDefault(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Cache: config.CacheConfig{},
	}
	core.Register(reg, cfg)

	var hasPostgresCache, hasRedis bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/postgres-cache":
			hasPostgresCache = true
		case "core/redis-cache":
			hasRedis = true
		}
	}
	if !hasPostgresCache {
		t.Fatal("expected core/postgres-cache to register by default")
	}
	if hasRedis {
		t.Fatal("redis cache plugin should not register when cache driver is unset")
	}
}

func TestRegister_LocalStorageByDefault(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Media: config.MediaConfig{},
	}
	core.Register(reg, cfg)

	var hasLocal, hasS3 bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/storage-local":
			hasLocal = true
		case "core/storage-s3":
			hasS3 = true
		}
	}
	if !hasLocal {
		t.Fatal("expected core/storage-local to register for local media storage")
	}
	if hasS3 {
		t.Fatal("s3 storage plugin should not register when media.storage=local")
	}
}

func TestRegister_S3StorageOnlyWhenConfigured(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	cfg := &config.Config{
		Media: config.MediaConfig{Storage: "s3"},
	}
	core.Register(reg, cfg)

	var hasLocal, hasS3 bool
	for _, e := range reg.Entries() {
		switch e.Name {
		case "core/storage-local":
			hasLocal = true
		case "core/storage-s3":
			hasS3 = true
		}
	}
	if !hasS3 {
		t.Fatal("expected core/storage-s3 to register when media.storage=s3")
	}
	if hasLocal {
		t.Fatal("local storage plugin should not register when media.storage=s3")
	}
}
