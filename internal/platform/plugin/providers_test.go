package plugin_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/domain/tax"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

type stubSearchProvider struct {
	name string
}

func (s *stubSearchProvider) Name() string { return s.name }
func (s *stubSearchProvider) IndexProduct(context.Context, search.Product) error {
	return nil
}
func (s *stubSearchProvider) RemoveProduct(context.Context, string) error { return nil }
func (s *stubSearchProvider) Search(context.Context, search.SearchQuery) (search.SearchResult, error) {
	return search.SearchResult{}, nil
}
func (s *stubSearchProvider) Suggest(context.Context, string, int) ([]search.Suggestion, error) {
	return nil, nil
}

var _ search.SearchEngine = (*stubSearchProvider)(nil)

type stubSearchPlugin struct {
	name     string
	provider search.SearchEngine
	initErr  error
}

func (p *stubSearchPlugin) Name() string {
	if p.name != "" {
		return p.name
	}
	return "stub/search"
}

func (p *stubSearchPlugin) Init(app *plugin.App) error {
	if p.initErr != nil {
		return p.initErr
	}
	app.RegisterSearchProvider(p.provider)
	return nil
}

func TestApp_RegisterSearchProvider(t *testing.T) {
	app := &plugin.App{}
	provider := &stubSearchProvider{name: "stub"}
	app.RegisterSearchProvider(provider)

	got, ok := app.SearchProvider()
	if !ok {
		t.Fatal("SearchProvider() ok = false, want true")
	}
	if got != provider {
		t.Fatalf("SearchProvider() = %v, want %v", got, provider)
	}
}

func TestApp_RegisterSearchProvider_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil search provider")
		}
	}()
	app := &plugin.App{}
	app.RegisterSearchProvider(nil)
}

func TestApp_RegisterSearchProvider_DoubleRegistrationPanics(t *testing.T) {
	app := &plugin.App{}
	app.RegisterSearchProvider(&stubSearchProvider{name: "first"})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for double search provider registration")
		}
	}()
	app.RegisterSearchProvider(&stubSearchProvider{name: "second"})
}

type stubTaxCalculator struct{}

func (stubTaxCalculator) Calculate(_ context.Context, _ *pricing.PricingContext) error {
	return nil
}

var _ tax.Calculator = stubTaxCalculator{}

func TestApp_RegisterTaxCalculator(t *testing.T) {
	app := &plugin.App{}
	calc := stubTaxCalculator{}
	app.RegisterTaxCalculator(calc)

	got, ok := app.TaxCalculator()
	if !ok {
		t.Fatal("TaxCalculator() ok = false, want true")
	}
	if got != calc {
		t.Fatalf("TaxCalculator() = %v, want %v", got, calc)
	}
}

func TestApp_RegisterTaxCalculator_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil tax calculator")
		}
	}()
	app := &plugin.App{}
	app.RegisterTaxCalculator(nil)
}

func TestApp_RegisterTaxCalculator_DoubleRegistrationPanics(t *testing.T) {
	app := &plugin.App{}
	app.RegisterTaxCalculator(stubTaxCalculator{})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for double tax calculator registration")
		}
	}()
	app.RegisterTaxCalculator(stubTaxCalculator{})
}

type stubCache struct{}

func (stubCache) Get(string, any) (bool, error)                    { return false, nil }
func (stubCache) Set(string, any, time.Duration) error             { return nil }
func (stubCache) Incr(string, int64, time.Duration) (int64, error) { return 0, nil }
func (stubCache) CompareAndSubtract(string, int64) (int64, error)  { return 0, nil }
func (stubCache) Delete(string) error                              { return nil }
func (stubCache) DeleteByPrefix(context.Context, string) error     { return nil }

var _ cache.Cache = stubCache{}

func TestApp_RegisterCache(t *testing.T) {
	app := &plugin.App{}
	c := stubCache{}
	app.RegisterCache(c)
	got, ok := app.Cache()
	if !ok || got != c {
		t.Fatalf("Cache() = (%v, %v)", got, ok)
	}
}

func TestApp_RegisterCache_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	(&plugin.App{}).RegisterCache(nil)
}

func TestApp_RegisterCache_DoubleRegistrationPanics(t *testing.T) {
	app := &plugin.App{}
	app.RegisterCache(stubCache{})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	app.RegisterCache(stubCache{})
}

type stubQueue struct{}

func (stubQueue) Enqueue(context.Context, jobs.Job) error    { return nil }
func (stubQueue) Dequeue(context.Context) (*jobs.Job, error) { return nil, nil }
func (stubQueue) Complete(context.Context, string) error     { return nil }
func (stubQueue) Fail(context.Context, string, error) error  { return nil }

var _ jobs.Queue = stubQueue{}

func TestApp_RegisterQueue(t *testing.T) {
	app := &plugin.App{}
	q := stubQueue{}
	app.RegisterQueue(q)
	got, ok := app.Queue()
	if !ok || got != q {
		t.Fatalf("Queue() = (%v, %v)", got, ok)
	}
}

func TestApp_RegisterQueue_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	(&plugin.App{}).RegisterQueue(nil)
}

