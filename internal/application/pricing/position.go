package pricing

import (
	"fmt"
	"strings"

	domainpricing "github.com/akarso/shopanda/internal/domain/pricing"
)

// DefaultPluginStepPosition inserts plugin steps after base price (legacy behavior).
const DefaultPluginStepPosition = "after:base"

// Step position modes for plugin pricing registration.
const (
	StepPositionAfter   = "after"
	StepPositionBefore  = "before"
	StepPositionReplace = "replace"
)

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

// ParseStepPosition splits a position string into mode (after|before|replace) and anchor.
func ParseStepPosition(position string) (mode string, anchor string, err error) {
	position = strings.TrimSpace(position)
	if position == "" {
		position = DefaultPluginStepPosition
	}
	parts := strings.SplitN(position, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("pricing position %q: want before:<step>, after:<step>, or replace:<step>", position)
	}
	mode = strings.TrimSpace(parts[0])
	anchor = strings.TrimSpace(parts[1])
	switch mode {
	case StepPositionAfter, StepPositionBefore, StepPositionReplace:
	default:
		return "", "", fmt.Errorf("pricing position %q: mode must be before, after, or replace", position)
	}
	if anchor == "" {
		return "", "", fmt.Errorf("pricing position %q: anchor must not be empty", position)
	}
	resolved, err := ResolveAnchor(anchor)
	if err != nil {
		return "", "", err
	}
	return mode, resolved, nil
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

// MergePluginSteps inserts or replaces plugin steps in the core pipeline.
// Replace registrations substitute a core step by name (one winner per core step).
// Before/after registrations insert relative to core or replaced step names.
func MergePluginSteps(core []domainpricing.PricingStep, plugins []PluginStepRegistration) ([]domainpricing.PricingStep, error) {
	if len(plugins) == 0 {
		return append([]domainpricing.PricingStep(nil), core...), nil
	}

	replacements := make(map[string]domainpricing.PricingStep)
	inserts := make([]PluginStepRegistration, 0, len(plugins))

	for _, reg := range plugins {
		if reg.Step == nil {
			return nil, fmt.Errorf("pricing plugin step must not be nil")
		}
		mode, anchor, err := ParseStepPosition(reg.Position)
		if err != nil {
			return nil, fmt.Errorf("pricing plugin step %q: %w", reg.Step.Name(), err)
		}
		if mode == StepPositionReplace {
			if _, exists := replacements[anchor]; exists {
				return nil, fmt.Errorf("pricing replace %q: duplicate replacement", anchor)
			}
			replacements[anchor] = reg.Step
			continue
		}
		inserts = append(inserts, reg)
	}

	corePipeline := make([]domainpricing.PricingStep, 0, len(core))
	slotAnchors := make([]string, 0, len(core))
	for _, step := range core {
		name := step.Name()
		if replacement, ok := replacements[name]; ok {
			corePipeline = append(corePipeline, replacement)
			slotAnchors = append(slotAnchors, name)
			delete(replacements, name)
			continue
		}
		corePipeline = append(corePipeline, step)
		slotAnchors = append(slotAnchors, name)
	}
	for anchor := range replacements {
		return nil, fmt.Errorf("pricing replace %q: no core step with that name", anchor)
	}

	if len(inserts) == 0 {
		return corePipeline, nil
	}

	type batchKey struct {
		anchor string
		after  bool
	}
	batches := make(map[batchKey][]domainpricing.PricingStep)
	batchOrder := make([]batchKey, 0)

	for _, reg := range inserts {
		mode, anchor, err := ParseStepPosition(reg.Position)
		if err != nil {
			return nil, fmt.Errorf("pricing plugin step %q: %w", reg.Step.Name(), err)
		}
		k := batchKey{anchor: anchor, after: mode == StepPositionAfter}
		if _, seen := batches[k]; !seen {
			batchOrder = append(batchOrder, k)
		}
		batches[k] = append(batches[k], reg.Step)
	}

	out := make([]domainpricing.PricingStep, 0, len(corePipeline)+len(inserts))
	for i, step := range corePipeline {
		anchor := slotAnchors[i]
		for _, k := range batchOrder {
			if k.after || k.anchor != anchor {
				continue
			}
			out = append(out, batches[k]...)
		}
		out = append(out, step)
		for _, k := range batchOrder {
			if !k.after || k.anchor != anchor {
				continue
			}
			out = append(out, batches[k]...)
		}
	}
	return out, nil
}
