package pluginsdk

import (
	"github.com/akarso/shopanda/pkg/extapi"
)

// Promotion registers custom promotion rule evaluators.
type Promotion struct {
	sdk *SDK
}

// Promotion returns promotion rule registration helpers for the SDK plugin.
func (s *SDK) Promotion() *Promotion {
	return &Promotion{sdk: s}
}

// RegisterCatalogCondition registers a catalog condition rule type.
func (p *Promotion) RegisterCatalogCondition(ruleType string, handler extapi.CatalogConditionHandler) error {
	return p.sdk.app.PromotionRules(p.sdk.plugin).RegisterCatalogCondition(ruleType, handler)
}

// RegisterCatalogAction registers a catalog action rule type.
func (p *Promotion) RegisterCatalogAction(ruleType string, handler extapi.CatalogActionHandler) error {
	return p.sdk.app.PromotionRules(p.sdk.plugin).RegisterCatalogAction(ruleType, handler)
}

// RegisterCartCondition registers a cart condition rule type.
func (p *Promotion) RegisterCartCondition(ruleType string, handler extapi.CartConditionHandler) error {
	return p.sdk.app.PromotionRules(p.sdk.plugin).RegisterCartCondition(ruleType, handler)
}

// RegisterCartAction registers a cart action rule type.
func (p *Promotion) RegisterCartAction(ruleType string, handler extapi.CartActionHandler) error {
	return p.sdk.app.PromotionRules(p.sdk.plugin).RegisterCartAction(ruleType, handler)
}
