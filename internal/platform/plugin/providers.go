package plugin

import (
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/payment"
)

// Bootstrap holds infrastructure handles plugins need during Init.
// Populated by the application before InitAll.
type Bootstrap struct {
	DB *sql.DB
}

// RegisterSearchProvider registers the active search backend implementation.
// The provider must implement search.SearchEngine.
func (a *App) RegisterSearchProvider(provider any) {
	if provider == nil {
		panic("plugin: search provider must not be nil")
	}
	if a.searchProvider != nil {
		panic("plugin: search provider already registered")
	}
	a.searchProvider = provider
}

// RegisterCache registers the active cache backend implementation.
// The provider must implement cache.Cache.
func (a *App) RegisterCache(cache any) {
	if cache == nil {
		panic("plugin: cache must not be nil")
	}
	if a.cache != nil {
		panic("plugin: cache already registered")
	}
	a.cache = cache
}

// RegisterQueue registers the active job queue backend implementation.
// The provider must implement jobs.Queue.
func (a *App) RegisterQueue(queue any) {
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
// The provider must implement payment.Provider.
func (a *App) RegisterPaymentProvider(provider any) {
	if provider == nil {
		panic("plugin: payment provider must not be nil")
	}
	p, ok := provider.(payment.Provider)
	if !ok {
		panic(fmt.Sprintf("plugin: payment provider must implement payment.Provider, got %T", provider))
	}
	if a.paymentRegistry == nil {
		a.paymentRegistry = payment.NewProviderRegistry()
	}
	a.paymentRegistry.Register(p)
}

// RegisterMediaStorage registers the active media storage backend implementation.
// The provider must implement media.Storage.
func (a *App) RegisterMediaStorage(storage any) {
	if storage == nil {
		panic("plugin: media storage must not be nil")
	}
	if a.mediaStorage != nil {
		panic("plugin: media storage already registered")
	}
	a.mediaStorage = storage
}

// SearchProvider returns the registered search backend, if any.
func (a *App) SearchProvider() (any, bool) {
	if a.searchProvider == nil {
		return nil, false
	}
	return a.searchProvider, true
}

// Cache returns the registered cache backend, if any.
func (a *App) Cache() (any, bool) {
	if a.cache == nil {
		return nil, false
	}
	return a.cache, true
}

// Queue returns the registered job queue backend, if any.
func (a *App) Queue() (any, bool) {
	if a.queue == nil {
		return nil, false
	}
	return a.queue, true
}

// PaymentRegistry returns the registered payment providers, if any.
func (a *App) PaymentRegistry() *payment.ProviderRegistry {
	return a.paymentRegistry
}

// PaymentProvider returns the default payment provider for backward compatibility.
func (a *App) PaymentProvider() (any, bool) {
	if a.paymentRegistry == nil || a.paymentRegistry.Len() == 0 {
		return nil, false
	}
	p, err := a.paymentRegistry.Resolve("")
	if err != nil {
		return nil, false
	}
	return p, true
}

// MediaStorage returns the registered media storage backend, if any.
func (a *App) MediaStorage() (any, bool) {
	if a.mediaStorage == nil {
		return nil, false
	}
	return a.mediaStorage, true
}
