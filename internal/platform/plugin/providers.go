package plugin

import (
	"database/sql"

	"github.com/akarso/shopanda/internal/domain/cache"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/jobs"
	"github.com/akarso/shopanda/internal/domain/mail"
	"github.com/akarso/shopanda/internal/domain/media"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/domain/shipping"
	"github.com/akarso/shopanda/internal/domain/tax"
)

// Bootstrap holds handles plugins need during Init.
// Populated by the application before InitAll.
// Domain repositories are injected by the composition root so plugins do not
// import internal/infrastructure adapters.
type Bootstrap struct {
	DB        *sql.DB
	Customers customer.CustomerRepository
	Variants  catalog.VariantRepository
}

// RegisterSearchProvider registers the active search backend implementation.
func (a *App) RegisterSearchProvider(provider search.SearchEngine) {
	if provider == nil {
		panic("plugin: search provider must not be nil")
	}
	if a.searchProvider != nil {
		panic("plugin: search provider already registered")
	}
	a.searchProvider = provider
}

// RegisterCache registers the active cache backend implementation.
func (a *App) RegisterCache(c cache.Cache) {
	if c == nil {
		panic("plugin: cache must not be nil")
	}
	if a.cache != nil {
		panic("plugin: cache already registered")
	}
	a.cache = c
}

// RegisterQueue registers the active job queue backend implementation.
func (a *App) RegisterQueue(queue jobs.Queue) {
	if queue == nil {
		panic("plugin: queue must not be nil")
	}
	if a.queue != nil {
		panic("plugin: queue already registered")
	}
	a.queue = queue
}

// RegisterPaymentProvider registers a payment provider for its Method().
// Multiple providers may be registered when they use distinct methods.
func (a *App) RegisterPaymentProvider(provider payment.Provider) {
	if provider == nil {
		panic("plugin: payment provider must not be nil")
	}
	if a.paymentRegistry == nil {
		a.paymentRegistry = payment.NewProviderRegistry()
	}
	a.paymentRegistry.Register(provider)
}

// RegisterMediaStorage registers the active media storage backend implementation.
func (a *App) RegisterMediaStorage(storage media.Storage) {
	if storage == nil {
		panic("plugin: media storage must not be nil")
	}
	if a.mediaStorage != nil {
		panic("plugin: media storage already registered")
	}
	a.mediaStorage = storage
}

// RegisterTaxCalculator registers the active tax calculation implementation.
func (a *App) RegisterTaxCalculator(calculator tax.Calculator) {
	if calculator == nil {
		panic("plugin: tax calculator must not be nil")
	}
	if a.taxCalculator != nil {
		panic("plugin: tax calculator already registered")
	}
	a.taxCalculator = calculator
}

// RegisterShippingRateProvider registers a shipping rate provider.
func (a *App) RegisterShippingRateProvider(provider shipping.Provider) {
	if provider == nil {
		panic("plugin: shipping rate provider must not be nil")
	}
	if a.shippingRegistry == nil {
		a.shippingRegistry = shipping.NewProviderRegistry()
	}
	a.shippingRegistry.Register(provider)
}

// RegisterMailSender registers the active mail delivery implementation.
func (a *App) RegisterMailSender(mailer mail.Mailer) {
	if mailer == nil {
		panic("plugin: mail sender must not be nil")
	}
	if a.mailSender != nil {
		panic("plugin: mail sender already registered")
	}
	a.mailSender = mailer
}

// SearchProvider returns the registered search backend, if any.
func (a *App) SearchProvider() (search.SearchEngine, bool) {
	if a.searchProvider == nil {
		return nil, false
	}
	return a.searchProvider, true
}

// Cache returns the registered cache backend, if any.
func (a *App) Cache() (cache.Cache, bool) {
	if a.cache == nil {
		return nil, false
	}
	return a.cache, true
}

// Queue returns the registered job queue backend, if any.
func (a *App) Queue() (jobs.Queue, bool) {
	if a.queue == nil {
		return nil, false
	}
	return a.queue, true
}

// PaymentRegistry returns the registered payment providers, if any.
func (a *App) PaymentRegistry() *payment.ProviderRegistry {
	return a.paymentRegistry
}

// MediaStorage returns the registered media storage backend, if any.
func (a *App) MediaStorage() (media.Storage, bool) {
	if a.mediaStorage == nil {
		return nil, false
	}
	return a.mediaStorage, true
}

// TaxCalculator returns the registered tax calculator, if any.
func (a *App) TaxCalculator() (tax.Calculator, bool) {
	if a.taxCalculator == nil {
		return nil, false
	}
	return a.taxCalculator, true
}

// ShippingRegistry returns the registered shipping providers, if any.
func (a *App) ShippingRegistry() *shipping.ProviderRegistry {
	return a.shippingRegistry
}

// MailSender returns the registered mail sender, if any.
func (a *App) MailSender() (mail.Mailer, bool) {
	if a.mailSender == nil {
		return nil, false
	}
	return a.mailSender, true
}
