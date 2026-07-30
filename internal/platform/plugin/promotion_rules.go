package plugin

import (
	"context"
	"fmt"

	domainpricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/promotion"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/pkg/extapi"
)

// PromotionRules exposes promotion rule evaluator registration to plugins during Init.
type PromotionRules struct {
	registry   *promotion.EvaluatorRegistry
	registrant string
}

// SetPromotionEvaluatorRegistry wires the shared promotion evaluator registry before plugin Init.
func (a *App) SetPromotionEvaluatorRegistry(registry *promotion.EvaluatorRegistry) {
	if registry == nil {
		panic("plugin: promotion evaluator registry must not be nil")
	}
	a.promotionEvaluatorsMu.Lock()
	defer a.promotionEvaluatorsMu.Unlock()
	a.promotionEvaluators = registry
}

// PromotionEvaluatorRegistry returns the shared promotion evaluator registry.
func (a *App) PromotionEvaluatorRegistry() *promotion.EvaluatorRegistry {
	a.promotionEvaluatorsMu.RLock()
	defer a.promotionEvaluatorsMu.RUnlock()
	return a.promotionEvaluators
}

// PromotionRules returns plugin-facing promotion rule registration scoped to registrant.
// Panics when SetPromotionEvaluatorRegistry was not called before plugin Init — the registry
// must be the same instance passed to catalog/cart promotion pricing steps.
func (a *App) PromotionRules(registrant string) *PromotionRules {
	a.promotionEvaluatorsMu.RLock()
	reg := a.promotionEvaluators
	a.promotionEvaluatorsMu.RUnlock()
	if reg == nil {
		panic("plugin: promotion evaluator registry not configured; call SetPromotionEvaluatorRegistry before plugin Init")
	}
	return &PromotionRules{registry: reg, registrant: registrant}
}

// RegisterCatalogCondition adds a catalog condition evaluator for ruleType.
func (p *PromotionRules) RegisterCatalogCondition(ruleType string, handler extapi.CatalogConditionHandler) error {
	if p == nil || p.registry == nil {
		return fmt.Errorf("plugin: promotion evaluator registry not configured")
	}
	if handler == nil {
		return fmt.Errorf("plugin: catalog condition handler must not be nil")
	}
	return p.registry.RegisterCatalogCondition(ruleType, p.registrant, func(ctx context.Context, config []byte, item *domainpricing.PricingItem) (bool, error) {
		return handler(ctx, config, toExtAPIPromotionItem(item))
	})
}

// RegisterCatalogAction adds a catalog action evaluator for ruleType.
func (p *PromotionRules) RegisterCatalogAction(ruleType string, handler extapi.CatalogActionHandler) error {
	if p == nil || p.registry == nil {
		return fmt.Errorf("plugin: promotion evaluator registry not configured")
	}
	if handler == nil {
		return fmt.Errorf("plugin: catalog action handler must not be nil")
	}
	return p.registry.RegisterCatalogAction(ruleType, p.registrant, func(ctx context.Context, config []byte, item *domainpricing.PricingItem, currency string) (shared.Money, error) {
		amount, err := handler(ctx, config, toExtAPIPromotionItem(item))
		if err != nil {
			return shared.Money{}, err
		}
		if amount < 0 {
			return shared.Money{}, fmt.Errorf("catalog action discount must not be negative")
		}
		return shared.NewMoney(amount, currency)
	})
}

// RegisterCartCondition adds a cart condition evaluator for ruleType.
func (p *PromotionRules) RegisterCartCondition(ruleType string, handler extapi.CartConditionHandler) error {
	if p == nil || p.registry == nil {
		return fmt.Errorf("plugin: promotion evaluator registry not configured")
	}
	if handler == nil {
		return fmt.Errorf("plugin: cart condition handler must not be nil")
	}
	return p.registry.RegisterCartCondition(ruleType, p.registrant, func(ctx context.Context, config []byte, subtotal shared.Money) (bool, error) {
		return handler(ctx, config, subtotal.Amount(), subtotal.Currency())
	})
}

// RegisterCartAction adds a cart action evaluator for ruleType.
func (p *PromotionRules) RegisterCartAction(ruleType string, handler extapi.CartActionHandler) error {
	if p == nil || p.registry == nil {
		return fmt.Errorf("plugin: promotion evaluator registry not configured")
	}
	if handler == nil {
		return fmt.Errorf("plugin: cart action handler must not be nil")
	}
	return p.registry.RegisterCartAction(ruleType, p.registrant, func(ctx context.Context, config []byte, subtotal shared.Money, currency string) (shared.Money, error) {
		amount, err := handler(ctx, config, subtotal.Amount(), currency)
		if err != nil {
			return shared.Money{}, err
		}
		if amount < 0 {
			return shared.Money{}, fmt.Errorf("cart action discount must not be negative")
		}
		return shared.NewMoney(amount, currency)
	})
}

func toExtAPIPromotionItem(item *domainpricing.PricingItem) *extapi.PromotionPricingItem {
	if item == nil {
		return &extapi.PromotionPricingItem{}
	}
	return &extapi.PromotionPricingItem{
		VariantID:   item.VariantID,
		Quantity:    item.Quantity,
		UnitAmount:  item.UnitPrice.Amount(),
		TotalAmount: item.Total.Amount(),
		Currency:    item.Total.Currency(),
	}
}
