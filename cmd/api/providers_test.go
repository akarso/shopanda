package main

import (
	"context"
	"io"
	"testing"
	"time"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainconfig "github.com/akarso/shopanda/internal/domain/config"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/search"
	inredis "github.com/akarso/shopanda/internal/infrastructure/redis"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/metrics"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/internal/platform/ratelimit"
	"github.com/akarso/shopanda/plugins/core"
	"github.com/akarso/shopanda/plugins/maildemo"
	"github.com/alicebob/miniredis/v2"
)

type mockDiscoveryFacetConfigurer struct {
	codes []string
}

func (m *mockDiscoveryFacetConfigurer) ConfigureAttributeFacets(_ context.Context, codes []string) error {
	m.codes = append([]string(nil), codes...)
	return nil
}

type facetSearchEngine struct {
	mockDiscoveryFacetConfigurer
	noopSearchEngine
}

type noopSearchEngine struct{}

func (noopSearchEngine) Name() string                                         { return "noop" }
func (noopSearchEngine) IndexProduct(context.Context, search.Product) error   { return nil }
func (noopSearchEngine) RemoveProduct(context.Context, string) error          { return nil }
func (noopSearchEngine) IndexCategory(context.Context, search.Category) error { return nil }
func (noopSearchEngine) RemoveCategory(context.Context, string) error         { return nil }
func (noopSearchEngine) Search(context.Context, search.SearchQuery) (search.SearchResult, error) {
	return search.SearchResult{}, nil
}
func (noopSearchEngine) Suggest(context.Context, string, int) ([]search.Suggestion, error) {
	return nil, nil
}

type mockConfigRepoForFacetSync struct {
	store map[string]interface{}
}

func (m *mockConfigRepoForFacetSync) Get(_ context.Context, key string) (interface{}, error) {
	return m.store[key], nil
}
func (m *mockConfigRepoForFacetSync) Set(_ context.Context, key string, value interface{}) error {
	m.store[key] = value
	return nil
}
func (m *mockConfigRepoForFacetSync) SetMany(_ context.Context, entries map[string]interface{}) error {
	for k, v := range entries {
		m.store[k] = v
	}
	return nil
}
func (m *mockConfigRepoForFacetSync) Delete(_ context.Context, key string) error {
	delete(m.store, key)
	return nil
}
func (m *mockConfigRepoForFacetSync) All(_ context.Context) ([]domainconfig.Entry, error) {
	return nil, nil
}

func TestNewDiscoveryFacetSyncer_UsesConfigurer(t *testing.T) {
	ctx := context.Background()
	repo := &mockConfigRepoForFacetSync{store: map[string]interface{}{}}
	store := adminApp.NewAttributeStore(repo)
	if err := store.CreateAttribute(ctx, catalog.Attribute{
		Code: "color", Label: "Color", Type: catalog.AttributeTypeText, UseInLayeredNav: true,
	}); err != nil {
		t.Fatalf("CreateAttribute: %v", err)
	}

	engine := &facetSearchEngine{}
	syncer := newDiscoveryFacetSyncer(store, engine)
	if err := syncer.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(engine.codes) != 1 || engine.codes[0] != "color" {
		t.Fatalf("codes = %v, want [color]", engine.codes)
	}
}

func TestNewDiscoveryFacetSyncer_NoOpWithoutConfigurer(t *testing.T) {
	repo := &mockConfigRepoForFacetSync{store: map[string]interface{}{}}
	syncer := newDiscoveryFacetSyncer(adminApp.NewAttributeStore(repo), noopSearchEngine{})
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

func TestResolvePaymentRegistry_FromPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	cfg := &config.Config{
		Search: config.SearchConfig{Engine: "postgres"},
	}
	core.Register(reg, cfg)
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	reg.InitAll(app)

	payReg, err := resolvePaymentRegistry(app)
	if err != nil {
		t.Fatalf("resolvePaymentRegistry() error: %v", err)
	}
	if payReg.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 manual provider", payReg.Len())
	}
	p, err := payReg.Resolve("")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if p.Method() != payment.MethodManual {
		t.Fatalf("Method() = %q, want manual", p.Method())
	}
}

