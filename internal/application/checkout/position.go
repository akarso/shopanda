package checkout

import (
	"fmt"
	"strings"
)

// DefaultPluginStepPosition appends plugin steps after the last core checkout step (legacy behavior).
const DefaultPluginStepPosition = "end"

// Step position modes for plugin checkout registration.
const (
	StepPositionAfter  = "after"
	StepPositionBefore = "before"
)

// CoreStepCatalog lists canonical core checkout workflow step names (stable v0).
var CoreStepCatalog = []string{
	"validate_cart",
	"recalculate_pricing",
	"reserve_inventory",
	"create_order",
	"select_shipping",
	"initiate_payment",
}

// anchorAliases maps ergonomic anchor names to canonical core step names.
var anchorAliases = map[string]string{
	"validate":  "validate_cart",
	"pricing":   "recalculate_pricing",
	"inventory": "reserve_inventory",
	"order":     "create_order",
	"shipping":  "select_shipping",
	"payment":   "initiate_payment",
}

// PluginStepRegistration binds a plugin step to a workflow position.
type PluginStepRegistration struct {
	Step     Step
	Position string
}

// ParseStepPosition splits a position string into mode (after|before) and anchor.
// Bare "start" and "end" are supported shortcuts per CHECKOUT_WORKFLOW.md.
func ParseStepPosition(position string) (mode string, anchor string, err error) {
	position = strings.TrimSpace(position)
	if position == "" {
		position = DefaultPluginStepPosition
	}
	switch position {
	case "start":
		return StepPositionBefore, CoreStepCatalog[0], nil
	case "end":
		last := CoreStepCatalog[len(CoreStepCatalog)-1]
		return StepPositionAfter, last, nil
	}

	parts := strings.SplitN(position, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("checkout position %q: want start, end, before:<step>, or after:<step>", position)
	}
	mode = strings.TrimSpace(parts[0])
	anchor = strings.TrimSpace(parts[1])
	switch mode {
	case StepPositionAfter, StepPositionBefore:
	default:
		return "", "", fmt.Errorf("checkout position %q: mode must be before or after", position)
	}
	if anchor == "" {
		return "", "", fmt.Errorf("checkout position %q: anchor must not be empty", position)
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
		return "", fmt.Errorf("checkout anchor must not be empty")
	}
	if mapped, ok := anchorAliases[anchor]; ok {
		anchor = mapped
	}
	for _, name := range CoreStepCatalog {
		if anchor == name {
			return name, nil
		}
	}
	return "", fmt.Errorf("checkout anchor %q: unknown core step (catalog: %s)", anchor, strings.Join(CoreStepCatalog, ", "))
}

// MergePluginSteps inserts plugin steps into the core checkout workflow.
func MergePluginSteps(core []Step, plugins []PluginStepRegistration) ([]Step, error) {
	if len(plugins) == 0 {
		return append([]Step(nil), core...), nil
	}

	type batchKey struct {
		anchor string
		after  bool
	}
	batches := make(map[batchKey][]Step)

	for _, reg := range plugins {
		if reg.Step == nil {
			return nil, fmt.Errorf("checkout plugin step must not be nil")
		}
		mode, anchor, err := ParseStepPosition(reg.Position)
		if err != nil {
			return nil, fmt.Errorf("checkout plugin step %q: %w", reg.Step.Name(), err)
		}
		k := batchKey{anchor: anchor, after: mode == StepPositionAfter}
		batches[k] = append(batches[k], reg.Step)
	}

	out := make([]Step, 0, len(core)+len(plugins))
	emitted := 0
	for _, step := range core {
		name := step.Name()
		if steps := batches[batchKey{anchor: name, after: false}]; len(steps) > 0 {
			out = append(out, steps...)
			emitted += len(steps)
		}
		out = append(out, step)
		if steps := batches[batchKey{anchor: name, after: true}]; len(steps) > 0 {
			out = append(out, steps...)
			emitted += len(steps)
		}
	}
	if emitted < len(plugins) {
		return nil, fmt.Errorf("checkout plugin steps: %d registration(s) target missing core step(s)", len(plugins)-emitted)
	}
	return out, nil
}
