package plugin

import (
	"fmt"

	"github.com/akarso/shopanda/internal/domain/store"
)

func (a *App) ensureTaxIdentifierTypes() *store.TaxIdentifierTypeRegistry {
	a.taxIdentifierTypesMu.Lock()
	defer a.taxIdentifierTypesMu.Unlock()
	if a.taxIdentifierTypes == nil {
		a.taxIdentifierTypes = store.DefaultTaxIdentifierTypeRegistry()
	}
	return a.taxIdentifierTypes
}

// RegisterTaxIdentifierType adds a plugin tax/company identifier type.
// Duplicate codes fail closed (error). Core catalog is seeded on first use.
func (a *App) RegisterTaxIdentifierType(t store.TaxIdentifierType) error {
	if a == nil {
		return fmt.Errorf("plugin: nil app")
	}
	return a.ensureTaxIdentifierTypes().Register(t)
}

// TaxIdentifierTypes returns the in-process tax identifier type registry
// (core catalog + plugin registrations).
func (a *App) TaxIdentifierTypes() *store.TaxIdentifierTypeRegistry {
	if a == nil {
		return store.DefaultTaxIdentifierTypeRegistry()
	}
	return a.ensureTaxIdentifierTypes()
}
