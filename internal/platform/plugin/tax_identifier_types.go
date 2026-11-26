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
// The registry normalizes the type (snake code, ISO countries, region allow-list)
// before the duplicate check. Duplicate codes and post-Freeze calls fail closed.
func (a *App) RegisterTaxIdentifierType(t store.TaxIdentifierType) error {
	if a == nil {
		return fmt.Errorf("plugin: nil app")
	}
	return a.ensureTaxIdentifierTypes().Register(t)
}

// TaxIdentifierTypes returns the in-process tax identifier type registry
// (core catalog + plugin registrations). Panics on a nil App.
func (a *App) TaxIdentifierTypes() *store.TaxIdentifierTypeRegistry {
	if a == nil {
		panic("plugin: TaxIdentifierTypes: nil app")
	}
	return a.ensureTaxIdentifierTypes()
}

// FreezeTaxIdentifierTypes seals plugin tax-ID type registration after InitAll.
func (a *App) FreezeTaxIdentifierTypes() {
	if a == nil {
		return
	}
	a.ensureTaxIdentifierTypes().Freeze()
}
