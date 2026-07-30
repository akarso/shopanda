package promotion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	domainpricing "github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// CatalogConditionEvaluator decides whether a catalog promotion condition matches a line item.
type CatalogConditionEvaluator func(ctx context.Context, config []byte, item *domainpricing.PricingItem) (bool, error)

// CatalogActionEvaluator computes a catalog line discount from plugin rule config.
type CatalogActionEvaluator func(ctx context.Context, config []byte, item *domainpricing.PricingItem, currency string) (shared.Money, error)

// CartConditionEvaluator decides whether a cart promotion condition matches the cart subtotal.
type CartConditionEvaluator func(ctx context.Context, config []byte, subtotal shared.Money) (bool, error)

// CartActionEvaluator computes a cart-level discount from plugin rule config.
type CartActionEvaluator func(ctx context.Context, config []byte, subtotal shared.Money, currency string) (shared.Money, error)

type registeredCatalogCondition struct {
	registrant string
	eval       CatalogConditionEvaluator
}

type registeredCatalogAction struct {
	registrant string
	eval       CatalogActionEvaluator
}

type registeredCartCondition struct {
	registrant string
	eval       CartConditionEvaluator
}

type registeredCartAction struct {
	registrant string
	eval       CartActionEvaluator
}

// EvaluatorRegistry stores plugin-registered promotion rule evaluators by JSON type string.
type EvaluatorRegistry struct {
	mu                sync.RWMutex
	catalogConditions map[string]registeredCatalogCondition
	catalogActions    map[string]registeredCatalogAction
	cartConditions    map[string]registeredCartCondition
	cartActions       map[string]registeredCartAction
}

// NewEvaluatorRegistry creates an empty promotion rule evaluator registry.
func NewEvaluatorRegistry() *EvaluatorRegistry {
	return &EvaluatorRegistry{
		catalogConditions: make(map[string]registeredCatalogCondition),
		catalogActions:    make(map[string]registeredCatalogAction),
		cartConditions:    make(map[string]registeredCartCondition),
		cartActions:       make(map[string]registeredCartAction),
	}
}

// RegisterCatalogCondition adds a catalog condition evaluator for ruleType.
func (r *EvaluatorRegistry) RegisterCatalogCondition(ruleType, registrant string, eval CatalogConditionEvaluator) error {
	return r.registerCatalogCondition(ruleType, registrant, eval)
}

// RegisterCatalogAction adds a catalog action evaluator for ruleType.
func (r *EvaluatorRegistry) RegisterCatalogAction(ruleType, registrant string, eval CatalogActionEvaluator) error {
	return r.registerCatalogAction(ruleType, registrant, eval)
}

// RegisterCartCondition adds a cart condition evaluator for ruleType.
func (r *EvaluatorRegistry) RegisterCartCondition(ruleType, registrant string, eval CartConditionEvaluator) error {
	return r.registerCartCondition(ruleType, registrant, eval)
}

// RegisterCartAction adds a cart action evaluator for ruleType.
func (r *EvaluatorRegistry) RegisterCartAction(ruleType, registrant string, eval CartActionEvaluator) error {
	return r.registerCartAction(ruleType, registrant, eval)
}