func TestResolvePaymentRegistry_ManualAndStripe(t *testing.T) {
	t.Setenv("SHOPANDA_PAYMENT_STRIPE_SECRET_KEY", "sk_test_example")

	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	cfg := &config.Config{
		Payment: config.PaymentConfig{
			Stripe: config.StripeConfig{Enabled: true},
		},
	}
	core.Register(reg, cfg)
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	reg.InitAll(app)

	payReg, err := resolvePaymentRegistry(app)
	if err != nil {
		t.Fatalf("resolvePaymentRegistry() error: %v", err)
	}
	if payReg.Len() != 2 {
		t.Fatalf("Len() = %d, want manual and stripe", payReg.Len())
	}

	stripe, err := payReg.Resolve(string(payment.MethodStripe))
	if err != nil {
		t.Fatalf("Resolve(stripe) error: %v", err)
	}
	if stripe.Method() != payment.MethodStripe {
		t.Fatalf("stripe Method() = %q", stripe.Method())
	}
}

func TestResolveMediaStorage_FromLocalPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	cfg := &config.Config{
		Media: config.MediaConfig{
			Storage: "local",
			Local: config.LocalStorageConfig{
				BasePath: "./public/media",
				BaseURL:  "/media",
			},
		},
	}
	core.Register(reg, cfg)
	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: cfg,
	}
	reg.InitAll(app)

	st, err := resolveMediaStorage(app, cfg)
	if err != nil {
		t.Fatalf("resolveMediaStorage() error: %v", err)
	}
	if st == nil {
		t.Fatal("resolveMediaStorage() returned nil storage")
	}
}

func TestResolveMailer_FromPlugin(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)
	cfg := &config.Config{
		Plugins: config.PluginsConfig{
			MailDemo: config.MailDemoPluginConfig{Enabled: true},
		},
		Mail: config.MailConfig{Driver: "smtp"},
	}
	reg.Register(maildemo.New())
	app := &plugin.App{Logger: log, Bus: event.NewBus(log), Config: cfg}
	if summary := reg.InitAll(app); summary.Failed != 0 || summary.Initialized != 1 {
		t.Fatalf("InitAll() summary = %+v", summary)
	}

	mailer, err := resolveMailer(app, cfg)
	if err != nil {
		t.Fatalf("resolveMailer() error: %v", err)
	}
	if err := mailer.Send(context.Background(), mail.Message{
		To:      "user@example.com",
		Subject: "Test",
		Body:    "Hello",
	}); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
}

func TestResolveMailer_CoreSMTPDefault(t *testing.T) {
	app := &plugin.App{}
	cfg := &config.Config{
		Mail: config.MailConfig{
			Driver: "smtp",
			SMTP:   config.SMTPConfig{Host: "localhost", Port: 25, From: "shop@localhost"},
		},
	}
	mailer, err := resolveMailer(app, cfg)
	if err != nil {
		t.Fatalf("resolveMailer() error: %v", err)
	}
	if mailer == nil {
		t.Fatal("resolveMailer() returned nil")
	}
}

func TestResolveRateLimitFactory_DisabledSkipsRedis(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimitConfig{Enabled: false, Driver: "redis"}}
	f, closeFn, err := resolveRateLimitFactory(cfg, logger.NewWithWriter(io.Discard, "error"), metrics.Noop())
	if err != nil {
		t.Fatalf("disabled redis driver should not dial: %v", err)
	}
	t.Cleanup(closeFn)
	if !f.New(ratelimit.Spec{Name: "default", Rate: 1, Burst: 1}).Allow(context.Background(), "k").Allowed {
		t.Fatal("memory factory should admit")
	}
}

