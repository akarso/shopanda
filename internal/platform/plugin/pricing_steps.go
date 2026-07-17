package plugin

import (
	"fmt"

	apppricing "github.com/akarso/shopanda/internal/application/pricing"
)

// PricingStepRegistration records a plugin pricing step and its pipeline position.
type PricingStepRegistration struct {
	Step     any
	Position string
}

type pricingStepRegistration struct {
	step     any
	position string
}

// RegisterPricingStep registers a pricing pipeline step.
// The step must implement pricing.PricingStep.
//
// Optional position uses before:<step>, after:<step>, or replace:<step>
// (aliases: base_price, discounts, promotions, taxes, finalization).
// Empty position defaults to after:base (between base price and catalog promotions).
// Only one replace registration is allowed per core step name.
func (a *App) RegisterPricingStep(step any, position ...string) {
	if step == nil {
		panic("plugin: pricing step must not be nil")
	}
	pos := apppricing.DefaultPluginStepPosition
	if len(position) > 0 && position[0] != "" {
		pos = position[0]
	}
	mode, anchor, err := apppricing.ParseStepPosition(pos)
	if err != nil {
		panic("plugin: " + err.Error())
	}
	if mode == apppricing.StepPositionReplace {
		for _, existing := range a.pricingSteps {
			em, eAnchor, err := apppricing.ParseStepPosition(existing.position)
			if err != nil {
				continue
			}
			if em == apppricing.StepPositionReplace && eAnchor == anchor {
				panic(fmt.Sprintf("plugin: duplicate pricing step replacement replace:%s", anchor))
			}
		}
	}
	a.pricingSteps = append(a.pricingSteps, pricingStepRegistration{
		step:     step,
		position: pos,
	})
}

// PricingSteps returns registered plugin pricing steps in registration order.
func (a *App) PricingSteps() []any {
	regs := a.PricingStepRegistrations()
	if len(regs) == 0 {
		return nil
	}
	out := make([]any, len(regs))
	for i, reg := range regs {
		out[i] = reg.Step
	}
	return out
}

// PricingStepRegistrations returns plugin pricing steps with positions.
func (a *App) PricingStepRegistrations() []PricingStepRegistration {
	if len(a.pricingSteps) == 0 {
		return nil
	}
	out := make([]PricingStepRegistration, len(a.pricingSteps))
	for i, reg := range a.pricingSteps {
		out[i] = PricingStepRegistration{
			Step:     reg.step,
			Position: reg.position,
		}
	}
	return out
}
