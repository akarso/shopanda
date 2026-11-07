package plugin

import (
	"database/sql"
	"fmt"

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

// BindQueue attaches the composition-root-resolved jobs.Queue for
// Integration.Enqueue. Distinct from RegisterQueue (plugin provider slot):
// Enqueue reads only the bound queue, so calls during Init always fail with
// ErrSyncJobQueueUnbound until the composition root binds after InitAll.
// When a plugin already RegisterQueue'd, queue must be that same instance
// (so a mistaken second NewJobQueue cannot silently split cron/Enqueue from
// the worker). Idempotent when the same instance is already bound.
func (a *App) BindQueue(queue jobs.Queue) error {
	if a == nil {
		return fmt.Errorf("plugin: BindQueue: app is nil")
	}
	if queue == nil {
		return fmt.Errorf("plugin: BindQueue: queue must not be nil")
	}
	if a.queue != nil && a.queue != queue {
		return fmt.Errorf("plugin: BindQueue: enqueue queue must match plugin-registered queue")
	}
	if a.boundQueue != nil && a.boundQueue != queue {
		return fmt.Errorf("plugin: BindQueue: enqueue queue already bound to a different instance")
	}
	a.boundQueue = queue
	return nil
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

// BindMediaStorage attaches the composition-root-resolved media.Storage for
// product plugins and late readers. Distinct from RegisterMediaStorage (plugin
// provider slot during Init): Init-time MediaStorage() only sees registered
// backends, while BoundMediaStorage() returns the single instance the root
// resolved (plugin-registered or fallback local/S3). When a plugin already
// RegisterMediaStorage'd, storage must be that same instance. Idempotent when
// the same instance is already bound.
func (a *App) BindMediaStorage(storage media.Storage) error {
	if a == nil {
		return fmt.Errorf("plugin: BindMediaStorage: app is nil")
	}
	if storage == nil {
		return fmt.Errorf("plugin: BindMediaStorage: storage must not be nil")
	}
	if a.mediaStorage != nil && a.mediaStorage != storage {
		return fmt.Errorf("plugin: BindMediaStorage: storage must match plugin-registered media")
	}
	if a.boundMedia != nil && a.boundMedia != storage {
		return fmt.Errorf("plugin: BindMediaStorage: media already bound to a different instance")
	}
	a.boundMedia = storage
	return nil
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

// BoundMediaStorage returns the composition-root media storage when
// BindMediaStorage has run, otherwise the plugin RegisterMediaStorage
// instance. Prefer this in product plugins so Init-order and fallback
// resolve paths cannot split readers onto a second local root.
func (a *App) BoundMediaStorage() (media.Storage, bool) {
	if a == nil {
		return nil, false
	}
	if a.boundMedia != nil {
		return a.boundMedia, true
	}
	if a.mediaStorage != nil {
		return a.mediaStorage, true
	}
	return nil, false
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
