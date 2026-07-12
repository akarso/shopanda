package pricing

import (
	"fmt"
	"strings"

	domainpricing "github.com/akarso/shopanda/internal/domain/pricing"
)

// DefaultPluginStepPosition inserts plugin steps after base price (legacy behavior).
const DefaultPluginStepPosition = "after:base"

// CoreStepCatalog lists canonical core pricing pipeline step names (stable v0).
var CoreStepCatalog = []string{
	"base",
	"catalog_promotions",
	"cart_promotions",
	"tax",
	"finalize",
}

// anchorAliases maps ergonomic anchor names to canonical core step names.
var anchorAliases = map[string]string{
	"base_price":  "base",
	"discounts":   "catalog_promotions",
	"promotions":  "cart_promotions",
	"taxes":       "tax",
	"finalization": "finalize",
}

// PluginStepRegistration binds a plugin step to a pipeline position.
type PluginStepRegistration struct {
	Step     domainpricing.PricingStep
	Position string
}

// ParseStepPosition splits a position string into relative ("before"|"after") and anchor.
func ParseStepPosition(position string) (relative string, anchor string, err error) {
	position = strings.TrimSpace(position)
	if position == "" {
		position = DefaultPluginStepPosition
	}
	parts := strings.SplitN(position, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("pricing position %q: want before:<step> or after:<step>", position)
	}
	relative = strings.TrimSpace(parts[0])
	anchor = strings.TrimSpace(parts[1])
	if relative != "before" && relative != "after" {
		return "", "", fmt.Errorf("pricing position %q: relative must be before or after", position)
	}
	if anchor == "" {
		return "", "", fmt.Errorf("pricing position %q: anchor must not be empty", position)
	}
	resolved, err := ResolveAnchor(anchor)
	if err != nil {
		return "", "", err
	}
	return relative, resolved, nil
}

// ResolveAnchor normalizes an anchor name (aliases → canonical core step name).
func ResolveAnchor(anchor string) (string, error) {
	anchor = strings.TrimSpace(anchor)
	if anchor == "" {
		return "", fmt.Errorf("pricing anchor must not be empty")
	}
	if mapped, ok := anchorAliases[anchor]; ok {
		anchor = mapped
	}
	for _, name := range CoreStepCatalog {
		if anchor == name {
			return name, nil
		}
	}
	return "", fmt.Errorf("pricing anchor %q: unknown core step (catalog: %s)", anchor, strings.Join(CoreStepCatalog, ", "))
}

// MergePluginSteps inserts plugin steps into the core pipeline at declared positions.
// Multiple registrations at the same anchor run in registration order.
func MergePluginSteps(core []domainpricing.PricingStep, plugins []PluginStepRegistration) ([]domainpricing.PricingStep, error) {
	if len(plugins) == 0 {
		return append([]domainpricing.PricingStep(nil), core...), nil
	}

	type batchKey struct {
		anchor string
		after  bool
	}
	batches := make(map[batchKey][]domainpricing.PricingStep)
	batchOrder := make([]batchKey, 0)

	for _, reg := range plugins {
		if reg.Step == nil {
			return nil, fmt.Errorf("pricing plugin step must not be nil")
		}
		relative, anchor, err := ParseStepPosition(reg.Position)
		if err != nil {
			return nil, fmt.Errorf("pricing plugin step %q: %w", reg.Step.Name(), err)
		}
		k := batchKey{anchor: anchor, after: relative == "after"}
		if _, seen := batches[k]; !seen {
			batchOrder = append(batchOrder, k)
		}
		batches[k] = append(batches[k], reg.Step)
	}

	out := make([]domainpricing.PricingStep, 0, len(core)+len(plugins))
	for _, step := range core {
		name := step.Name()
		for _, k := range batchOrder {
			if k.after || k.anchor != name {
				continue
			}
			out = append(out, batches[k]...)
		}
		out = append(out, step)
		for _, k := range batchOrder {
			if !k.after || k.anchor != name {
				continue
			}
			out = append(out, batches[k]...)
		}
	}
	return out, nil
}