func TestResolveRateLimitFactory_UnknownDriver(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimitConfig{Enabled: true, Driver: "memcached"}}
	_, _, err := resolveRateLimitFactory(cfg, logger.NewWithWriter(io.Discard, "error"), metrics.Noop())
	if err == nil {
		t.Fatal("expected error for unknown driver")
	}
}

func TestResolveRateLimitFactory_RedisMissingURL(t *testing.T) {
	cfg := &config.Config{RateLimit: config.RateLimitConfig{Enabled: true, Driver: "redis"}}
	_, _, err := resolveRateLimitFactory(cfg, logger.NewWithWriter(io.Discard, "error"), metrics.Noop())
	if err == nil {
		t.Fatal("expected error when redis URL is missing")
	}
}

func TestResolveRateLimitFactory_DedicatedURLBeatsCacheURL(t *testing.T) {
	cacheMR, err := miniredis.Run()
	if err != nil {
		t.Fatalf("cache miniredis: %v", err)
	}
	t.Cleanup(cacheMR.Close)
	limMR, err := miniredis.Run()
	if err != nil {
		t.Fatalf("limiter miniredis: %v", err)
	}
	t.Cleanup(limMR.Close)
	cfg := &config.Config{
		RateLimit: config.RateLimitConfig{
			Enabled: true,
			Driver:  "redis",
			Redis:   config.RateLimitRedisConfig{URL: "redis://" + limMR.Addr()},
		},
		Cache: config.CacheConfig{Redis: config.RedisCacheConfig{URL: "redis://" + cacheMR.Addr(), KeyPrefix: "shopanda"}},
	}
	f, closeFn, err := resolveRateLimitFactory(cfg, logger.NewWithWriter(io.Discard, "error"), metrics.Noop())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Cleanup(closeFn)
	owned, ok := f.(*inredis.LimiterFactory)
	if !ok {
		t.Fatalf("factory type %T", f)
	}
	if got := owned.Client().Options().Addr; got != limMR.Addr() {
		t.Fatalf("limiter Addr = %q, want dedicated %q (not cache %q)", got, limMR.Addr(), cacheMR.Addr())
	}
}

func TestResolveRateLimitFactory_DedicatedURLWithoutCacheURL(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	cfg := &config.Config{
		RateLimit: config.RateLimitConfig{
			Enabled: true,
			Driver:  "redis",
			Redis:   config.RateLimitRedisConfig{URL: "redis://" + mr.Addr()},
		},
	}
	f, closeFn, err := resolveRateLimitFactory(cfg, logger.NewWithWriter(io.Discard, "error"), metrics.Noop())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Cleanup(closeFn)
	owned, ok := f.(*inredis.LimiterFactory)
	if !ok {
		t.Fatalf("factory type %T", f)
	}
	if got := owned.Client().Options().Addr; got != mr.Addr() {
		t.Fatalf("limiter Addr = %q, want %q", got, mr.Addr())
	}
}

func TestResolveRateLimitFactory_DedicatedClientDoesNotCloseCache(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	url := "redis://" + mr.Addr()
	store, err := inredis.New(inredis.Config{URL: url, KeyPrefix: "shopanda"})
	if err != nil {
		t.Fatalf("New cache: %v", err)
	}
	cfg := &config.Config{
		RateLimit: config.RateLimitConfig{Enabled: true, Driver: "redis"},
		Cache:     config.CacheConfig{Redis: config.RedisCacheConfig{URL: url, KeyPrefix: "shopanda"}},
	}
	f, closeFn, err := resolveRateLimitFactory(cfg, logger.NewWithWriter(io.Discard, "error"), metrics.Noop())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	owned, ok := f.(*inredis.LimiterFactory)
	if !ok {
		t.Fatalf("factory type %T", f)
	}
	if owned.Client() == store.Client() {
		t.Fatal("limiter must use a dedicated client, not the cache pool")
	}
	closeFn()
	if err := store.Client().Ping(context.Background()).Err(); err != nil {
		t.Fatalf("cache client should stay open after limiter close: %v", err)
	}
}

