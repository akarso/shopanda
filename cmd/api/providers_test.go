package main

import (
	"context"
	"io"
	"testing"

	adminApp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	domainconfig "github.com/akarso/shopanda/internal/domain/config"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/core"
	"github.com/akarso/shopanda/plugins/maildemo"
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

func (noopSearchEngine) Name() string                                       { return "noop" }
func (noopSearchEngine) IndexProduct(context.Context, search.Product) error { return nil }
func (noopSearchEngine) RemoveProduct(context.Context, string) error        { return nil }
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