func (r *EvaluatorRegistry) registerCatalogCondition(ruleType, registrant string, eval CatalogConditionEvaluator) error {
	if r == nil {
		return fmt.Errorf("promotion: registry must not be nil")
	}
	ruleType = normalizeRuleType(ruleType)
	if ruleType == "" {
		return fmt.Errorf("promotion: rule type must not be empty")
	}
	registrant = normalizeRegistrant(registrant)
	if registrant == "" {
		return fmt.Errorf("promotion: registrant must not be empty")
	}
	if eval == nil {
		return fmt.Errorf("promotion: catalog condition evaluator must not be nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.catalogConditions[ruleType]; exists {
		return fmt.Errorf("promotion: catalog condition %q already registered", ruleType)
	}
	r.catalogConditions[ruleType] = registeredCatalogCondition{registrant: registrant, eval: eval}
	return nil
}

func (r *EvaluatorRegistry) registerCatalogAction(ruleType, registrant string, eval CatalogActionEvaluator) error {
	if r == nil {
		return fmt.Errorf("promotion: registry must not be nil")
	}
	ruleType = normalizeRuleType(ruleType)
	if ruleType == "" {
		return fmt.Errorf("promotion: rule type must not be empty")
	}
	registrant = normalizeRegistrant(registrant)
	if registrant == "" {
		return fmt.Errorf("promotion: registrant must not be empty")
	}
	if eval == nil {
		return fmt.Errorf("promotion: catalog action evaluator must not be nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.catalogActions[ruleType]; exists {
		return fmt.Errorf("promotion: catalog action %q already registered", ruleType)
	}
	r.catalogActions[ruleType] = registeredCatalogAction{registrant: registrant, eval: eval}
	return nil
}

func (r *EvaluatorRegistry) registerCartCondition(ruleType, registrant string, eval CartConditionEvaluator) error {
	if r == nil {
		return fmt.Errorf("promotion: registry must not be nil")
	}
	ruleType = normalizeRuleType(ruleType)
	if ruleType == "" {
		return fmt.Errorf("promotion: rule type must not be empty")
	}
	registrant = normalizeRegistrant(registrant)
	if registrant == "" {
		return fmt.Errorf("promotion: registrant must not be empty")
	}
	if eval == nil {
		return fmt.Errorf("promotion: cart condition evaluator must not be nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.cartConditions[ruleType]; exists {
		return fmt.Errorf("promotion: cart condition %q already registered", ruleType)
	}
	r.cartConditions[ruleType] = registeredCartCondition{registrant: registrant, eval: eval}
	return nil
}

func (r *EvaluatorRegistry) registerCartAction(ruleType, registrant string, eval CartActionEvaluator) error {
	if r == nil {
		return fmt.Errorf("promotion: registry must not be nil")
	}
	ruleType = normalizeRuleType(ruleType)
	if ruleType == "" {
		return fmt.Errorf("promotion: rule type must not be empty")
	}
	registrant = normalizeRegistrant(registrant)
	if registrant == "" {
		return fmt.Errorf("promotion: registrant must not be empty")
	}
	if eval == nil {
		return fmt.Errorf("promotion: cart action evaluator must not be nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.cartActions[ruleType]; exists {
		return fmt.Errorf("promotion: cart action %q already registered", ruleType)
	}
	r.cartActions[ruleType] = registeredCartAction{registrant: registrant, eval: eval}
	return nil
}

// HasCatalogCondition reports whether ruleType is registered.
func (r *EvaluatorRegistry) HasCatalogCondition(ruleType string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.catalogConditions[normalizeRuleType(ruleType)]
	return ok
}

// HasCatalogAction reports whether ruleType is registered.
func (r *EvaluatorRegistry) HasCatalogAction(ruleType string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.catalogActions[normalizeRuleType(ruleType)]
	return ok
}

// HasCartCondition reports whether ruleType is registered.
func (r *EvaluatorRegistry) HasCartCondition(ruleType string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.cartConditions[normalizeRuleType(ruleType)]
	return ok
}

// HasCartAction reports whether ruleType is registered.
func (r *EvaluatorRegistry) HasCartAction(ruleType string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.cartActions[normalizeRuleType(ruleType)]
	return ok
}

// EvalCatalogCondition runs a registered catalog condition evaluator.
func (r *EvaluatorRegistry) EvalCatalogCondition(ctx context.Context, ruleType string, config []byte, item *domainpricing.PricingItem) (bool, error) {
	if r == nil {
		return false, fmt.Errorf("promotion: catalog condition %q: no registry configured", ruleType)
	}
	r.mu.RLock()
	reg, ok := r.catalogConditions[normalizeRuleType(ruleType)]
	r.mu.RUnlock()
	if !ok {
		return false, fmt.Errorf("promotion: catalog condition %q: not registered", ruleType)
	}
	return reg.eval(ctx, config, item)
}

// EvalCatalogAction runs a registered catalog action evaluator.
func (r *EvaluatorRegistry) EvalCatalogAction(ctx context.Context, ruleType string, config []byte, item *domainpricing.PricingItem, currency string) (shared.Money, error) {
	if r == nil {
		return shared.Money{}, fmt.Errorf("promotion: catalog action %q: no registry configured", ruleType)
	}
	r.mu.RLock()
	reg, ok := r.catalogActions[normalizeRuleType(ruleType)]
	r.mu.RUnlock()
	if !ok {
		return shared.Money{}, fmt.Errorf("promotion: catalog action %q: not registered", ruleType)
	}
	return reg.eval(ctx, config, item, currency)
}

// EvalCartCondition runs a registered cart condition evaluator.
func (r *EvaluatorRegistry) EvalCartCondition(ctx context.Context, ruleType string, config []byte, subtotal shared.Money) (bool, error) {
	if r == nil {
		return false, fmt.Errorf("promotion: cart condition %q: no registry configured", ruleType)
	}
	r.mu.RLock()
	reg, ok := r.cartConditions[normalizeRuleType(ruleType)]
	r.mu.RUnlock()
	if !ok {
		return false, fmt.Errorf("promotion: cart condition %q: not registered", ruleType)
	}
	return reg.eval(ctx, config, subtotal)
}

// EvalCartAction runs a registered cart action evaluator.
func (r *EvaluatorRegistry) EvalCartAction(ctx context.Context, ruleType string, config []byte, subtotal shared.Money, currency string) (shared.Money, error) {
	if r == nil {
		return shared.Money{}, fmt.Errorf("promotion: cart action %q: no registry configured", ruleType)
	}
	r.mu.RLock()
	reg, ok := r.cartActions[normalizeRuleType(ruleType)]
	r.mu.RUnlock()
	if !ok {
		return shared.Money{}, fmt.Errorf("promotion: cart action %q: not registered", ruleType)
	}
	return reg.eval(ctx, config, subtotal, currency)
}

// RuleTypeFromConfig reads the JSON "type" field from promotion rule config.
func RuleTypeFromConfig(data []byte) (string, error) {
	if len(data) == 0 || string(data) == "null" {
		return "", fmt.Errorf("promotion: rule config is required")
	}
	var cfg struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("promotion: decode rule type: %w", err)
	}
	ruleType := normalizeRuleType(cfg.Type)
	if ruleType == "" {
		return "", fmt.Errorf("promotion: rule type must not be empty")
	}
	return ruleType, nil
}

func normalizeRuleType(ruleType string) string {
	return strings.TrimSpace(ruleType)
}

func normalizeRegistrant(registrant string) string {
	return strings.TrimSpace(registrant)
}