func TestResolveRateLimitFactory_OwnedClientCloses(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	cfg := &config.Config{
		RateLimit: config.RateLimitConfig{Enabled: true, Driver: "redis"},
		Cache:     config.CacheConfig{Redis: config.RedisCacheConfig{URL: "redis://" + mr.Addr(), KeyPrefix: "shopanda"}},
	}
	f, closeFn, err := resolveRateLimitFactory(cfg, logger.NewWithWriter(io.Discard, "error"), metrics.Noop())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	owned, ok := f.(*inredis.LimiterFactory)
	if !ok {
		t.Fatalf("factory type %T", f)
	}
	closeFn()
	if err := owned.Client().Ping(context.Background()).Err(); err == nil {
		t.Fatal("owned client should be closed")
	}
}

func TestResolveRateLimitFactory_UnreachableRedisFailOpen(t *testing.T) {
	cfg := &config.Config{
		RateLimit: config.RateLimitConfig{Enabled: true, Driver: "redis", OnError: "open"},
		Cache:     config.CacheConfig{Redis: config.RedisCacheConfig{URL: "redis://127.0.0.1:1", KeyPrefix: "shopanda"}},
	}
	f, closeFn, err := resolveRateLimitFactory(cfg, logger.NewWithWriter(io.Discard, "error"), metrics.Noop())
	if err != nil {
		t.Fatalf("fail-open should start without Redis: %v", err)
	}
	t.Cleanup(closeFn)
	start := time.Now()
	d := f.New(ratelimit.Spec{Name: "default", Rate: 10, Burst: 10}).Allow(context.Background(), "k")
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("open circuit should skip Redis, took %v", time.Since(start))
	}
	if !d.Allowed || !d.BackendError {
		t.Fatalf("decision = %+v, want fail-open while circuit is held", d)
	}
}

func TestResolveRateLimitFactory_UnreachableRedisPerRouteClosed(t *testing.T) {
	cfg := &config.Config{
		RateLimit: config.RateLimitConfig{
			Enabled: true,
			Driver:  "redis",
			OnError: "open",
			PerRoute: []config.RouteRateLimitRule{
				{PathPrefix: "/api/v1/auth/login", Rate: 1, Burst: 2, OnError: "closed"},
			},
		},
		Cache: config.CacheConfig{Redis: config.RedisCacheConfig{URL: "redis://127.0.0.1:1", KeyPrefix: "shopanda"}},
	}
	f, closeFn, err := resolveRateLimitFactory(cfg, logger.NewWithWriter(io.Discard, "error"), metrics.Noop())
	if err != nil {
		t.Fatalf("global open should start without Redis: %v", err)
	}
	t.Cleanup(closeFn)
	start := time.Now()
	closed := f.New(ratelimit.Spec{Name: "route:/api/v1/auth/login", Rate: 1, Burst: 2, OnError: ratelimit.OnErrorClosed}).Allow(context.Background(), "k")
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("open circuit should skip Redis, took %v", time.Since(start))
	}
	if closed.Allowed || !closed.BackendError {
		t.Fatalf("per-route closed = %+v, want deny during boot hold", closed)
	}
	open := f.New(ratelimit.Spec{Name: "default", Rate: 10, Burst: 10}).Allow(context.Background(), "k")
	if !open.Allowed || !open.BackendError {
		t.Fatalf("global open = %+v, want admit during boot hold", open)
	}
}

func TestResolveRateLimitFactory_UnreachableRedisFailClosed(t *testing.T) {
	cfg := &config.Config{
		RateLimit: config.RateLimitConfig{Enabled: true, Driver: "redis", OnError: "closed"},
		Cache:     config.CacheConfig{Redis: config.RedisCacheConfig{URL: "redis://127.0.0.1:1", KeyPrefix: "shopanda"}},
	}
	_, _, err := resolveRateLimitFactory(cfg, logger.NewWithWriter(io.Discard, "error"), metrics.Noop())
	if err == nil {
		t.Fatal("fail-closed should refuse to start when Redis is unreachable")
	}
}