func TestApp_RegisterQueue_DoubleRegistrationPanics(t *testing.T) {
	app := &plugin.App{}
	app.RegisterQueue(stubQueue{})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	app.RegisterQueue(stubQueue{})
}

type stubMedia struct{}

func (stubMedia) Name() string                 { return "stub" }
func (stubMedia) Save(string, io.Reader) error { return nil }
func (stubMedia) Delete(string) error          { return nil }
func (stubMedia) URL(string) string            { return "" }

var _ media.Storage = stubMedia{}

func TestApp_RegisterMediaStorage(t *testing.T) {
	app := &plugin.App{}
	s := stubMedia{}
	app.RegisterMediaStorage(s)
	got, ok := app.MediaStorage()
	if !ok || got != s {
		t.Fatalf("MediaStorage() = (%v, %v)", got, ok)
	}
}

func TestApp_RegisterMediaStorage_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	(&plugin.App{}).RegisterMediaStorage(nil)
}

func TestApp_RegisterMediaStorage_DoubleRegistrationPanics(t *testing.T) {
	app := &plugin.App{}
	app.RegisterMediaStorage(stubMedia{})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	app.RegisterMediaStorage(stubMedia{})
}

type stubMailer struct{}

func (stubMailer) Send(context.Context, mail.Message) error { return nil }

var _ mail.Mailer = stubMailer{}

func TestApp_RegisterMailSender(t *testing.T) {
	app := &plugin.App{}
	m := stubMailer{}
	app.RegisterMailSender(m)
	got, ok := app.MailSender()
	if !ok || got != m {
		t.Fatalf("MailSender() = (%v, %v)", got, ok)
	}
}

func TestApp_RegisterMailSender_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	(&plugin.App{}).RegisterMailSender(nil)
}

func TestApp_RegisterMailSender_DoubleRegistrationPanics(t *testing.T) {
	app := &plugin.App{}
	app.RegisterMailSender(stubMailer{})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	app.RegisterMailSender(stubMailer{})
}

type stubShipping struct{}

func (stubShipping) Method() shipping.ShippingMethod {
	return shipping.MethodFreeShipping
}
func (stubShipping) CalculateRate(context.Context, string, string, int) (shipping.ShippingRate, error) {
	return shipping.ShippingRate{Cost: shared.MustNewMoney(0, "USD")}, nil
}

var _ shipping.Provider = stubShipping{}

func TestApp_RegisterShippingRateProvider(t *testing.T) {
	app := &plugin.App{}
	app.RegisterShippingRateProvider(stubShipping{})
	reg := app.ShippingRegistry()
	if reg == nil || reg.Len() != 1 {
		t.Fatalf("ShippingRegistry() = %+v", reg)
	}
}

func TestApp_RegisterShippingRateProvider_NilPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	(&plugin.App{}).RegisterShippingRateProvider(nil)
}

func TestRegistry_StubCorePluginRegistersProvider(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	provider := &stubSearchProvider{name: "stub"}
	reg.Register(&stubSearchPlugin{provider: provider})

	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{},
	}
	reg.InitAll(app)

	got, ok := app.SearchProvider()
	if !ok {
		t.Fatal("SearchProvider() ok = false after stub plugin init")
	}
	if got != provider {
		t.Fatalf("SearchProvider() = %v, want %v", got, provider)
	}
}

func TestRegistry_FailedPluginDoesNotPanic(t *testing.T) {
	log := logger.NewWithWriter(io.Discard, "error")
	reg := plugin.NewRegistry(log)

	reg.Register(&stubSearchPlugin{name: "stub/failing", initErr: errors.New("boom")})
	reg.Register(&stubSearchPlugin{name: "stub/ok", provider: &stubSearchProvider{name: "ok"}})

	app := &plugin.App{
		Logger: log,
		Bus:    event.NewBus(log),
		Config: &config.Config{},
	}
	summary := reg.InitAll(app)

	if summary.Failed != 1 || summary.Initialized != 1 {
		t.Fatalf("summary = %+v, want 1 failed and 1 initialized", summary)
	}

	got, ok := app.SearchProvider()
	if !ok {
		t.Fatal("SearchProvider() ok = false, want provider from second plugin")
	}
	sp, ok := got.(*stubSearchProvider)
	if !ok || sp.name != "ok" {
		t.Fatalf("SearchProvider() = %T %v, want stub name ok", got, got)
	}
}

func TestCorePostgresSearchEnabled(t *testing.T) {
	cfg := &config.Config{Search: config.SearchConfig{Engine: "meilisearch"}}
	if cfg.CorePostgresSearchEnabled() {
		t.Fatal("CorePostgresSearchEnabled() = true, want false for meilisearch")
	}

	cfg.Search.Engine = "postgres"
	if !cfg.CorePostgresSearchEnabled() {
		t.Fatal("CorePostgresSearchEnabled() = false, want true for postgres")
	}

	disabled := false
	cfg.Plugins.Core.PostgresSearch = &disabled
	if cfg.CorePostgresSearchEnabled() {
		t.Fatal("CorePostgresSearchEnabled() = true, want false when explicitly disabled")
	}
}
