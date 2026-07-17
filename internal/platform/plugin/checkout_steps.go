package plugin

import (
	checkoutapp "github.com/akarso/shopanda/internal/application/checkout"
)

// CheckoutStepRegistration records a plugin checkout step and its workflow position.
type CheckoutStepRegistration struct {
	Step     any
	Position string
}

type checkoutStepRegistration struct {
	step     any
	position string
}

// RegisterCheckoutStep registers a checkout workflow step.
// The step must implement checkout.Step.
//
// Optional position uses start, end, before:<step>, or after:<step>
// (aliases: validate, pricing, inventory, order, shipping, payment).
// Empty position defaults to end (legacy append-only behavior).
func (a *App) RegisterCheckoutStep(step any, position ...string) {
	if step == nil {
		panic("plugin: checkout step must not be nil")
	}
	pos := checkoutapp.DefaultPluginStepPosition
	if len(position) > 0 && position[0] != "" {
		pos = position[0]
	}
	if _, _, err := checkoutapp.ParseStepPosition(pos); err != nil {
		panic("plugin: " + err.Error())
	}
	a.checkoutSteps = append(a.checkoutSteps, checkoutStepRegistration{
		step:     step,
		position: pos,
	})
}

// CheckoutSteps returns registered plugin checkout steps in registration order.
func (a *App) CheckoutSteps() []any {
	regs := a.CheckoutStepRegistrations()
	if len(regs) == 0 {
		return nil
	}
	out := make([]any, len(regs))
	for i, reg := range regs {
		out[i] = reg.Step
	}
	return out
}

// CheckoutStepRegistrations returns plugin checkout steps with positions.
func (a *App) CheckoutStepRegistrations() []CheckoutStepRegistration {
	if len(a.checkoutSteps) == 0 {
		return nil
	}
	out := make([]CheckoutStepRegistration, len(a.checkoutSteps))
	for i, reg := range a.checkoutSteps {
		out[i] = CheckoutStepRegistration{
			Step:     reg.step,
			Position: reg.position,
		}
	}
	return out
}
